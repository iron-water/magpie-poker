import test from "node:test";
import assert from "node:assert/strict";
import { Table, PokerError } from "../server/poker/game.mjs";

const SEED = "ab".repeat(32);

function table(seats = 2, config = {}) {
  const t = new Table({
    id: "t_test",
    name: "测试",
    hostId: "p0",
    config: { smallBlind: 10, bigBlind: 20, startingChips: 1000, actionSeconds: 0, maxSeats: seats, ...config },
  });
  for (let i = 0; i < seats; i++) t.addPlayer({ id: `p${i}`, name: `P${i}` });
  return t;
}

test("heads-up: button posts small blind and acts first preflop", () => {
  const t = table(2);
  t.startHand(SEED);
  assert.equal(t.dealerSeat, 0);
  assert.equal(t.sbSeat, 0);
  assert.equal(t.bbSeat, 1);
  assert.equal(t.toAct, "p0");
  assert.equal(t.player("p0").bet, 10);
  assert.equal(t.player("p1").bet, 20);

  t.act("p0", { type: "call" });
  assert.equal(t.toAct, "p1");
  t.act("p1", { type: "check" });
  assert.equal(t.street, "flop");
  assert.equal(t.board.length, 3);
  assert.equal(t.toAct, "p1");

  t.act("p1", { type: "bet", amount: 20 });
  assert.equal(t.toAct, "p0");
  t.act("p0", { type: "call" });
  assert.equal(t.street, "turn");
  assert.equal(t.toAct, "p1");
});

test("three-handed: button is UTG preflop, small blind first postflop", () => {
  const t = table(3);
  t.startHand(SEED);
  assert.equal(t.dealerSeat, 0);
  assert.equal(t.sbSeat, 1);
  assert.equal(t.bbSeat, 2);
  assert.equal(t.toAct, "p0");

  t.act("p0", { type: "fold" });
  t.act("p1", { type: "call" });
  t.act("p2", { type: "check" });
  assert.equal(t.street, "flop");
  assert.equal(t.toAct, "p1");
});

test("minimum raise is enforced and reopens action", () => {
  const t = table(2);
  t.startHand(SEED);
  assert.equal(t.legalFor(t.player("p0")).raise.min, 40);
  t.act("p0", { type: "raise", amount: 40 });
  assert.equal(t.legalFor(t.player("p1")).raise.min, 60);
  assert.throws(() => t.act("p1", { type: "raise", amount: 50 }), (e) => e instanceof PokerError && e.code === "too_small");
  t.act("p1", { type: "raise", amount: 60 });
  assert.equal(t.legalFor(t.player("p0")).raise.min, 80);
  t.act("p0", { type: "raise", amount: 80 });
  assert.equal(t.currentBet, 80);
});

test("fold to one player awards the whole pot without a showdown", () => {
  const t = table(2);
  t.startHand(SEED);
  t.act("p0", { type: "raise", amount: 60 });
  t.act("p1", { type: "fold" });
  assert.equal(t.phase, "showdown");
  assert.equal(t.showdown.reason, "fold");
  assert.equal(t.showdown.total, 80);
  assert.equal(t.player("p0").chips, 1020);
  assert.equal(t.player("p1").chips, 980);
  assert.equal(t.player("p0").chips + t.player("p1").chips, 2000);
});

test("all-in runout builds a main pot and a side pot", () => {
  const t = table(3);
  t.player("p0").chips = 100;
  t.player("p1").chips = 60;
  t.player("p2").chips = 200;
  t.startHand(SEED);
  assert.equal(t.toAct, "p0");
  t.act("p0", { type: "allin" });
  t.act("p1", { type: "call" });
  t.act("p2", { type: "call" });
  assert.equal(t.phase, "showdown");
  assert.equal(t.board.length, 5);
  assert.equal(t.showdown.total, 260);
  assert.deepEqual(t.showdown.pots.map((x) => x.amount).sort((a, b) => a - b), [80, 180]);
  assert.equal(t.players.reduce((n, p) => n + p.chips, 0), 360);
});

