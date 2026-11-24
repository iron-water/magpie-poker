import { createHash, createHmac, randomBytes } from "node:crypto";

export const RANKS = "23456789TJQKA";
export const SUITS = "shdc";

export function parseCard(text) {
  if (Number.isInteger(text) && text >= 0 && text < 52) return text;
  const s = String(text || "");
  const r = RANKS.indexOf(s[0]?.toUpperCase());
  const u = SUITS.indexOf(s[1]?.toLowerCase());
  if (s.length !== 2 || r < 0 || u < 0) throw new Error(`bad card: ${JSON.stringify(text)}`);
  return r * 4 + u;
}

export function formatCard(card) {
  const c = Number(card);
  if (!Number.isInteger(c) || c < 0 || c > 51) throw new Error(`bad card: ${card}`);
  return RANKS[Math.floor(c / 4)] + SUITS[c % 4];
}

export function rankOf(card) {
  return Math.floor(Number(card) / 4) + 2;
}

export function suitOf(card) {
  return Number(card) % 4;
}

export function newDeck() {
  return Array.from({ length: 52 }, (_, i) => i);
}

export function randomSeed() {
  return randomBytes(32).toString("hex");
}

export function commitSeed(seedHex) {
  return createHash("sha256").update(String(seedHex)).digest("hex");
}

// Deterministic Fisher-Yates. Every index draws a fresh 32-bit word from
// HMAC-SHA256(seed, handNo:counter) and rejects the biased tail.
export function seededShuffle(seedHex, handNo = 0) {
  const deck = newDeck();
  const seed = Buffer.from(String(seedHex), "utf8");
  let counter = 0;
  let pool = Buffer.alloc(0);
  let offset = 0;
  const word = () => {
    if (offset + 4 > pool.length) {
      pool = createHmac("sha256", seed).update(`${handNo}:${counter++}`).digest();
      offset = 0;
    }
    const n = pool.readUInt32BE(offset);
    offset += 4;
    return n;
  };
  const below = (n) => {
    const limit = Math.floor(0x100000000 / n) * n;
    let v;
    do v = word(); while (v >= limit);
    return v % n;
  };
  for (let i = deck.length - 1; i > 0; i--) {
    const j = below(i + 1);
    [deck[i], deck[j]] = [deck[j], deck[i]];
  }
  return deck;
}

// Combines every player's revealed seed so one player cannot pick the deck.
export function combineSeeds(seeds) {
  const list = (seeds || []).map(String).filter(Boolean).sort();
  const h = createHash("sha256");
  for (const s of list) h.update(s + "\n");
  return h.digest("hex");
}

export function cardsText(cards) {
  return (cards || []).map(formatCard).join(" ");
}
