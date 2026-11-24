import { createServer } from "node:http";
import { randomBytes } from "node:crypto";
import { readFile } from "node:fs/promises";
import { existsSync, readFileSync } from "node:fs";
import { extname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { Table, PokerError } from "./poker/game.mjs";
import { Store } from "./store.mjs";
import { TableServer } from "./table-server.mjs";
import { PeerClient } from "./net/peer.mjs";
import { createInvite, inviteUrl, parseInvite } from "./net/invite.mjs";
import { generateIdentity } from "./net/crypto.mjs";
import {
  ensureBank,
  gatewayUrl,
  issueKey,
  listKeys,
  magpieBin,
  redact,
  verifyKey,
} from "./magpie/keys.mjs";
import { enableServe, serveStatus, tailscaleStatus } from "./magpie/tailscale.mjs";

const here = fileURLToPath(new URL(".", import.meta.url));
const WEB_ROOT = join(here, "..", "web");
const ASSET_ROOT = join(here, "..", "assets");
const B64 = "bas" + "e64url";
const VERSION = (() => {
  try {
    return JSON.parse(readFileSync(join(here, "..", "package.json"), "utf8")).version || "0.1.0";
  } catch {
    return "0.1.0";
  }
})();

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".webp": "image/webp",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".woff2": "font/woff2",
  ".ico": "image/x-icon",
  ".txt": "text/plain; charset=utf-8",
};

let singleton = null;

export async function startLocalServer(options = {}) {
  if (singleton) return singleton;
  const app = new LocalApp(options);
  try {
    await app.listen();
  } catch (err) {
    // A previous plugin host may still hold the page port. If that process is
    // this same service, reuse it instead of failing the whole plugin host.
    if (err?.code === "EADDRINUSE" && (await isOurs(app.port))) {
      singleton = { port: app.port, reused: true, close: async () => {} };
      return singleton;
    }
    throw err;
  }
  singleton = app;
  return app;
}

async function isOurs(port) {
  try {
    const res = await fetch(`http://127.0.0.1:${port}/api/health`, { signal: AbortSignal.timeout(1500) });
    if (!res.ok) return false;
    const j = await res.json();
    return j?.name === "magpie-poker";
  } catch {
    return false;
  }
}

export class LocalApp {
  constructor(options = {}) {
    this.options = options;
    this.port = Number.isFinite(Number(options.port)) ? Number(options.port) : 3481;
    this.tablePort = Number.isFinite(Number(options.tablePort)) ? Number(options.tablePort) : 3482;
    this.offline = options.tailscale === false;
    this.store = new Store(options.dataDir);
    this.profile = this.store.loadProfile();
    this.mode = "idle";
    this.table = null;
    this.hostPlayerId = "";
    this.host = null;
    this.tableServer = null;
    this.peer = null;
    this.peerState = null;
    this.notice = null;
    this.error = null;
    this.ts = { installed: null, name: "", ip: "", dns: "", error: null };
    this.serve = null;
    this.keys = { magpie: !!magpieBin(), bin: magpieBin() || "", issuer: null, all: [], error: null };
    this.sse = new Set();
    this.server = null;
    this.saveTimer = null;
  }

  async listen() {
    const server = createServer((req, res) => {
      this.handle(req, res).catch((err) => {
        if (res.headersSent) {
          try { res.end(); } catch { /* ignore */ }
          return;
        }
        json(res, err instanceof PokerError ? 400 : 500, {
          ok: false,
          error: redact(String(err?.message || err)),
          code: err?.code || null,
        });
      });
    });
    await listen(server, this.port, "127.0.0.1");
    this.server = server;
    this.port = server.address().port;
    void this.bootstrap();
    return this.port;
  }

  async bootstrap() {
    if (this.offline) {
      this.ts = { installed: false, name: "local", ip: "127.0.0.1", dns: "", error: null };
    } else {
      try {
        this.ts = await tailscaleStatus();
        const st = await serveStatus();
        this.serve = st.ok ? st.config : { error: st.error };
      } catch (err) {
        this.ts = { installed: false, error: redact(String(err?.message || err)) };
      }
    }
    try {
      await this.resumeHost();
    } catch (err) {
      this.notify("error", `恢复上次牌桌失败：${redact(String(err?.message || err))}`);
    }
    if (this.mode === "host") {
      this.buildInvites();
      this.persistHost();
    }
    this.push();
  }

