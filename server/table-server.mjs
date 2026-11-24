import { createServer } from "node:http";
import { randomBytes } from "node:crypto";
import { deriveSession, generateIdentity, hostProof, joinProof, safeEqual, verifyRequest } from "./net/crypto.mjs";
import { PokerError } from "./poker/game.mjs";

const MAX_BODY = 64 * 1024;
const TS_WINDOW = 120_000;
const B64 = "bas" + "e64url";

export class TableServer {
  constructor({ table, identity = generateIdentity(), secret, port = 3482, onChange = () => {}, log = () => {} }) {
    this.table = table;
    this.identity = identity;
    this.secret = secret;
    this.port = port;
    this.onChange = onChange;
    this.log = log;
    this.sessions = new Map();
    this.seen = new Set();
    this.seenQueue = [];
    this.joinAttempts = new Map();
    this.closeTimers = new Map();
    this.server = null;
    this.timer = null;
    this.boundPort = 0;
  }

  async listen() {
    const server = createServer((req, res) => {
      this.handle(req, res).catch((err) => {
        if (res.headersSent) {
          try { res.end(); } catch { /* ignore */ }
          return;
        }
        const status = err instanceof PokerError ? 400 : 500;
        json(res, status, { ok: false, error: String(err?.message || err), code: err?.code || null });
      });
    });
    this.server = server;
    for (let i = 0; i < 10; i++) {
      const wanted = this.port + i;
      try {
        await listen(server, wanted, "0.0.0.0");
        this.boundPort = server.address().port;
        break;
      } catch (err) {
        if (err?.code !== "EADDRINUSE") throw err;
      }
    }
    if (!server.listening) throw new Error(`牌桌端口 ${this.port}–${this.port + 9} 都被占用`);
    this.timer = setInterval(() => {
      try {
        if (this.table.tick()) {
          this.onChange();
          this.broadcast();
        }
      } catch (err) {
        this.log(`tick: ${err?.message || err}`);
      }
    }, 500);
    this.timer.unref?.();
    return this.boundPort;
  }

  async close() {
    clearInterval(this.timer);
    for (const t of this.closeTimers.values()) clearTimeout(t);
    this.closeTimers.clear();
    for (const s of this.sessions.values()) for (const res of s.streams) res.end();
    this.sessions.clear();
    if (this.server) await new Promise((r) => this.server.close(r));
    this.server = null;
  }

  hostProofFor(playerId) {
    const s = this.sessions.get(playerId);
    return s ? s.key : null;
  }

  async handle(req, res) {
    const url = new URL(req.url, "http://peer");
    const path = url.pathname;

    if (path === "/peer/health" || path === "/peer/peek") {
      if (req.method === "GET" || req.method === "POST") {
        return json(res, 200, {
          ok: true,
          tableId: this.table.id,
          tableName: this.table.name,
          phase: this.table.phase,
          players: this.table.players.filter((p) => p.seat >= 0).length,
          maxSeats: this.table.maxSeats,
          version: 1,
        });
      }
    }

    if (path === "/peer/join" && req.method === "POST") return this.join(req, res);
    if (path === "/peer/events" && req.method === "GET") return this.events(req, res, url);

    if (path.startsWith("/peer/") && req.method === "POST") {
      const body = await readBody(req);
      const session = this.authenticate(req, path, body);
      return this.signed(req, res, path, body, session);
    }

    return json(res, 404, { ok: false, error: "not found" });
  }

