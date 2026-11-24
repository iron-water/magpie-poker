import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import http from "node:http";
import { test } from "node:test";

const B64 = "bas" + "e64url";

import {
  deriveSession,
  fingerprint,
  generateIdentity,
  hostProof,
  joinProof,
  nonce,
  publicKeyObject,
  safeEqual,
  signRequest,
  verifyRequest,
} from "../server/net/crypto.mjs";
import { createInvite, inviteUrl, parseInvite, shortId } from "../server/net/invite.mjs";
import { PeerClient, PeerError } from "../server/net/peer.mjs";

function tableSecret() {
  return randomBytes(32).toString(B64);
}

function b64u(text) {
  return Buffer.from(text, "utf8").toString(B64);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function within(promise, ms = 4000) {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`超时 ${ms}ms`)), ms);
    timer.unref?.();
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer));
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (chunk) => chunks.push(chunk));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve(server.address().port));
  });
}

test("X25519 双方派生同一会话", () => {
  // JSON round-trip proves the private JWK survives storage/transport.
  const alice = JSON.parse(JSON.stringify(generateIdentity()));
  const bob = JSON.parse(JSON.stringify(generateIdentity()));
  const secret = tableSecret();
  const shared = {
    priv: alice.priv,
    peerPub: bob.pub,
    tableSecret: secret,
    tableId: "t_1",
    playerId: "p_a",
  };
  const left = deriveSession(shared);
  const right = deriveSession({ ...shared, priv: bob.priv, peerPub: alice.pub });
  assert.equal(left.length, 32);
  assert.ok(left.equals(right));
  assert.equal(typeof publicKeyObject(bob.pub).export, "function");

  const otherPlayer = deriveSession({ ...shared, playerId: "p_b" });
  assert.ok(!left.equals(otherPlayer));
  const otherTable = deriveSession({ ...shared, tableId: "t_2" });
  assert.ok(!left.equals(otherTable));
});

test("joinProof 对密钥、桌、公钥、随机数敏感", () => {
  const pub = generateIdentity().pub;
  const secret = tableSecret();
  const n = nonce();
  const proof = joinProof(secret, "t_1", pub, n);
  assert.match(proof, /^[0-9a-f]{64}$/);
  assert.equal(proof, joinProof(secret, "t_1", pub, n));
  assert.notEqual(proof, joinProof(tableSecret(), "t_1", pub, n));
  assert.notEqual(proof, joinProof(secret, "t_2", pub, n));
  assert.notEqual(proof, joinProof(secret, "t_1", generateIdentity().pub, n));
  assert.notEqual(proof, joinProof(secret, "t_1", pub, nonce()));
});

test("signRequest/verifyRequest 拒绝任何篡改", () => {
  const key = randomBytes(32);
  const body = JSON.stringify({ type: "raise", amount: 300 });
  const path = "/peer/action";
  const ts = 1764000000000;
  const sig = signRequest(key, "post", path, ts, body);
  assert.match(sig, /^[0-9a-f]{64}$/);
  assert.equal(verifyRequest(key, "POST", path, ts, body, sig), true);

  assert.equal(verifyRequest(key, "POST", path, ts, `${body} `, sig), false);
  assert.equal(verifyRequest(key, "POST", path, ts, JSON.stringify({ type: "raise", amount: 301 }), sig), false);
  assert.equal(verifyRequest(key, "GET", path, ts, body, sig), false);
  assert.equal(verifyRequest(key, "POST", path, ts + 1, body, sig), false);
  assert.equal(verifyRequest(key, "POST", "/peer/chat", ts, body, sig), false);
  assert.equal(verifyRequest(key, "POST", path, ts, body, `${sig}0`), false);
  assert.equal(verifyRequest(key, "POST", path, ts, body, ""), false);
  assert.equal(verifyRequest(randomBytes(32), "POST", path, ts, body, sig), false);

  const empty = signRequest(key, "GET", "/peer/events", ts, "");
  assert.equal(verifyRequest(key, "GET", "/peer/events", ts, "", empty), true);
  assert.equal(verifyRequest(key, "GET", "/peer/events", ts, undefined, empty), true);
});

