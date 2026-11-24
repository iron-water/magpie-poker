import assert from "node:assert/strict";
import { test } from "node:test";
import { parseCard, rankOf, seededShuffle, suitOf } from "../server/poker/cards.mjs";
import { categoryName, compareScores, evaluate } from "../server/poker/eval.mjs";

const h = (...cards) => evaluate(cards);

function subsetOf(hand, best) {
  const pool = hand.map(parseCard).sort((a, b) => a - b);
  const got = best.slice().sort((a, b) => a - b);
  let i = 0;
  for (const c of got) {
    while (i < pool.length && pool[i] !== c) i++;
    assert.ok(i < pool.length, `best card ${c} not in hand`);
    i++;
  }
}

function checkShape(hand) {
  const r = evaluate(hand);
  assert.equal(r.best.length, 5);
  assert.equal(new Set(r.best).size, 5);
  for (const c of r.best) assert.ok(Number.isInteger(c) && c >= 0 && c < 52);
  subsetOf(hand, r.best);
  assert.equal(h(...r.best).score, r.score, "best combination must reproduce the score");
  assert.ok(Number.isInteger(r.score) && r.score >= 0);
  assert.equal(typeof r.name, "string");
  assert.equal(typeof r.nameZh, "string");
  return r;
}

// Independent evaluator: rank bitmask + group-by-count, no shared code with eval.mjs.
function naiveScore(cards) {
  const ranks = cards.map(rankOf).sort((a, b) => b - a);
  const flush = new Set(cards.map(suitOf)).size === 1;
  let mask = 0;
  for (const r of ranks) mask |= 1 << r;
  const has = (r) => (mask & (1 << r)) !== 0;
  let straightHigh = 0;
  for (let high = 14; high >= 6 && !straightHigh; high--) {
    if (has(high) && has(high - 1) && has(high - 2) && has(high - 3) && has(high - 4)) straightHigh = high;
  }
  if (!straightHigh && has(14) && has(5) && has(4) && has(3) && has(2)) straightHigh = 5;

  const counts = new Map();
  for (const r of ranks) counts.set(r, (counts.get(r) || 0) + 1);
  const of = (n) => [...counts.keys()].filter((r) => counts.get(r) === n).sort((a, b) => b - a);
  const quads = of(4);
  const trips = of(3);
  const pairs = of(2);
  const singles = of(1);

  let category;
  let sig;
  if (straightHigh && flush) [category, sig] = [8, [straightHigh]];
  else if (quads.length) [category, sig] = [7, [quads[0], singles[0]]];
  else if (trips.length && pairs.length) [category, sig] = [6, [trips[0], pairs[0]]];
  else if (flush) [category, sig] = [5, ranks.slice(0, 5)];
  else if (straightHigh) [category, sig] = [4, [straightHigh]];
  else if (trips.length) [category, sig] = [3, [trips[0], singles[0], singles[1]]];
  else if (pairs.length >= 2) [category, sig] = [2, [pairs[0], pairs[1], singles[0]]];
  else if (pairs.length === 1) [category, sig] = [1, [pairs[0], singles[0], singles[1], singles[2]]];
  else [category, sig] = [0, ranks.slice(0, 5)];

  const full = sig.concat([0, 0, 0, 0, 0]).slice(0, 5);
  const shifts = [16, 12, 8, 4, 0];
  let score = category * 2 ** 20;
  for (let i = 0; i < 5; i++) score += full[i] * 2 ** shifts[i];
  return { score, category };
}

function naiveBest(cards) {
  let best = null;
  const n = cards.length;
  for (let i = 0; i < n - 4; i++) {
    for (let j = i + 1; j < n - 3; j++) {
      for (let k = j + 1; k < n - 2; k++) {
        for (let l = k + 1; l < n - 1; l++) {
          for (let m = l + 1; m < n; m++) {
            const r = naiveScore([cards[i], cards[j], cards[k], cards[l], cards[m]]);
            if (!best || r.score > best.score) best = r;
          }
        }
      }
    }
  }
  return best;
}

