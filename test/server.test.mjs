import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { LocalApp, startLocalServer } from "../server/local-server.mjs";

async function app() {
  const dir = await mkdtemp(join(tmpdir(), "poker-test-"));
  const a = new LocalApp({ port: 0, tablePort: 0, dataDir: dir, tailscale: false });
  await a.listen();
  return { a, dir };
}

async function post(a, path, body, expectOk = true) {
  const res = await fetch(`http://127.0.0.1:${a.port}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body || {}),
  });
  const j = await res.json();
  if (expectOk && !j.ok) throw new Error(`${path}: ${j.error || res.status}`);
  return j;
}

async function waitFor(fn, ms = 4000) {
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const v = fn();
    if (v) return v;
    await new Promise((r) => setTimeout(r, 20));
  }
  throw new Error("timed out");
}

test("host and guest play a hand over the peer protocol", async () => {
  const { a, dir: da } = await app();
  const { a: b, dir: db } = await app();
  try {
    await post(a, "/api/profile", { name: "房主" });
    await post(b, "/api/profile", { name: "客人" });
    const hosted = await post(a, "/api/table/host", {
      name: "集成测试桌",
      config: {
        smallBlind: 5,
        bigBlind: 10,
        startingChips: 500,
        maxSeats: 2,
        actionSeconds: 30,
        chipValue: 0.01,
      },
    });
    assert.match(hosted.invite.code, /^POKER1\./);
    assert.equal(hosted.table.phase, "lobby");

    await post(b, "/api/table/join", { code: hosted.invite.code });
    await waitFor(() => b.state().table && b.state().table.phase === "lobby");
    assert.equal(b.state().mode, "guest");
    assert.equal(b.state().table.seats.length, 2);

    await post(a, "/api/table/start");
    await waitFor(() => b.state().table.phase === "hand");
    assert.equal(a.state().table.handNo, 1);
    assert.equal(a.state().me.legal.call, 5);
    assert.equal(a.state().table.seats.find((s) => s.name === "客人").cards, null);

    await post(a, "/api/action", { type: "call" });
    await waitFor(() => b.state().table.me.legal);
    await post(b, "/api/action", { type: "check" });
    await waitFor(() => a.state().table.board.length >= 3);
    await waitFor(() => b.state().me.legal);
    await post(b, "/api/action", { type: "check" });
    await waitFor(() => a.state().me.legal);

    await post(a, "/api/chat", { text: "认真打" });
    await waitFor(() => b.state().table.log.some((l) => l.text.includes("认真打")));

    await post(a, "/api/action", { type: "allin" });
    await waitFor(() => b.state().table.me.legal && b.state().table.me.legal.fold);
    await post(b, "/api/action", { type: "fold" });
    await waitFor(() => a.state().table.phase === "showdown");

    const ended = await post(a, "/api/table/end");
    assert.equal(ended.settlement.transfers.length, 1);
    const x = ended.settlement.transfers[0];
    assert.equal(x.fromName, "客人");
    assert.equal(x.toName, "房主");
    assert.equal(x.amountChips, 10);
    assert.equal(x.status, "pending");
    await waitFor(() => b.state().table.settlement);
    assert.equal(b.state().table.settlement.transfers[0].key, null);
  } finally {
    await a.close();
    await b.close();
    await rm(da, { recursive: true, force: true });
    await rm(db, { recursive: true, force: true });
  }
});

test("a wrong invite code cannot join", async () => {
  const { a, dir: da } = await app();
  const { a: b, dir: db } = await app();
  try {
    const hosted = await post(a, "/api/table/host", {
      name: "锁住的牌桌",
      config: { startingChips: 300, maxSeats: 2, actionSeconds: 30 },
    });
    const tampered = hosted.invite.code.slice(0, -4) + (hosted.invite.code.endsWith("AAAA") ? "BBBB" : "AAAA");
    const r = await post(b, "/api/table/join", { code: tampered }, false);
    assert.equal(r.ok, false);
    assert.equal(b.state().mode, "idle");
  } finally {
    await a.close();
    await b.close();
    await rm(da, { recursive: true, force: true });
    await rm(db, { recursive: true, force: true });
  }
});


test("startLocalServer reuses a service already on the page port", async () => {
  const { a, dir } = await app();
  try {
    const again = await startLocalServer({ port: a.port, tablePort: 0, dataDir: dir, tailscale: false });
    assert.equal(again.reused, true);
    assert.equal(again.port, a.port);
  } finally {
    await a.close();
  }
});