  async join(req, res) {
    const ip = req.socket.remoteAddress || "?";
    const now = Date.now();
    const rl = this.joinAttempts.get(ip) || { n: 0, reset: now + 60_000 };
    if (now > rl.reset) {
      rl.n = 0;
      rl.reset = now + 60_000;
    }
    rl.n += 1;
    this.joinAttempts.set(ip, rl);
    if (rl.n > 20) return json(res, 429, { ok: false, error: "尝试太频繁" });

    let body;
    try {
      body = JSON.parse((await readBody(req)) || "{}");
    } catch {
      return json(res, 400, { ok: false, error: "JSON 不合法", code: "bad_json" });
    }
    if (body.v !== 1) return json(res, 400, { ok: false, error: "协议版本不匹配", code: "bad_version" });
    if (body.tableId !== this.table.id) return json(res, 404, { ok: false, error: "没有这张牌桌", code: "wrong_table" });
    if (this.table.phase === "ended") return json(res, 409, { ok: false, error: "牌局已经结束", code: "ended" });
    const name = String(body.name || "").trim().slice(0, 16);
    if (!name) return json(res, 400, { ok: false, error: "缺少名字", code: "no_name" });
    if (typeof body.pub !== "string" || typeof body.nonce !== "string" || typeof body.proof !== "string") {
      return json(res, 400, { ok: false, error: "入桌信息不完整", code: "bad_join" });
    }
    let proof;
    try {
      proof = joinProof(this.secret, this.table.id, body.pub, body.nonce);
    } catch {
      return json(res, 400, { ok: false, error: "公钥不合法", code: "bad_pub" });
    }
    if (!safeEqual(proof, body.proof)) return json(res, 403, { ok: false, error: "邀请码不匹配", code: "bad_proof" });

    let id = `p_${randomBytes(4).toString("hex")}`;
    while (this.table.player(id)) id = `p_${randomBytes(4).toString("hex")}`;
    let player;
    try {
      player = this.table.addPlayer({
        id,
        name,
        avatar: String(body.avatar || "fox").slice(0, 16),
        gatewayUrl: String(body.profile?.gatewayUrl || "").slice(0, 300),
        tailscale: body.profile?.tailscale || null,
      });
    } catch (err) {
      return json(res, 409, { ok: false, error: String(err.message || err), code: err?.code || "join_failed" });
    }
    if (player.id !== id) {
      this.dropSession(player.id);
      id = player.id;
    }
    const sessionKey = deriveSession({
      priv: this.identity.priv,
      peerPub: body.pub,
      tableSecret: this.secret,
      tableId: this.table.id,
      playerId: id,
    });
    this.sessions.set(id, { key: sessionKey, pub: body.pub, streams: new Set(), joinedAt: Date.now() });
    this.onChange();
    this.broadcast();
    this.log(`join ${id} seat=${player.seat}`);
    return json(res, 200, {
      ok: true,
      tableId: this.table.id,
      playerId: id,
      seat: player.seat,
      hostPub: this.identity.pub,
      hostProof: hostProof(sessionKey, id),
      sessionId: randomBytes(6).toString("hex"),
    });
  }

  authenticate(req, path, body) {
    const playerId = String(req.headers["x-poker-player"] || "");
    const ts = Number(req.headers["x-poker-ts"]);
    const sig = String(req.headers["x-poker-sig"] || "");
    const s = this.sessions.get(playerId);
    if (!s) throw Object.assign(new Error("入桌已失效"), { code: "no_session" });
    if (!Number.isFinite(ts) || Math.abs(Date.now() - ts) > TS_WINDOW) {
      throw Object.assign(new Error("请求已过期"), { code: "expired" });
    }
    if (!verifyRequest(s.key, req.method, path, ts, body, sig)) {
      throw Object.assign(new Error("签名不匹配"), { code: "bad_sig" });
    }
    if (this.seen.has(sig)) throw Object.assign(new Error("重复请求"), { code: "replay" });
    this.seen.add(sig);
    this.seenQueue.push(sig);
    if (this.seenQueue.length > 4000) this.seen.delete(this.seenQueue.shift());
    return { playerId, key: s.key };
  }