test("controlled showdown pays the best five-card hand", async () => {
  const t = table(2);
  t.startHand(SEED);
  t.phase = "hand";
  t.street = "river";
  const { parseCard } = await import("../server/poker/cards.mjs");
  t.board = ["2c", "7d", "9s", "Jc", "3h"].map(parseCard);
  t.player("p0").cards = ["As", "Ah"].map(parseCard);
  t.player("p1").cards = ["Ks", "Kh"].map(parseCard);
  for (const p of t.players) {
    p.committed = 100;
    p.chips = 900;
    p.folded = false;
    p.allIn = false;
    p.bet = 0;
  }
  t.handPlayers = ["p0", "p1"];
  t.showdown = null;
  t.finishShowdown();
  assert.equal(t.phase, "showdown");
  assert.equal(t.player("p0").chips, 1100);
  assert.equal(t.player("p1").chips, 900);
  assert.equal(t.showdown.pots[0].winners[0], "p0");
});

test("split pot: the board plays for both players", async () => {
  const t = table(2);
  t.startHand(SEED);
  const { parseCard } = await import("../server/poker/cards.mjs");
  t.board = ["Ah", "Kh", "Qh", "Jh", "Th"].map(parseCard);
  t.player("p0").cards = ["2c", "3d"].map(parseCard);
  t.player("p1").cards = ["4s", "5c"].map(parseCard);
  for (const p of t.players) {
    p.committed = 100;
    p.chips = 900;
    p.folded = false;
    p.allIn = false;
    p.bet = 0;
  }
  t.handPlayers = ["p0", "p1"];
  t.showdown = null;
  t.finishShowdown();
  assert.equal(t.player("p0").chips, 1000);
  assert.equal(t.player("p1").chips, 1000);
  assert.equal(t.showdown.pots[0].winners.length, 2);
});

test("timeout folds when facing a bet and checks when it can", () => {
  const t = table(2, { actionSeconds: 1 });
  t.startHand(SEED);
  t.deadline = Date.now() - 1;
  assert.equal(t.tick(), true);
  assert.equal(t.player("p0").folded, true);
  assert.equal(t.phase, "showdown");

  const u = table(2, { actionSeconds: 1 });
  u.startHand(SEED);
  u.act("p0", { type: "call" });
  u.act("p1", { type: "check" });
  assert.equal(u.street, "flop");
  u.deadline = Date.now() - 1;
  assert.equal(u.tick(), true);
  assert.equal(u.player("p1").lastAction, "过牌");
});

test("illegal actions are rejected", () => {
  const t = table(2);
  assert.throws(() => t.act("p0", { type: "fold" }), (e) => e.code === "no_hand");
  t.startHand(SEED);
  assert.throws(() => t.act("p1", { type: "fold" }), (e) => e.code === "out_of_turn");
  assert.throws(() => t.act("p0", { type: "check" }), (e) => e.code === "must_call");
  assert.throws(() => t.act("p0", { type: "bet", amount: 50 }), (e) => e.code === "use_raise");
});

test("settlement matches winners and losers and is zero-sum", () => {
  const t = table(2);
  t.startHand(SEED);
  t.act("p0", { type: "raise", amount: 60 });
  t.act("p1", { type: "fold" });
  t.endTable();
  assert.equal(t.phase, "ended");
  assert.equal(t.settlement.transfers.length, 1);
  const x = t.settlement.transfers[0];
  assert.equal(x.from, "p1");
  assert.equal(x.to, "p0");
  assert.equal(x.amountChips, 20);
  assert.equal(x.amount, 0.01);
  assert.equal(x.status, "pending");
});

test("state redacts other players' cards and exposes legal actions only on turn", () => {
  const t = table(2);
  t.startHand(SEED);
  const a = t.viewFor("p0");
  assert.equal(a.me.cards.length, 2);
  assert.equal(a.seats.find((s) => s.id === "p1").cards, null);
  assert.ok(a.me.legal);
  assert.equal(a.me.legal.call, 10);
  const b = t.viewFor("p1");
  assert.equal(b.me.legal, null);
});

test("JSON round-trip keeps the hand playable", () => {
  const t = table(2);
  t.startHand(SEED);
  t.act("p0", { type: "call" });
  const clone = Table.fromJSON(t.toJSON());
  assert.equal(clone.street, "preflop");
  assert.equal(clone.toAct, "p1");
  assert.equal(clone.player("p0").chips, 980);
  clone.act("p1", { type: "check" });
  assert.equal(clone.street, "flop");
});

test("rebuy between hands raises chips and buy-in together", () => {
  const t = table(2);
  t.startHand(SEED);
  t.act("p0", { type: "fold" });
  const before = t.player("p1").chips;
  t.rebuy("p1", 500);
  assert.equal(t.player("p1").chips, before + 500);
  assert.equal(t.player("p1").buyIn, 1500);
});