test("nonce 与 fingerprint 稳定", () => {
  const id = generateIdentity();
  const prints = new Set([fingerprint(id.pub), fingerprint(id.pub)]);
  assert.equal(prints.size, 1);
  assert.match(fingerprint(id.pub), /^[0-9a-f]{16}$/);
  assert.equal(fingerprint(id.pub), fingerprint(JSON.parse(JSON.stringify(id)).pub));
  assert.notEqual(fingerprint(id.pub), fingerprint(generateIdentity().pub));

  const seen = new Set();
  for (let i = 0; i < 50; i++) {
    const n = nonce();
    assert.match(n, /^[0-9a-f]{32}$/);
    seen.add(n);
  }
  assert.equal(seen.size, 50);

  assert.equal(safeEqual("abc", "abc"), true);
  assert.equal(safeEqual("abc", "abd"), false);
  assert.equal(safeEqual("abc", "abcd"), false);

  const key = randomBytes(32);
  const proof = hostProof(key, "p_1");
  assert.match(proof, /^[0-9a-f]{64}$/);
  assert.equal(proof, hostProof(key, "p_1"));
  assert.notEqual(proof, hostProof(key, "p_2"));
  assert.notEqual(proof, hostProof(randomBytes(32), "p_1"));
});

test("邀请码生成并解析所有接受的形式", () => {
  const hostId = generateIdentity();
  const secret = tableSecret();
  const fields = {
    tableId: "t_7f3a",
    tableName: "周五牌局",
    host: "100.76.46.108",
    port: 3478,
    tls: false,
    hostPub: hostId.pub,
    secret,
    hostName: "ironwater",
    expiresAt: 1765000000000,
  };
  const code = createInvite(fields);
  assert.ok(code.startsWith("POKER1."));
  assert.ok(code.length <= 4096);
  const parsed = parseInvite(code);
  assert.deepEqual(parsed, { v: 1, ...fields });

  const forms = [
    code,
    `  ${code}  `,
    `poker://join?c=${code}`,
    `poker://join?code=${code}`,
    `http://100.76.46.108:3478/#${code}`,
    `https://ironwater.example-tailnet.ts.net/#${code}`,
    `https://ironwater.example-tailnet.ts.net/?invite=${code}`,
    inviteUrl(code, "http://100.76.46.108:3478"),
  ];
  for (const form of forms) assert.deepEqual(parseInvite(form), parsed, form.slice(0, 40));

  assert.deepEqual(parseInvite(createInvite({ ...fields, expiresAt: undefined })).expiresAt, null);
  assert.equal(inviteUrl(code, "http://100.76.46.108:3478/"), `http://100.76.46.108:3478/#${code}`);
  assert.equal(inviteUrl(code, "http://100.76.46.108:3478/?x=1"), `http://100.76.46.108:3478/#${code}`);
});

test("拒绝畸形邀请码且不回显密钥", () => {
  const hostId = generateIdentity();
  const secret = tableSecret();
  const base = {
    v: 1,
    tableId: "t_1",
    tableName: "桌",
    host: "127.0.0.1",
    port: 3478,
    tls: false,
    hostPub: hostId.pub,
    secret,
    hostName: "h",
  };
  const craft = (overrides) => `POKER1.${b64u(JSON.stringify({ ...base, ...overrides }))}`;
  const malformed = [
    "",
    "   ",
    "hello world",
    "POKER1.",
    "POKER2.abc",
    `POKER1.${"A".repeat(5000)}`,
    craft({ v: 2 }),
    craft({ tableId: 42 }),
    craft({ tableId: "" }),
    craft({ host: 7 }),
    craft({ hostName: null }),
    craft({ tableName: 3 }),
    craft({ port: 0 }),
    craft({ port: 70000 }),
    craft({ port: "3478" }),
    craft({ tls: "no" }),
    craft({ secret: Buffer.alloc(16).toString(B64) }),
    craft({ secret: "not a key" }),
    craft({ hostPub: Buffer.alloc(16).toString(B64) }),
    craft({ hostPub: "" }),
    craft({ expiresAt: "not a date" }),
    "http://127.0.0.1:3478/",
  ];
  for (const input of malformed) {
    assert.throws(() => parseInvite(input), (error) => {
      assert.ok(error instanceof Error);
      assert.ok(error.message.length > 0);
      return true;
    });
  }
  try {
    parseInvite(craft({ port: 0 }));
    assert.fail("应当拒绝");
  } catch (error) {
    assert.ok(!error.message.includes(secret));
  }
});

test("shortId 适合界面展示", () => {
  assert.equal(shortId("t_7f3a"), "t_7f3a");
  assert.equal(shortId("p_abcdefghij"), "p_abcd");
  assert.equal(shortId(""), "?");
  assert.equal(shortId(null), "?");
  assert.equal(shortId("p_ab12"), "p_ab12");
});

