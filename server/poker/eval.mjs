import { RANKS, formatCard, parseCard, rankOf, suitOf } from "./cards.mjs";

export const HIGH_CARD = 0;
export const PAIR = 1;
export const TWO_PAIR = 2;
export const TRIPS = 3;
export const STRAIGHT = 4;
export const FLUSH = 5;
export const FULL_HOUSE = 6;
export const QUADS = 7;
export const STRAIGHT_FLUSH = 8;

const RANK = new Int8Array(52);
const SUIT = new Int8Array(52);
for (let c = 0; c < 52; c++) {
  RANK[c] = rankOf(c);
  SUIT[c] = suitOf(c);
}

// Scratch for score5: it is synchronous and never re-entered.
const COUNT = new Int8Array(15);
const DESC = new Int8Array(5);

function pack(category, r1, r2, r3, r4, r5) {
  return (category << 20) | (r1 << 16) | (r2 << 12) | (r3 << 8) | (r4 << 4) | r5;
}

function score5(a, b, c, d, e) {
  const cnt = COUNT;
  cnt.fill(0);
  cnt[RANK[a]]++;
  cnt[RANK[b]]++;
  cnt[RANK[c]]++;
  cnt[RANK[d]]++;
  cnt[RANK[e]]++;
  const flush = SUIT[a] === SUIT[b] && SUIT[b] === SUIT[c] && SUIT[c] === SUIT[d] && SUIT[d] === SUIT[e];

  const desc = DESC;
  let n = 0;
  let quad = 0;
  let trips = 0;
  let hiPair = 0;
  let loPair = 0;
  for (let r = 14; r >= 2; r--) {
    const k = cnt[r];
    if (k === 0) continue;
    desc[n++] = r;
    if (k === 4) quad = r;
    else if (k === 3) trips = r;
    else if (k === 2) {
      if (hiPair) loPair = r;
      else hiPair = r;
    }
  }

  if (n === 5) {
    let high = 0;
    if (desc[0] - desc[4] === 4) high = desc[0];
    else if (desc[0] === 14 && desc[1] === 5 && desc[4] === 2) high = 5;
    if (high) return flush ? pack(STRAIGHT_FLUSH, high, 0, 0, 0, 0) : pack(STRAIGHT, high, 0, 0, 0, 0);
  }
  if (flush) return pack(FLUSH, desc[0], desc[1], desc[2], desc[3], desc[4]);
  if (quad) return pack(QUADS, quad, desc[0] === quad ? desc[1] : desc[0], 0, 0, 0);
  if (trips) {
    if (hiPair) return pack(FULL_HOUSE, trips, hiPair, 0, 0, 0);
    let k1 = 0;
    let k2 = 0;
    for (let i = 0; i < n; i++) {
      const r = desc[i];
      if (r === trips) continue;
      if (!k1) k1 = r;
      else {
        k2 = r;
        break;
      }
    }
    return pack(TRIPS, trips, k1, k2, 0, 0);
  }
  if (loPair) {
    let kicker = 0;
    for (let i = 0; i < n; i++) {
      if (desc[i] !== hiPair && desc[i] !== loPair) {
        kicker = desc[i];
        break;
      }
    }
    return pack(TWO_PAIR, hiPair, loPair, kicker, 0, 0);
  }
  if (hiPair) {
    let k1 = 0;
    let k2 = 0;
    let k3 = 0;
    for (let i = 0; i < n; i++) {
      const r = desc[i];
      if (r === hiPair) continue;
      if (!k1) k1 = r;
      else if (!k2) k2 = r;
      else {
        k3 = r;
        break;
      }
    }
    return pack(PAIR, hiPair, k1, k2, k3, 0);
  }
  return pack(HIGH_CARD, desc[0], desc[1], desc[2], desc[3], desc[4]);
}

function normalize(cards) {
  if (!Array.isArray(cards)) throw new Error("cards must be an array");
  const n = cards.length;
  if (n < 5 || n > 7) throw new Error(`need 5 to 7 cards, got ${n}`);
  const out = new Array(n);
  const seen = new Set();
  for (let i = 0; i < n; i++) {
    let card;
    try {
      card = parseCard(cards[i]);
    } catch {
      throw new Error(`bad card: ${JSON.stringify(cards[i])}`);
    }
    if (seen.has(card)) throw new Error(`duplicate card: ${formatCard(card)}`);
    seen.add(card);
    out[i] = card;
  }
  return out;
}

export function evaluate(cards) {
  const hand = normalize(cards);
  const n = hand.length;
  const idx = [0, 1, 2, 3, 4];
  let bestScore = -1;
  let bestCombo = null;
  for (;;) {
    const s = score5(hand[idx[0]], hand[idx[1]], hand[idx[2]], hand[idx[3]], hand[idx[4]]);
    if (s > bestScore) {
      bestScore = s;
      bestCombo = idx.slice();
    }
    let i = 4;
    while (i >= 0 && idx[i] === n - 5 + i) i--;
    if (i < 0) break;
    idx[i]++;
    for (let j = i + 1; j < 5; j++) idx[j] = idx[j - 1] + 1;
  }
  const category = bestScore >>> 20;
  const ranks = [
    (bestScore >> 16) & 15,
    (bestScore >> 12) & 15,
    (bestScore >> 8) & 15,
    (bestScore >> 4) & 15,
    bestScore & 15,
  ];
  const names = categoryName(category, ranks);
  const best = bestCombo.map((i) => hand[i]).sort((x, y) => x - y);
  return { score: bestScore, category, name: names.en, nameZh: names.zh, ranks, best };
}

export function compareScores(a, b) {
  if (a === b) return 0;
  return a < b ? -1 : 1;
}

function label(rank) {
  return RANKS[rank - 2] || "?";
}

export function categoryName(category, ranks) {
  if (!Number.isInteger(category) || category < 0 || category > 8) {
    throw new Error(`bad category: ${category}`);
  }
  const r = (i) => label((ranks && ranks[i]) || 0);
  switch (category) {
    case HIGH_CARD:
      return { en: `High card ${r(0)}`, zh: `${r(0)} 高牌` };
    case PAIR:
      return { en: `Pair of ${r(0)}`, zh: `一对 ${r(0)}` };
    case TWO_PAIR:
      return { en: `Two pair, ${r(0)} and ${r(1)}`, zh: `两对 ${r(0)} 与 ${r(1)}` };
    case TRIPS:
      return { en: `Three of a kind, ${r(0)}`, zh: `三条 ${r(0)}` };
    case STRAIGHT:
      return { en: `Straight, ${r(0)} high`, zh: `${r(0)} 高顺子` };
    case FLUSH:
      return { en: `Flush, ${r(0)} high`, zh: `${r(0)} 高同花` };
    case FULL_HOUSE:
      return { en: `Full house, ${r(0)} over ${r(1)}`, zh: `${r(0)} 葫芦` };
    case QUADS:
      return { en: `Four of a kind, ${r(0)}`, zh: `四条 ${r(0)}` };
    default:
      return { en: `Straight flush, ${r(0)} high`, zh: `同花顺 ${r(0)} 高` };
  }
}
