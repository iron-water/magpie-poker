// Playing-card DOM factory. No images: CSS + Unicode suit glyphs.
// Card strings follow docs/API.md: rank in 23456789TJQKA + suit in shdc.

const SUIT_GLYPH = { s: "♠", h: "♥", d: "♦", c: "♣" };
const SUIT_NAME = { s: "黑桃", h: "红桃", d: "方块", c: "梅花" };
const RANKS = "23456789TJQKA";
const SUITS = "shdc";
let fourColorDefault = false;

function parse(card) {
  if (typeof card === "number" && Number.isInteger(card) && card >= 0 && card < 52) {
    return { rank: RANKS[Math.floor(card / 4)], suit: SUITS[card % 4] };
  }
  const text = String(card ?? "");
  const rank = text[0]?.toUpperCase();
  const suit = text[1]?.toLowerCase();
  if (text.length === 2 && RANKS.includes(rank) && SUITS.includes(suit)) {
    return { rank, suit };
  }
  return null;
}

export function isCard(card) {
  return parse(card) !== null;
}

export function cardLabel(card) {
  const parsed = parse(card);
  if (!parsed) return "未知牌";
  return `${SUIT_NAME[parsed.suit]} ${parsed.rank}`;
}

export function setFourColor(on) {
  fourColorDefault = Boolean(on);
  document.documentElement.dataset.fourColor = on ? "1" : "0";
}

export function getFourColor() {
  return fourColorDefault;
}

export function emptyCard(options = {}) {
  const el = document.createElement("div");
  el.className = `card card--empty card--${options.size || "md"}`;
  el.setAttribute("aria-hidden", "true");
  return el;
}

export function cardBack(options = {}) {
  const el = document.createElement("div");
  el.className = `card card--back card--${options.size || "sm"}`;
  el.setAttribute("role", "img");
  el.setAttribute("aria-label", "暗牌");
  const mark = document.createElement("span");
  mark.className = "card-back-mark";
  mark.setAttribute("aria-hidden", "true");
  el.append(mark);
  return el;
}

// options: { size: "sm"|"md"|"lg", fourColor, back, faceDown, className }
export function cardEl(card, options = {}) {
  if (options.back || options.faceDown) return cardBack(options);
  const parsed = parse(card);
  if (!parsed) return emptyCard(options);
  const four = options.fourColor ?? fourColorDefault;
  const size = options.size || "md";
  const el = document.createElement("div");
  el.className =
    `card card--${size} suit-${parsed.suit}` +
    (four ? " card--four" : "") +
    (options.className ? ` ${options.className}` : "");
  el.setAttribute("role", "img");
  el.setAttribute("aria-label", cardLabel(card));

  const corner = document.createElement("span");
  corner.className = "card-corner";
  const rank = document.createElement("span");
  rank.className = "card-rank";
  rank.textContent = parsed.rank;
  const suit = document.createElement("span");
  suit.className = "card-suit";
  suit.textContent = SUIT_GLYPH[parsed.suit];
  corner.append(rank, suit);

  const pip = document.createElement("span");
  pip.className = "card-pip";
  pip.setAttribute("aria-hidden", "true");
  pip.textContent = SUIT_GLYPH[parsed.suit];

  const far = corner.cloneNode(true);
  far.classList.add("card-corner--far");
  far.setAttribute("aria-hidden", "true");

  el.append(corner, pip, far);
  return el;
}

export function cardsRow(cards, options = {}) {
  const row = document.createElement("div");
  row.className = `card-row card-row--${options.size || "sm"}`;
  for (const card of cards || []) row.append(cardEl(card, options));
  return row;
}