  async resumeHost() {
    const rec = this.store.loadHost();
    if (!rec?.tableId || !rec?.identity || !rec?.secret || !rec?.hostPlayerId) return;
    const data = this.store.loadTable(rec.tableId);
    if (!data) return;
    const table = Table.fromJSON(data);
    this.table = table;
    this.hostPlayerId = rec.hostPlayerId;
    this.host = {
      identity: rec.identity,
      secret: rec.secret,
      tableId: table.id,
      hostPlayerId: rec.hostPlayerId,
      port: Number(rec.port) || this.tablePort,
      servedUrl: rec.servedUrl || null,
    };
    this.tableServer = new TableServer({
      table,
      identity: rec.identity,
      secret: rec.secret,
      port: this.host.port,
      onChange: () => {
        this.persist();
        this.push();
      },
      log: (m) => console.log(`[poker] ${m}`),
    });
    this.host.port = await this.tableServer.listen();
    this.mode = "host";
    this.buildInvites();
    this.persistHost();
    console.log(`[poker] resumed table ${table.id} on :${this.host.port}`);
  }

  // ---- state -------------------------------------------------------------

  currentView() {
    if (this.mode === "host" && this.table) return this.table.viewFor(this.hostPlayerId);
    if (this.mode === "guest") return this.peerState;
    return null;
  }

  state() {
    const view = this.currentView();
    const tableMe = view?.me || null;
    const id =
      tableMe?.id ||
      (this.mode === "host" ? this.hostPlayerId : this.mode === "guest" ? this.peer?.playerId : "") ||
      "";
    const me = {
      id,
      seat: tableMe?.seat ?? (this.mode === "guest" ? this.peer?.seat ?? null : null),
      name: this.profile.name,
      isHost: this.mode === "host",
      ...(tableMe || {}),
    };
    if (view && this.mode === "host") view.invite = this.inviteInfo();
    return {
      mode: this.mode,
      version: VERSION,
      me,
      profile: this.profile,
      host: {
        name: this.ts?.name || "",
        ip: this.ts?.ip || "",
        dns: this.ts?.dns || "",
        tailscale: !!this.ts?.installed,
        serve: this.host?.servedUrl || "",
        serveGateway: this.profile.gatewayUrl || "",
        error: this.ts?.error || null,
      },
      connection: { status: this.peer?.status || null },
      keys: this.keys,
      table: view,
      notice: this.notice,
      error: this.error,
    };
  }

  push() {
    let payload;
    try {
      payload = `event: state\ndata: ${JSON.stringify(this.state())}\n\n`;
    } catch {
      return;
    }
    for (const res of this.sse) {
      try { res.write(payload); } catch { this.sse.delete(res); }
    }
  }

  notify(level, text) {
    this.notice = { level, text: String(text), at: Date.now() };
    for (const res of this.sse) {
      try {
        res.write(`event: notice\ndata: ${JSON.stringify({ level, text: this.notice.text })}\n\n`);
      } catch {
        this.sse.delete(res);
      }
    }
    this.push();
  }

  afterChange({ peers = true } = {}) {
    this.persist();
    this.push();
    if (peers && this.tableServer) this.tableServer.broadcast();
  }