test("every category", () => {
  const cases = [
    [["2c", "4d", "7h", "9s", "Jc"], 0, "High card J", "J 高牌"],
    [["As", "Ah", "7d", "4c", "2s"], 1, "Pair of A", "一对 A"],
    [["As", "Ah", "Kd", "Kc", "3s"], 2, "Two pair, A and K", "两对 A 与 K"],
    [["7s", "7h", "7d", "Kc", "2s"], 3, "Three of a kind, 7", "三条 7"],
    [["9s", "8h", "7d", "6c", "5s"], 4, "Straight, 9 high", "9 高顺子"],
    [["As", "Ks", "9s", "5s", "3s"], 5, "Flush, A high", "A 高同花"],
    [["Ks", "Kh", "Kd", "4c", "4s"], 6, "Full house, K over 4", "K 葫芦"],
    [["Qs", "Qh", "Qd", "Qc", "8s"], 7, "Four of a kind, Q", "四条 Q"],
    [["9h", "8h", "7h", "6h", "5h"], 8, "Straight flush, 9 high", "同花顺 9 高"],
  ];
  for (const [cards, category, name, nameZh] of cases) {
    const r = checkShape(cards);
    assert.equal(r.category, category, cards.join(" "));
    assert.equal(r.name, name);
    assert.equal(r.nameZh, nameZh);
  }
});

test("wheel and broadway", () => {
  const wheel = checkShape(["As", "2h", "3d", "4c", "5s"]);
  assert.equal(wheel.category, 4);
  assert.deepEqual(wheel.ranks, [5, 0, 0, 0, 0]);
  assert.equal(wheel.nameZh, "5 高顺子");

  const broadway = checkShape(["Ts", "Jd", "Qh", "Kc", "As"]);
  assert.equal(broadway.category, 4);
  assert.deepEqual(broadway.ranks, [14, 0, 0, 0, 0]);
  assert.equal(compareScores(wheel.score, broadway.score), -1);

  assert.equal(h("As", "2h", "3d", "4c", "5s", "6h").category, 4);
  assert.equal(h("As", "2h", "3d", "4c", "5s", "6h").ranks[0], 6, "6-high straight beats the wheel");
  assert.equal(h("2h", "3d", "4c", "5s", "6h", "7c").ranks[0], 7);

  const wheelSf = checkShape(["Ah", "2h", "3h", "4h", "5h"]);
  assert.equal(wheelSf.category, 8);
  assert.deepEqual(wheelSf.ranks, [5, 0, 0, 0, 0]);
});

test("kicker ordering", () => {
  assert.equal(compareScores(h("As", "Ah", "Kd", "7c", "2s").score, h("As", "Ah", "Qd", "7c", "2s").score), 1);
  assert.equal(compareScores(h("As", "Ah", "Kd", "Qc", "2s").score, h("As", "Ah", "Kd", "Jc", "2s").score), 1);
  assert.equal(compareScores(h("7s", "7h", "7d", "Ac", "2s").score, h("7s", "7h", "7d", "Kc", "2s").score), 1);
  assert.equal(compareScores(h("As", "Ah", "Kd", "Kc", "3s").score, h("As", "Ah", "Kd", "Kc", "2s").score), 1);
  assert.equal(compareScores(h("As", "Ks", "9s", "5s", "3s").score, h("As", "Ks", "9s", "5s", "2s").score), 1);
  assert.equal(compareScores(h("As", "Kd", "9h", "7c", "3s").score, h("As", "Kd", "9h", "7c", "2s").score), 1);
});

test("full house beats a flush and a straight", () => {
  const boat = h("2s", "2h", "2d", "3c", "3s");
  const flush = h("As", "Ks", "Qs", "Js", "9s");
  assert.equal(boat.category, 6);
  assert.equal(flush.category, 5);
  assert.equal(compareScores(boat.score, flush.score), 1);
  assert.equal(compareScores(flush.score, h("9h", "8d", "7c", "6s", "5h").score), 1);
  // two pair plus trips on a 7-card board makes the boat, not trips.
  const seven = checkShape(["9s", "9h", "9d", "4c", "4s", "Kd", "2c"]);
  assert.equal(seven.category, 6);
  assert.deepEqual(seven.ranks.slice(0, 2), [9, 4]);
});

test("quads with kicker", () => {
  const a = checkShape(["2s", "2h", "2d", "2c", "As"]);
  const k = checkShape(["2s", "2h", "2d", "2c", "Ks"]);
  assert.equal(a.category, 7);
  assert.deepEqual(a.ranks, [2, 14, 0, 0, 0]);
  assert.equal(compareScores(a.score, k.score), 1);
  const seven = checkShape(["2s", "2h", "2d", "2c", "As", "Ks", "Qs"]);
  assert.equal(seven.category, 7);
  assert.deepEqual(seven.ranks, [2, 14, 0, 0, 0]);
});

