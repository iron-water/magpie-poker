import test from "node:test";
import assert from "node:assert/strict";
import { computePots, awardPots } from "../server/poker/pots.mjs";

const p = (id, committed, folded = false) => ({ id, seat: Number(id.slice(1)), committed, folded });

test("matched bets make one pot", () => {
  const r = computePots([p("p0", 20), p("p1", 20)]);
  assert.deepEqual(r.pots, [{ amount: 40, eligible: ["p0", "p1"] }]);
  assert.equal(r.total, 40);
  assert.deepEqual(r.refunds, []);
});

test("uncalled bet comes back before pots", () => {
  const r = computePots([p("p0", 100), p("p1", 30, true), p("p2", 30)]);
  assert.deepEqual(r.refunds, [{ id: "p0", amount: 70 }]);
  assert.deepEqual(r.pots, [{ amount: 90, eligible: ["p0", "p2"] }]);
  assert.equal(r.total, 90);
});

test("side pots split by commitment level", () => {
  const r = computePots([p("p0", 100), p("p1", 60), p("p2", 100)]);
  assert.deepEqual(r.pots, [
    { amount: 180, eligible: ["p0", "p1", "p2"] },
    { amount: 80, eligible: ["p0", "p2"] },
  ]);
  assert.equal(r.total, 260);
  assert.deepEqual(r.refunds, []);
});

test("top-heavy stacks refund and build two pots", () => {
  const r = computePots([p("p0", 100), p("p1", 300), p("p2", 300)]);
  assert.deepEqual(r.refunds, []);
  assert.deepEqual(r.pots, [
    { amount: 300, eligible: ["p0", "p1", "p2"] },
    { amount: 400, eligible: ["p1", "p2"] },
  ]);
  assert.equal(r.total, 700);
});

test("a folded player's chips stay in the pot but cannot win", () => {
  const r = computePots([p("p0", 50, true), p("p1", 50), p("p2", 50)]);
  assert.deepEqual(r.pots, [{ amount: 150, eligible: ["p1", "p2"] }]);
});

test("everyone all-in for the same amount", () => {
  const r = computePots([p("p0", 100), p("p1", 100), p("p2", 100)]);
  assert.deepEqual(r.pots, [{ amount: 300, eligible: ["p0", "p1", "p2"] }]);
});

test("a single contributor gets everything back", () => {
  const r = computePots([p("p0", 25), p("p1", 0, true)]);
  assert.deepEqual(r.refunds, [{ id: "p0", amount: 25 }]);
  assert.deepEqual(r.pots, []);
});

test("odd chip goes to the first winner left of the dealer", () => {
  const pots = [{ amount: 101, eligible: ["p0", "p1"] }];
  const scores = { p0: 10, p1: 10 };
  const r = awardPots(pots, (id) => scores[id], {
    dealerSeat: 0,
    seatCount: 3,
    seatOf: (id) => Number(id.slice(1)),
  });
  assert.equal(r[0].winners.length, 2);
  // seats 0,1,2 -> dealer 0, first seat left is 1
  assert.deepEqual(
    r[0].winners.map((w) => [w.id, w.chips]),
    [
      ["p1", 51],
      ["p0", 50],
    ]
  );
});

test("awardPots gives a tie split and nothing to losers", () => {
  const pots = [{ amount: 200, eligible: ["p0", "p1", "p2"] }];
  const scores = { p0: 7, p1: 9, p2: 9 };
  const r = awardPots(pots, (id) => scores[id], {
    dealerSeat: 0,
    seatCount: 3,
    seatOf: (id) => Number(id.slice(1)),
  });
  assert.deepEqual(r[0].winners.map((w) => w.id), ["p1", "p2"]);
  assert.equal(r[0].winners.reduce((n, w) => n + w.chips, 0), 200);
});
