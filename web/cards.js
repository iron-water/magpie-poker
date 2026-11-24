// Playing-card DOM factory. Each skin ships 52 pre-rendered faces plus a back;
// the DOM keeps the size/rounding/shadow, the image carries the art.
// Card strings follow docs/API.md: rank in 23456789TJQKA + suit in shdc.

const SUIT_GLYPH = { s: "♠", h: "♥", d: "♦", c: "♣" };
const SUIT_NAME = { s: "黑桃", h: "红桃", d: "方块", c: "梅花" };
const RANKS = "23456789TJQKA";
const SUITS = "shdc";

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

function currentSkin() {
  return document.documentElement.dataset.skin === "saintdenis" ? "saintdenis" : "gilded";
}

function faceUrl(card) {
  const p = parse(card);
  return `/assets/skins/${currentSkin()}/cards/${p.rank}${p.suit}.webp`;
}

function backUrl() {
  return `/assets/skins/${currentSkin()}/card-back.webp`;
}

function fallbackFace(el, parsed) {
  el.classList.add("card--fallback");
  el.textContent = "";
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
}

export function isCard(card) {
  return parse(card) !== null;
}

export function cardLabel(card) {
  const parsed = parse(card);
  if (!parsed) return "未知牌";
  return `${SUIT_NAME[parsed.suit]} ${parsed.rank === "T" ? "10" : parsed.rank}`;
}

export function emptyCard(options = {}) {
  const el = document.createElement("div");
  el.className = `card card--empty card--${options.size || "md"}`;
  el.setAttribute("aria-hidden", "true");
  return el;
}

export function cardBack(options = {}) {
  const el = document.createElement("div");
  el.className = `card card--back card--imgcard card--${options.size || "sm"} card--skin-${currentSkin()}`;
  el.setAttribute("role", "img");
  el.setAttribute("aria-label", "暗牌");
  const img = document.createElement("img");
  img.className = "card-img";
  img.src = backUrl();
  img.alt = "";
  img.draggable = false;
  img.addEventListener("error", () => {
    img.remove();
    const mark = document.createElement("span");
    mark.className = "card-back-mark";
    mark.setAttribute("aria-hidden", "true");
    el.append(mark);
  });
  el.append(img);
  return el;
}

// options: { size: "sm"|"md"|"lg", back, faceDown, className }
export function cardEl(card, options = {}) {
  if (options.back || options.faceDown) return cardBack(options);
  const parsed = parse(card);
  if (!parsed) return emptyCard(options);
  const size = options.size || "md";
  const el = document.createElement("div");
  el.className =
    `card card--${size} card--face card--imgcard card--skin-${currentSkin()}` +
    (options.className ? ` ${options.className}` : "");
  el.dataset.card = `${parsed.rank}${parsed.suit}`;
  el.dataset.suit = parsed.suit;
  el.setAttribute("role", "img");
  el.setAttribute("aria-label", cardLabel(card));

  const img = document.createElement("img");
  img.className = "card-img";
  img.src = faceUrl(card);
  img.alt = "";
  img.draggable = false;
  img.addEventListener("error", () => fallbackFace(el, parsed));
  el.append(img);
  return el;
}

export function cardsRow(cards, options = {}) {
  const row = document.createElement("div");
  row.className = `card-row card-row--${options.size || "sm"}`;
  for (const card of cards || []) row.append(cardEl(card, options));
  return row;
}
