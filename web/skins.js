// Two curated skins built from the ChatGPT and Gemini asset sets.
// CSS variables are set on :root so the CSS and the card factory stay in sync.

const ROOT = "/assets/skins";

export const SKINS = {
  gilded: {
    label: "鎏金象牙",
    short: "鎏金",
    deck: "经典点阵",
    source: "ChatGPT",
    base: `${ROOT}/gilded`,
    cards: `${ROOT}/gilded/cards`,
    back: `${ROOT}/gilded/card-back.webp`,
    avatars: {
      fox: `${ROOT}/gilded/avatar-fox.webp`,
      panda: `${ROOT}/gilded/avatar-panda.webp`,
      tiger: `${ROOT}/gilded/avatar-tiger.webp`,
      owl: `${ROOT}/gilded/avatar-owl.webp`,
      octopus: `${ROOT}/gilded/avatar-octopus.webp`,
      unicorn: `${ROOT}/gilded/avatar-unicorn.webp`,
      cat: `${ROOT}/gilded/avatar-cat.webp`,
      wolf: `${ROOT}/gilded/avatar-wolf.webp`,
    },
    chips: {
      25: `${ROOT}/gilded/chip-25.webp`,
      100: `${ROOT}/gilded/chip-100.webp`,
      500: `${ROOT}/gilded/chip-500.webp`,
      1000: `${ROOT}/gilded/chip-1000.webp`,
      5000: `${ROOT}/gilded/chip-5000.webp`,
    },
    icons: {
      fold: `${ROOT}/gilded/icon-fold.webp`,
      check: `${ROOT}/gilded/icon-check.webp`,
      call: `${ROOT}/gilded/icon-call.webp`,
      raise: `${ROOT}/gilded/icon-raise.webp`,
      allin: `${ROOT}/gilded/icon-allin.webp`,
    },
    backgrounds: {
      felt: `${ROOT}/gilded/table-felt.webp`,
      lobby: `${ROOT}/gilded/bg-lobby.webp`,
      room: `${ROOT}/gilded/bg-room.webp`,
      settlement: `${ROOT}/gilded/bg-settlement.webp`,
    },
    markers: {
      dealerButton: `${ROOT}/gilded/dealer-button.webp`,
      emptySeat: `${ROOT}/gilded/empty-seat.webp`,
      sb: `${ROOT}/gilded/marker-sb.webp`,
      bb: `${ROOT}/gilded/marker-bb.webp`,
      allin: `${ROOT}/gilded/stamp-allin.webp`,
    },
    settlement: {
      trophy: `${ROOT}/gilded/trophy.webp`,
      keyCard: `${ROOT}/gilded/key-card.webp`,
      paid: `${ROOT}/gilded/stamp-paid.webp`,
    },
  },
  saintdenis: {
    label: "圣丹尼",
    short: "圣丹尼",
    deck: "大牌面",
    source: "Gemini",
    base: `${ROOT}/saintdenis`,
    cards: `${ROOT}/saintdenis/cards`,
    back: `${ROOT}/saintdenis/card-back.webp`,
    avatars: {
      fox: `${ROOT}/saintdenis/avatar-fox.webp`,
      panda: `${ROOT}/saintdenis/avatar-panda.webp`,
      tiger: `${ROOT}/saintdenis/avatar-tiger.webp`,
      owl: `${ROOT}/saintdenis/avatar-owl.webp`,
      octopus: `${ROOT}/saintdenis/avatar-octopus.webp`,
      unicorn: `${ROOT}/saintdenis/avatar-unicorn.webp`,
      cat: `${ROOT}/saintdenis/avatar-cat.webp`,
      wolf: `${ROOT}/saintdenis/avatar-wolf.webp`,
    },
    chips: {
      25: `${ROOT}/saintdenis/chip-25.webp`,
      100: `${ROOT}/saintdenis/chip-100.webp`,
      500: `${ROOT}/saintdenis/chip-500.webp`,
      1000: `${ROOT}/saintdenis/chip-1000.webp`,
      5000: `${ROOT}/saintdenis/chip-5000.webp`,
    },
    icons: {
      fold: `${ROOT}/saintdenis/icon-fold.webp`,
      check: `${ROOT}/saintdenis/icon-check.webp`,
      call: `${ROOT}/saintdenis/icon-call.webp`,
      raise: `${ROOT}/saintdenis/icon-raise.webp`,
      allin: `${ROOT}/saintdenis/icon-allin.webp`,
    },
    backgrounds: {
      felt: `${ROOT}/saintdenis/table-felt.webp`,
      lobby: `${ROOT}/saintdenis/bg-lobby.webp`,
      room: `${ROOT}/saintdenis/bg-room.webp`,
      settlement: `${ROOT}/saintdenis/bg-settlement.webp`,
    },
    markers: {
      dealerButton: `${ROOT}/saintdenis/dealer-button.webp`,
      emptySeat: `${ROOT}/saintdenis/empty-seat.webp`,
      sb: `${ROOT}/saintdenis/marker-sb.webp`,
      bb: `${ROOT}/saintdenis/marker-bb.webp`,
      allin: `${ROOT}/saintdenis/stamp-allin.webp`,
    },
    settlement: {
      trophy: `${ROOT}/saintdenis/trophy.webp`,
      keyCard: `${ROOT}/saintdenis/key-card.webp`,
      paid: `${ROOT}/saintdenis/stamp-paid.webp`,
    },
  },
};

const CSS_VARS = {
  felt: "--felt-art",
  lobby: "--bg-lobby",
  room: "--bg-room",
  settlement: "--bg-settlement",
};

export function getSkin() {
  const name = document.documentElement.dataset.skin;
  return SKINS[name] ? name : "gilded";
}

export function applySkin(name) {
  const skin = SKINS[name] ? name : "gilded";
  const data = SKINS[skin];
  const root = document.documentElement;
  root.dataset.skin = skin;
  const set = (key, value) => root.style.setProperty(key, `url("${value}")`);
  set("--card-back-art", data.back);
  for (const [k, v] of Object.entries(data.backgrounds)) set(CSS_VARS[k], v);
  set("--dealer-button", data.markers.dealerButton);
  set("--empty-seat", data.markers.emptySeat);
  set("--marker-sb", data.markers.sb);
  set("--marker-bb", data.markers.bb);
  set("--stamp-allin", data.markers.allin);
  for (const [k, v] of Object.entries(data.chips)) set(`--chip-${k}`, v);
  for (const [k, v] of Object.entries(data.icons)) set(`--icon-${k}`, v);
  for (const [k, v] of Object.entries(data.settlement)) {
    const varName = k === "keyCard" ? "--key-card" : k === "paid" ? "--stamp-paid" : "--trophy";
    set(varName, v);
  }
  return data;
}

export function skinAsset(key) {
  const data = SKINS[getSkin()];
  return data.avatars[key] || "";
}