async function startHost() {
  const hostId = generateIdentity();
  const secret = tableSecret();
  const tableId = "t_live1";
  const playerId = "p_guest01";
  const state = {
    session: null,
    joins: 0,
    badProof: false,
    eventsSigOk: false,
    actions: [],
  };
  let sseResponse = null;
  let sseClosedResolve = () => {};
  state.sseClosed = new Promise((resolve) => {
    sseClosedResolve = resolve;
  });

  const respond = (res, status, payload) => {
    res.writeHead(status, { "content-type": "application/json" });
    res.end(JSON.stringify(payload));
  };

  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, "http://127.0.0.1");
    try {
      if (req.method === "POST" && url.pathname === "/peer/join") {
        state.joins += 1;
        const data = JSON.parse(await readBody(req));
        const expected = joinProof(secret, tableId, data.pub, data.nonce);
        if (!safeEqual(expected, String(data.proof ?? ""))) {
          state.badProof = true;
          respond(res, 400, { ok: false, code: "bad_proof", error: "邀请校验失败" });
          return;
        }
        state.session = deriveSession({
          priv: hostId.priv,
          peerPub: data.pub,
          tableSecret: secret,
          tableId,
          playerId,
        });
        respond(res, 200, {
          ok: true,
          playerId,
          seat: 2,
          hostPub: hostId.pub,
          hostProof: hostProof(state.session, playerId),
          sessionId: "s_live",
        });
        return;
      }

      if (req.method === "GET" && url.pathname === "/peer/events") {
        const ts = url.searchParams.get("ts") ?? "";
        const sig = url.searchParams.get("sig") ?? "";
        state.eventsSigOk =
          url.searchParams.get("playerId") === playerId &&
          verifyRequest(state.session, "GET", "/peer/events", ts, "", sig);
        res.writeHead(200, {
          "content-type": "text/event-stream",
          "cache-control": "no-cache",
          connection: "keep-alive",
        });
        sseResponse = res;
        res.on("close", () => sseClosedResolve());
        res.write(": 心跳\n\n");
        // A malformed event must be ignored, and the notice uses CRLF plus
        // split data lines before the state event arrives.
        res.write(Buffer.from('event: state\ndata: {not json\n\n', "utf8"));
        res.write(
          Buffer.from('event: notice\r\ndata: {"level":"info",\r\ndata: "text":"你好"}\r\n\r\n', "utf8"),
        );
        await delay(5);
        const event = Buffer.from(
          `event: state\nid: 1\ndata: ${JSON.stringify({ mode: "guest", table: { id: tableId, name: "周五牌局" } })}\n\n`,
          "utf8",
        );
        // Split inside a multi-byte character to exercise the decoder.
        const cut = Math.max(1, event.indexOf(Buffer.from("五")) + 1);
        res.write(event.subarray(0, cut));
        await delay(5);
        res.write(event.subarray(cut));
        return;
      }

      if (req.method === "POST" && url.pathname === "/peer/action") {
        const body = await readBody(req);
        const okSig =
          String(req.headers["x-poker-player"] ?? "") === playerId &&
          verifyRequest(
            state.session,
            "POST",
            "/peer/action",
            String(req.headers["x-poker-ts"] ?? ""),
            body,
            String(req.headers["x-poker-sig"] ?? ""),
          );
        state.actions.push({ body, okSig });
        respond(res, 200, { ok: true, accepted: okSig });
        return;
      }

      if (req.method === "POST" && url.pathname === "/peer/chat") {
        await readBody(req);
        respond(res, 400, { ok: false, code: "rate_limited", error: "太快了" });
        return;
      }

      respond(res, 404, { ok: false, error: "not found" });
    } catch (error) {
      respond(res, 500, { ok: false, error: String(error?.message ?? error) });
    }
  });

  const port = await listen(server);
  return {
    server,
    port,
    hostId,
    secret,
    tableId,
    playerId,
    state,
    async close() {
      if (sseResponse) sseResponse.end();
      server.closeAllConnections();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

test("集成：入桌、SSE 状态与签名请求", async (t) => {
  const host = await startHost();
  t.after(() => host.close());
  const code = createInvite({
    tableId: host.tableId,
    tableName: "周五牌局",
    host: "127.0.0.1",
    port: host.port,
    tls: false,
    hostPub: host.hostId.pub,
    secret: host.secret,
    hostName: "ironwater",
  });

  const states = [];
  const notices = [];
  const statuses = [];
  let resolveState = () => {};
  const firstState = new Promise((resolve) => {
    resolveState = resolve;
  });
  const client = new PeerClient({
    invite: code,
    profile: { name: "Mio", avatar: "fox", gatewayUrl: "https://mio.example.ts.net:8444", tailscale: true },
    onState: (snapshot) => {
      states.push(snapshot);
      resolveState(snapshot);
    },
    onNotice: (notice) => notices.push(notice),
    onStatus: (status) => statuses.push(status),
    fetchImpl: globalThis.fetch,
  });

  await client.connect();
  assert.equal(client.status, "online");
  assert.equal(client.playerId, host.playerId);
  assert.equal(client.seat, 2);
  assert.equal(client.sessionId, "s_live");
  assert.equal(client.baseUrl, `http://127.0.0.1:${host.port}`);
  assert.deepEqual(statuses, ["connecting", "online"]);
  assert.equal(host.state.badProof, false);

  const stop = client.subscribe();
  const snapshot = await within(firstState);
  assert.equal(snapshot.mode, "guest");
  assert.equal(snapshot.table.name, "周五牌局");
  assert.equal(states.length, 1);
  assert.deepEqual(notices, [{ level: "info", text: "你好" }]);
  assert.equal(host.state.eventsSigOk, true);

  const reply = await client.post("/peer/action", { type: "raise", amount: 300 });
  assert.equal(reply.ok, true);
  assert.equal(reply.accepted, true);
  assert.equal(host.state.actions.length, 1);
  assert.equal(host.state.actions[0].okSig, true);
  assert.equal(host.state.actions[0].body, JSON.stringify({ type: "raise", amount: 300 }));

  await assert.rejects(
    () => client.post("/peer/chat", { text: "hi" }),
    (error) => error instanceof PeerError && error.code === "rate_limited" && error.status === 400,
  );

  stop();
  await within(host.state.sseClosed, 2000);
  assert.equal(client.status, "online");

  client.close();
  assert.equal(client.status, "closed");
  await assert.rejects(
    () => client.post("/peer/action", { type: "fold" }),
    (error) => error instanceof PeerError && error.code === "closed",
  );
  assert.equal(typeof client.subscribe(), "function");
  assert.equal(client.subscribe()(), undefined);
});

test("入桌失败：错误密钥与假冒主机", async (t) => {
  const hostId = generateIdentity();
  const secret = tableSecret();
  const tableId = "t_bad";
  const playerId = "p_bad";
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, "http://127.0.0.1");
    if (req.method !== "POST" || url.pathname !== "/peer/join") {
      res.writeHead(404);
      res.end();
      return;
    }
    const data = JSON.parse(await readBody(req));
    const expected = joinProof(secret, tableId, data.pub, data.nonce);
    if (!safeEqual(expected, String(data.proof ?? ""))) {
      res.writeHead(400, { "content-type": "application/json" });
      res.end(JSON.stringify({ ok: false, code: "bad_proof", error: "邀请校验失败" }));
      return;
    }
    deriveSession({ priv: hostId.priv, peerPub: data.pub, tableSecret: secret, tableId, playerId });
    res.writeHead(200, { "content-type": "application/json" });
    res.end(
      JSON.stringify({
        ok: true,
        playerId,
        seat: 0,
        hostPub: hostId.pub,
        hostProof: "0".repeat(64),
        sessionId: "s_bad",
      }),
    );
  });
  const port = await listen(server);
  t.after(() => {
    server.closeAllConnections();
    return new Promise((resolve) => server.close(resolve));
  });

  const fields = {
    tableId,
    tableName: "测试桌",
    host: "127.0.0.1",
    port,
    tls: false,
    hostPub: hostId.pub,
    hostName: "ironwater",
  };
  const good = createInvite({ ...fields, secret });
  const wrong = createInvite({ ...fields, secret: tableSecret() });

  const rejected = new PeerClient({ invite: wrong, profile: { name: "Mio" } });
  await assert.rejects(
    () => rejected.connect(),
    (error) => error instanceof PeerError && error.code === "bad_proof" && error.status === 400,
  );
  assert.equal(rejected.status, "offline");
  assert.equal(rejected.playerId, null);

  const spoofed = new PeerClient({ invite: good, profile: { name: "Mio" } });
  await assert.rejects(
    () => spoofed.connect(),
    (error) => error instanceof PeerError && error.code === "bad_host_proof",
  );
  assert.equal(spoofed.status, "offline");
  assert.equal(spoofed.playerId, null);
});