  async signed(req, res, path, body, session) {
    let data = {};
    if (body) {
      try {
        data = JSON.parse(body);
      } catch {
        return json(res, 400, { ok: false, error: "JSON 不合法", code: "bad_json" });
      }
    }
    const { playerId } = session;
    try {
      if (path === "/peer/action") this.table.act(playerId, data);
      else if (path === "/peer/chat") this.table.chat(playerId, data?.text);
      else if (path === "/peer/rebuy") this.table.rebuy(playerId, data?.amount);
      else if (path === "/peer/settle") {
        this.table.markIssued(data?.transferId, playerId, {
          key: data?.key,
          expires: data?.expires,
          gatewayUrl: data?.gatewayUrl,
        });
      } else if (path === "/peer/verify") this.table.markVerified(data?.transferId, playerId);
      else if (path === "/peer/leave") this.table.leave(playerId);
      else if (path === "/peer/ping") return json(res, 200, { ok: true, t: Date.now() });
      else return json(res, 404, { ok: false, error: "not found" });
    } catch (err) {
      return json(res, 400, { ok: false, error: String(err.message || err), code: err?.code || null });
    }
    this.onChange();
    this.broadcast();
    if (path === "/peer/leave") this.dropSession(playerId);
    return json(res, 200, { ok: true });
  }

  events(req, res, url) {
    const playerId = String(url.searchParams.get("playerId") || "");
    const ts = Number(url.searchParams.get("ts"));
    const sig = String(url.searchParams.get("sig") || "");
    const s = this.sessions.get(playerId);
    if (!s) return json(res, 403, { ok: false, error: "入桌已失效" });
    if (!Number.isFinite(ts) || Math.abs(Date.now() - ts) > TS_WINDOW) {
      return json(res, 403, { ok: false, error: "请求已过期" });
    }
    if (!verifyRequest(s.key, "GET", "/peer/events", ts, "", sig)) {
      return json(res, 403, { ok: false, error: "签名不匹配" });
    }
    const p = this.table.player(playerId);
    if (!p || p.left) return json(res, 403, { ok: false, error: "不在牌桌上" });
    p.connected = true;
    p.sittingOut = false;
    const timer = this.closeTimers.get(playerId);
    if (timer) {
      clearTimeout(timer);
      this.closeTimers.delete(playerId);
    }

    res.writeHead(200, {
      "content-type": "text/event-stream; charset=utf-8",
      "cache-control": "no-cache, no-transform",
      connection: "keep-alive",
      "x-accel-buffering": "no",
    });
    res.write("retry: 1500\n\n");
    s.streams.add(res);
    this.send(res, this.table.viewFor(playerId));
    const beat = setInterval(() => {
      try { res.write(": ping\n\n"); } catch { /* closed */ }
    }, 15_000);
    beat.unref?.();
    const done = () => {
      clearInterval(beat);
      s.streams.delete(res);
      if (!s.streams.size && this.sessions.get(playerId) === s) {
        const t = setTimeout(() => {
          this.closeTimers.delete(playerId);
          if (this.sessions.get(playerId) === s && !s.streams.size) {
            this.table.markDisconnected(playerId);
            this.onChange();
            this.broadcast();
          }
        }, 8000);
        t.unref?.();
        this.closeTimers.set(playerId, t);
      }
    };
    req.on("close", done);
    req.on("error", done);
  }

  send(res, view) {
    try {
      res.write(`event: state\ndata: ${JSON.stringify(view)}\n\n`);
    } catch {
      /* the close handler cleans it up */
    }
  }

  broadcast() {
    for (const [playerId, s] of this.sessions) {
      if (!s.streams.size) continue;
      const view = this.table.viewFor(playerId);
      for (const res of s.streams) this.send(res, view);
    }
  }

  dropSession(playerId) {
    const s = this.sessions.get(playerId);
    if (!s) return;
    for (const res of s.streams) {
      try { res.end(); } catch { /* ignore */ }
    }
    this.sessions.delete(playerId);
  }
}

function listen(server, port, host) {
  return new Promise((resolve, reject) => {
    const onError = (err) => {
      server.removeListener("listening", onOk);
      reject(err);
    };
    const onOk = () => {
      server.removeListener("error", onError);
      resolve();
    };
    server.once("error", onError);
    server.once("listening", onOk);
    server.listen(port, host);
  });
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    let size = 0;
    const chunks = [];
    req.on("data", (c) => {
      size += c.length;
      if (size > MAX_BODY) {
        reject(Object.assign(new Error("请求体太大"), { code: "too_large" }));
        req.destroy();
        return;
      }
      chunks.push(c);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
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