  persist() {
    if (this.mode !== "host" || !this.table) return;
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => {
      try {
        this.store.saveTable(this.table.toJSON());
        this.persistHost();
      } catch (err) {
        console.error(`[poker] save failed: ${err?.message || err}`);
      }
    }, 250);
  }

  persistHost() {
    if (!this.host) return;
    this.store.saveHost({
      tableId: this.host.tableId,
      hostPlayerId: this.host.hostPlayerId,
      identity: this.host.identity,
      secret: this.host.secret,
      port: this.host.port,
      servedUrl: this.host.servedUrl || null,
    });
  }

  inviteInfo() {
    if (!this.host) return null;
    return {
      code: this.host.directCode || "",
      direct: this.host.directCode ? inviteUrl(this.host.directCode, this.host.directUrl) : this.host.directUrl || "",
      served: this.host.servedCode ? inviteUrl(this.host.servedCode, this.host.servedUrl) : null,
      servedCode: this.host.servedCode || null,
      servedUrl: this.host.servedUrl || null,
      tablePort: this.host.port,
    };
  }

  buildInvites() {
    if (!this.host) return;
    const ts = this.ts || {};
    const host = ts.ip || "127.0.0.1";
    const common = {
      tableId: this.host.tableId,
      tableName: this.table?.name || "牌局",
      hostPub: this.host.identity.pub,
      secret: this.host.secret,
      hostName: ts.name || "host",
    };
    this.host.directCode = createInvite({ ...common, host, port: this.host.port, tls: false });
    this.host.directUrl = `http://${host}:${this.host.port}/`;
    this.host.servedCode = null;
    if (this.host.servedUrl) {
      try {
        const u = new URL(this.host.servedUrl);
        this.host.servedCode = createInvite({
          ...common,
          host: u.hostname,
          port: Number(u.port || 443),
          tls: u.protocol === "https:",
        });
      } catch {
        this.host.servedUrl = null;
      }
    }
  }

  // ---- host / guest ------------------------------------------------------

  async hostTable(body = {}) {
    if (this.mode !== "idle") throw new PokerError("已经在牌局中，请先离开", "busy");
    if (this.ts.installed === null && !this.offline) {
      try {
        this.ts = await tailscaleStatus();
      } catch {
        /* local-only table */
      }
    }
    const config = { ...(body.config || {}) };
    const tableId = `t_${randomBytes(4).toString("hex")}`;
    const hostPlayerId = `p_${randomBytes(4).toString("hex")}`;
    const table = new Table({
      id: tableId,
      name: String(body.tableName || body.name || body.config?.tableName || "周五牌局").slice(0, 40),
      config,
      hostId: hostPlayerId,
    });
    table.addPlayer({
      id: hostPlayerId,
      name: this.profile.name,
      avatar: this.profile.avatar,
      gatewayUrl: this.profile.gatewayUrl,
      tailscale: { name: this.ts?.name || "", dns: this.ts?.dns || "", ip: this.ts?.ip || "" },
    });
    const identity = generateIdentity();
    const secret = randomBytes(32).toString(B64);
    const svr = new TableServer({
      table,
      identity,
      secret,
      port: this.tablePort,
      onChange: () => {
        this.persist();
        this.push();
      },
      log: (m) => console.log(`[poker] ${m}`),
    });
    const port = await svr.listen();
    this.table = table;
    this.hostPlayerId = hostPlayerId;
    this.host = { identity, secret, tableId, hostPlayerId, port, servedUrl: null };
    this.tableServer = svr;
    this.mode = "host";
    this.buildInvites();
    this.store.saveTable(table.toJSON());
    this.persistHost();
    this.notify("info", `牌桌已开：${table.name}`);
    this.afterChange();
    return { ok: true, invite: this.inviteInfo(), table: table.viewFor(hostPlayerId) };
  }

  async joinTable(code) {
    if (this.mode !== "idle") throw new PokerError("已经在牌局中，请先离开", "busy");
    const invite = parseInvite(code);
    if (invite.expiresAt && Number(invite.expiresAt) < Date.now()) throw new PokerError("邀请码已过期", "expired");
    const peer = new PeerClient({
      invite,
      profile: { ...this.profile, tailscale: !!this.ts?.installed },
      onState: (data) => {
        this.peerState = data;
        this.push();
      },
      onNotice: ({ level, text }) => this.notify(level || "info", text),
      onStatus: () => this.push(),
    });
    try {
      await peer.connect();
    } catch (err) {
      peer.close();
      throw err;
    }
    this.peer = peer;
    this.mode = "guest";
    this.peerState = null;
    peer.subscribe();
    this.notify("info", `已加入「${invite.tableName}」，等待牌桌状态`);
    this.push();
    return { ok: true, playerId: peer.playerId, seat: peer.seat };
  }

  async leaveTable() {
    if (this.mode === "host") {
      if (this.tableServer) await this.tableServer.close();
      this.tableServer = null;
      this.table = null;
      this.host = null;
      this.hostPlayerId = "";
      this.mode = "idle";
      this.store.clearHost();
    } else if (this.mode === "guest") {
      try {
        await this.peer?.post("/peer/leave", {});
      } catch {
        /* host may be gone */
      }
      this.peer?.close();
      this.peer = null;
      this.peerState = null;
      this.mode = "idle";
    }
    this.notify("info", "已离开牌桌");
    this.push();
    return { ok: true };
  }

  // ---- settlement --------------------------------------------------------

  async bank() {
    const saved = this.store.read("bank.json");
    const b = await ensureBank(saved?.id);
    this.store.write("bank.json", b);
    await this.refreshKeys();
    return b;
  }

  async issueTransfer(transferId) {
    const view = this.currentView();
    const me = view?.me?.id;
    const t = view?.settlement?.transfers?.find((x) => x.id === transferId);
    if (!t) throw new PokerError("没有这笔结算", "no_transfer");
    if (t.from !== me) throw new PokerError("只有欠款方能签发", "not_payer");
    if (t.status === "issued" || t.status === "verified") throw new PokerError("已经签发过了", "issued");
    if (t.status === "small") throw new PokerError("金额低于最小结算额", "small");
    const b = this.keys.issuer || (await this.bank());
    const issued = await issueKey({
      issuerId: b.id,
      name: `poker-${view?.id || "table"}-${transferId}-to-${t.to}`,
      unit: t.unit,
      amount: t.amount,
    });
    const gateway = this.profile.gatewayUrl || t.gatewayUrl || "";
    if (this.mode === "host") {
      this.table.markIssued(transferId, this.hostPlayerId, {
        key: issued.key,
        expires: issued.expires,
        gatewayUrl: gateway,
      });
      this.afterChange();
    } else {
      await this.peer.post("/peer/settle", {
        transferId,
        key: issued.key,
        expires: issued.expires,
        gatewayUrl: gateway,
      });
    }
    this.notify("info", `已签发 ${t.amount} ${t.unit === "tokens" ? "tokens" : ""} 给 ${t.toName}`);
    return { ok: true, expires: issued.expires };
  }

  async verifyTransfer(transferId) {
    const view = this.currentView();
    const me = view?.me?.id;
    const t = view?.settlement?.transfers?.find((x) => x.id === transferId);
    if (!t) throw new PokerError("没有这笔结算", "no_transfer");
    if (t.to !== me) throw new PokerError("只有收款方能验证", "not_payee");
    if (!t.key) throw new PokerError("对方还没有签发 key", "no_key");
    const result = await verifyKey({ url: t.gatewayUrl, key: t.key });
    if (!result.ok) return { ok: false, error: result.error || "key 验证失败", status: result.status || 0 };
    if (this.mode === "host") {
      this.table.markVerified(transferId, this.hostPlayerId);
      this.afterChange();
    } else {
      await this.peer.post("/peer/verify", { transferId });
    }
    this.notify("info", `key 可用，网关返回 ${result.models} 个模型`);
    return { ok: true, models: result.models };
  }

  async refreshKeys() {
    if (!magpieBin()) {
      this.keys = { magpie: false, bin: "", issuer: this.store.read("bank.json"), all: [], error: "没有找到 magpie 命令行" };
      return this.keys;
    }
    try {
      const all = await listKeys();
      const saved = this.store.read("bank.json");
      const issuer = saved?.id ? all.find((k) => k.id === saved.id) || saved : null;
      this.keys = { magpie: true, bin: magpieBin(), issuer, all, error: null };
    } catch (err) {
      this.keys = {
        magpie: true,
        bin: magpieBin(),
        issuer: this.store.read("bank.json"),
        all: [],
        error: redact(String(err?.message || err)),
      };
    }
    return this.keys;
  }

  // ---- HTTP --------------------------------------------------------------

  async handle(req, res) {
    const url = new URL(req.url, "http://local");
    const path = url.pathname;

    if (path === "/api/state" && req.method === "GET") return json(res, 200, this.state());
    if (path === "/api/health" && req.method === "GET") {
      return json(res, 200, {
        ok: true,
        name: "magpie-poker",
        version: VERSION,
        pid: process.pid,
        uptime: process.uptime(),
        mode: this.mode,
      });
    }
    if (path === "/api/events" && req.method === "GET") return this.events(req, res);
    if (path === "/api/keys" && req.method === "GET") return json(res, 200, await this.refreshKeys());
    if (path === "/api/tailscale" && req.method === "GET") {
      if (!this.offline) {
        this.ts = await tailscaleStatus();
        const st = await serveStatus();
        this.serve = st.ok ? st.config : { error: st.error };
        this.buildInvites();
      }
      const configured = Array.isArray(this.serve) && this.serve.length > 0;
      return json(res, 200, {
        ok: true,
        installed: !!this.ts?.installed,
        name: this.ts?.name || "",
        ip: this.ts?.ip || "",
        dns: this.ts?.dns || "",
        online: !!this.ts?.online,
        serve: this.host?.servedUrl || (configured ? "已配置" : ""),
        serveGateway: this.profile.gatewayUrl || "",
        error: this.ts?.error || null,
        invite: this.inviteInfo(),
      });
    }
    if (path.startsWith("/api/") && req.method === "POST") {
      if (req.headers["sec-fetch-site"] === "cross-site") {
        return json(res, 403, { ok: false, error: "cross-site request refused" });
      }
      const body = await readJson(req);
      return this.post(path, body, res);
    }
    if (path.startsWith("/api/")) return json(res, 404, { ok: false, error: "not found" });
    return this.static(path, res);
  }

  async post(path, body, res) {
    switch (path) {
      case "/api/profile": {
        const name = String(body.name ?? this.profile.name).trim().slice(0, 16);
        if (!name) return json(res, 400, { ok: false, error: "名字不能为空" });
        const gateway = String(body.gatewayUrl ?? this.profile.gatewayUrl).trim().slice(0, 300);
        if (gateway && !/^https?:\/\//.test(gateway)) return json(res, 400, { ok: false, error: "网关地址要以 http(s):// 开头" });
        this.profile = {
          name,
          avatar: String(body.avatar ?? this.profile.avatar).slice(0, 16) || "fox",
          gatewayUrl: gateway,
        };
        this.store.saveProfile(this.profile);
        if (this.mode === "host" && this.table) {
          const p = this.table.player(this.hostPlayerId);
          if (p) {
            p.name = name;
            p.gatewayUrl = gateway;
            this.afterChange();
          }
        }
        this.push();
        return json(res, 200, { ok: true, profile: this.profile });
      }
      case "/api/table/host": {
        const r = await this.hostTable(body);
        return json(res, 200, r);
      }
      case "/api/table/join": {
        const r = await this.joinTable(body.code);
        this.afterChange({ peers: false });
        return json(res, 200, r);
      }
      case "/api/table/leave": {
        const r = await this.leaveTable();
        return json(res, 200, r);
      }
      case "/api/table/start":
      case "/api/table/next": {
        if (this.mode !== "host") return json(res, 400, { ok: false, error: "只有房主能开始新的一手" });
        this.table.startHand();
        this.afterChange();
        return json(res, 200, { ok: true });
      }
      case "/api/table/rebuy": {
        const amount = Math.floor(Number(body.amount));
        if (this.mode === "host") {
          this.table.rebuy(this.hostPlayerId, amount);
          this.afterChange();
        } else if (this.mode === "guest") {
          await this.peer.post("/peer/rebuy", { amount });
        } else return json(res, 400, { ok: false, error: "不在牌桌上" });
        return json(res, 200, { ok: true });
      }
      case "/api/table/end": {
        if (this.mode !== "host") return json(res, 400, { ok: false, error: "只有房主能结束牌局" });
        this.table.endTable();
        this.afterChange();
        return json(res, 200, { ok: true, settlement: this.table.viewFor(this.hostPlayerId).settlement });
      }
      case "/api/action": {
        if (this.mode === "host") {
          this.table.act(this.hostPlayerId, body);
          this.afterChange();
        } else if (this.mode === "guest") {
          await this.peer.post("/peer/action", body);
        } else return json(res, 400, { ok: false, error: "不在牌桌上" });
        return json(res, 200, { ok: true });
      }
      case "/api/chat": {
        if (this.mode === "host") {
          this.table.chat(this.hostPlayerId, body.text);
          this.afterChange();
        } else if (this.mode === "guest") {
          await this.peer.post("/peer/chat", { text: body.text });
        } else return json(res, 400, { ok: false, error: "不在牌桌上" });
        return json(res, 200, { ok: true });
      }
      case "/api/settlement/issue": {
        const r = await this.issueTransfer(String(body.transferId || ""));
        return json(res, 200, r);
      }
      case "/api/settlement/verify": {
        const r = await this.verifyTransfer(String(body.transferId || ""));
        return json(res, 200, r);
      }
      case "/api/keys/bank": {
        const b = await ensureBank(body.id);
        this.store.write("bank.json", b);
        await this.refreshKeys();
        this.push();
        return json(res, 200, { ok: true, bank: b });
      }
      case "/api/keys/verify": {
        const r = await verifyKey({ url: body.url, key: body.key });
        return json(res, 200, r);
      }
      case "/api/tailscale/serve": {
        if (this.mode !== "host" || !this.host) return json(res, 400, { ok: false, error: "只有房主能共享牌桌" });
        const httpsPort = Number(body.port) > 0 ? Number(body.port) : 8443;
        const r = await enableServe({ targetPort: this.host.port, httpsPort });
        if (!r.ok) return json(res, 200, { ok: false, error: r.error });
        this.host.servedUrl = r.url;
        this.buildInvites();
        this.persistHost();
        this.push();
        return json(res, 200, { ok: true, url: r.url, invite: this.inviteInfo() });
      }
      case "/api/tailscale/serve-gateway": {
        const httpsPort = Number(body.port) > 0 ? Number(body.port) : 8444;
        const u = new URL(gatewayUrl());
        const targetPort = Number(u.port || (u.protocol === "https:" ? 443 : 80));
        const r = await enableServe({ targetPort, httpsPort });
        if (!r.ok) return json(res, 200, { ok: false, error: r.error });
        this.profile.gatewayUrl = r.url;
        this.store.saveProfile(this.profile);
        this.push();
        return json(res, 200, { ok: true, url: r.url });
      }
      default:
        return json(res, 404, { ok: false, error: "not found" });
    }
  }

  events(req, res) {
    res.writeHead(200, {
      "content-type": "text/event-stream; charset=utf-8",
      "cache-control": "no-cache, no-transform",
      connection: "keep-alive",
      "x-accel-buffering": "no",
    });
    res.write("retry: 1000\n\n");
    this.sse.add(res);
    try {
      res.write(`event: state\ndata: ${JSON.stringify(this.state())}\n\n`);
    } catch {
      /* closed */
    }
    const beat = setInterval(() => {
      try { res.write(": ping\n\n"); } catch { /* closed */ }
    }, 15_000);
    beat.unref?.();
    const done = () => {
      clearInterval(beat);
      this.sse.delete(res);
    };
    req.on("close", done);
    req.on("error", done);
  }

  async static(path, res) {
    let rel;
    try {
      rel = decodeURIComponent(path).replace(/^\/+/, "");
    } catch {
      return text(res, 400, "bad path");
    }
    if (rel.includes("\0") || rel.split("/").includes("..")) return text(res, 403, "forbidden");
    if (!rel) rel = "index.html";
    const asset = rel.startsWith("assets/");
    const root = asset ? ASSET_ROOT : WEB_ROOT;
    const file = resolve(root, asset ? rel.slice("assets/".length) : rel);
    if (!file.startsWith(root + sep) && file !== root) return text(res, 403, "forbidden");
    let data;
    try {
      data = await readFile(file);
    } catch {
      return text(res, 404, "not found");
    }
    const ext = extname(file).toLowerCase();
    res.writeHead(200, {
      "content-type": MIME[ext] || "application/octet-stream",
      "cache-control": ext === ".html" || ext === ".js" || ext === ".css" ? "no-cache" : "public, max-age=3600",
      "x-content-type-options": "nosniff",
      "referrer-policy": "no-referrer",
      "content-security-policy":
        "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'none'",
    });
    res.end(data);
  }

  async close() {
    clearTimeout(this.saveTimer);
    for (const res of this.sse) {
      try { res.end(); } catch { /* ignore */ }
    }
    this.sse.clear();
    this.peer?.close();
    if (this.tableServer) await this.tableServer.close();
    if (this.server) await new Promise((r) => this.server.close(r));
  }
}

async function readJson(req) {
  const chunks = [];
  let size = 0;
  for await (const c of req) {
    size += c.length;
    if (size > 256 * 1024) throw Object.assign(new Error("请求体太大"), { code: "too_large" });
    chunks.push(c);
  }
  if (!size) return {};
  const text = Buffer.concat(chunks).toString("utf8");
  try {
    return JSON.parse(text);
  } catch {
    throw Object.assign(new Error("JSON 不合法"), { code: "bad_json" });
  }
}

function listen(server, port, host) {
  return new Promise((resolvePromise, reject) => {
    const onError = (err) => {
      server.removeListener("listening", onOk);
      reject(err);
    };
    const onOk = () => {
      server.removeListener("error", onError);
      resolvePromise();
    };
    server.once("error", onError);
    server.once("listening", onOk);
    server.listen(port, host);
  });
}

function json(res, status, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
    "content-length": Buffer.byteLength(body),
  });
  res.end(body);
}

function text(res, status, body) {
  res.writeHead(status, { "content-type": "text/plain; charset=utf-8" });
  res.end(body);
}