test("split pot equal scores", () => {
  const a = h("As", "Kd", "Qh", "Jc", "9s");
  const b = h("Ah", "Ks", "Qd", "Js", "9c");
  assert.equal(a.score, b.score);
  assert.equal(compareScores(a.score, b.score), 0);
  assert.equal(compareScores(b.score, a.score), 0);
  assert.equal(compareScores(1, 2), -1);
  assert.equal(compareScores(2, 1), 1);
  assert.equal(compareScores(7, 7), 0);

  const flushA = h("As", "Ks", "Qs", "Js", "9s");
  const flushB = h("Ah", "Kh", "Qh", "Jh", "9h");
  assert.equal(compareScores(flushA.score, flushB.score), 0);
});

test("3-card board plus 2 hole cards", () => {
  const r = checkShape(["As", "Kd", "Qh", "Jc", "Ts"]);
  assert.equal(r.category, 4);
  assert.deepEqual(r.ranks, [14, 0, 0, 0, 0]);
  const turned = checkShape(["9h", "8h", "7h", "Th", "6h"]);
  assert.equal(turned.category, 8);
  assert.deepEqual(turned.ranks, [10, 0, 0, 0, 0]);

  // Board pairs, hole cards only play when they improve the best five of seven.
  const boardPair = checkShape(["As", "Ah", "7d", "2c", "3s", "9h", "Kd"]);
  assert.equal(boardPair.category, 1);
  assert.deepEqual(boardPair.ranks, [14, 13, 9, 7, 0]);
});

test("input validation", () => {
  assert.throws(() => evaluate(["As", "As", "Kd", "Qh", "Jc"]), /duplicate card: As/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh"]), /need 5 to 7 cards, got 4/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh", "Tc", "9s", "8h", "7d"]), /need 5 to 7 cards, got 8/);
  assert.throws(() => evaluate([]), /need 5 to 7 cards/);
  assert.throws(() => evaluate("As Ks Qd Jh Tc"), /cards must be an array/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh", "1s"]), /bad card/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh", "Ax"]), /bad card/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh", null]), /bad card/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh", 52]), /bad card/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh", -1]), /bad card/);
  assert.throws(() => evaluate(["As", "Ks", "Qd", "Jh", "10s"]), /bad card/);
});

test("score format and categoryName", () => {
  const r = h("As", "Ah", "Kd", "Kc", "3s");
  assert.equal(r.score, (2 << 20) | (14 << 16) | (13 << 12) | (3 << 8));
  assert.deepEqual(r.ranks, [14, 13, 3, 0, 0]);
  assert.deepEqual(categoryName(4, [5, 0, 0, 0, 0]), { en: "Straight, 5 high", zh: "5 高顺子" });
  assert.deepEqual(categoryName(8, [9, 0, 0, 0, 0]), { en: "Straight flush, 9 high", zh: "同花顺 9 高" });
  assert.equal(evaluate(["As", "Ks", "Qs", "Js", "Ts"]).score, evaluate([48, 44, 40, 36, 32]).score);
});

test("randomized cross-check against the naive evaluator (10k hands)", () => {
  for (let i = 0; i < 10000; i++) {
    const deck = seededShuffle("eval-cross-check", i);
    const hand = deck.slice(0, 7);
    const mine = evaluate(hand);
    const theirs = naiveBest(hand);
    assert.equal(mine.score, theirs.score, `hand ${i}: ${hand.join(",")}`);
    assert.equal(mine.category, theirs.category, `hand ${i}: ${hand.join(",")}`);
    assert.equal(h(...mine.best).score, mine.score);
  }
});

test("randomized cross-check on 5 and 6 card hands", () => {
  for (let i = 0; i < 2000; i++) {
    const deck = seededShuffle("eval-short", i);
    const size = i % 2 === 0 ? 5 : 6;
    const hand = deck.slice(0, size);
    const mine = evaluate(hand);
    const theirs = naiveBest(hand);
    assert.equal(mine.score, theirs.score, `${size} cards, hand ${i}`);
    assert.equal(mine.category, theirs.category);
  }
});
