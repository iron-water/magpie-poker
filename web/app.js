import { cardEl, cardBack, emptyCard } from "./cards.js";
import { applySkin, getSkin, skinAsset, SKINS } from "./skins.js";

const params = new URLSearchParams(location.search);
const DEMO = params.get("demo") === "1";
const AVATAR_ALIASES = {
  fox: "🦊",
  panda: "🐼",
  tiger: "🐯",
  frog: "🐸",
  wolf: "🐺",
  owl: "🦉",
  octopus: "🐙",
  unicorn: "🦄",
  cat: "🐱",
};
const AVATAR_KEYS = {
  "🦊": "fox",
  "🐼": "panda",
  "🐯": "tiger",
  "🐸": "frog",
  "🐺": "wolf",
  "🦉": "owl",
  "🐙": "octopus",
  "🦄": "unicorn",
  "🐱": "cat",
  fox: "fox",
  panda: "panda",
  tiger: "tiger",
  frog: "frog",
  wolf: "wolf",
  owl: "owl",
  octopus: "octopus",
  unicorn: "unicorn",
  cat: "cat",
};
const DIALOG_IDS = { invite: "modal-invite", settings: "modal-settings", log: "modal-log", confirm: "modal-confirm" };

let state = null;
let view = "lounge";
let selectedAvatar = "🦊";
let avatarTouched = false;
let pending = new Set();
let awaitingState = false;
let lastInvite = null;
let rebuyOpen = false;
let keysInfo = null;
let tailscaleInfo = null;
let healthInfo = null;
let demoBase = null;
let demoSettlementData = null;
let demoBotTimer = null;
let demoBotBudget = 0;
let confirmResolve = null;

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));

/* ============================== boot ============================== */

boot();

async function boot() {
  const requested = params.get("skin");
  let saved = "gilded";
  try {
    saved = localStorage.getItem("poker-skin") || "gilded";
  } catch {
    /* private mode */
  }
  applySkin(requested === "saintdenis" || requested === "gilded" ? requested : saved);
  document.documentElement.classList.add("use-art");
  tagAvatarOptions();
  updateSkinButtons();
  bindTopbar();
  bindLounge();
  bindActionBar();
  bindDialogs();
  bindKeyboard();
  startClock();

  if (DEMO) {
    try {
      const mod = await import("./demo-state.js");
      demoBase = structuredClone(mod.demoState);
      demoSettlementData = structuredClone(mod.demoSettlement);
      state = structuredClone(demoBase);
      if (params.get("seats") === "9" && state.table) {
        state.table.config.maxSeats = 9;
      }
      resetDemoDeadline(state.table);
      selectedAvatar = avatarOf(state.profile?.avatar);
      setConnection("demo", "演示模式");
      view = state.table?.settlement ? "settlement" : "table";
      render();
    } catch (error) {
      setConnection("error", "示例加载失败");
      view = "lounge";
      render();
      toast(`示例状态加载失败：${error.message}`, "error");
    }
    return;
  }

  try {
    const next = await request("/api/state");
    state = next;
    selectedAvatar = avatarOf(state.profile?.avatar);
    setConnection("open", "已连接");
    view = state.table ? (state.table.settlement ? "settlement" : "table") : "lounge";
    render();
  } catch (error) {
    setConnection("error", "未连接");
    render();
    toast(`无法连接本机牌局服务：${error.message}`, "error");
  }
  connectEvents();
}

function connectEvents() {
  if (DEMO) return;
  let events;
  try {
    events = new EventSource("/api/events");
  } catch (error) {
    setConnection("error", "连接失败");
    return;
  }
  events.onopen = () => setConnection("open", "已连接");
  events.addEventListener("state", (event) => {
    try {
      const next = JSON.parse(event.data);
      setConnection("open", "已连接");
      applyState(next);
    } catch {
      toast("收到无法解析的牌桌状态", "warn");
    }
  });
  events.addEventListener("notice", (event) => {
    let notice = null;
    try {
      notice = JSON.parse(event.data);
    } catch {
      notice = { level: "info", text: event.data };
    }
    const level = notice?.level === "error" || notice?.level === "warn" ? notice.level : "info";
    if (notice?.text) toast(String(notice.text), level);
  });
  events.onerror = () => setConnection("error", "重连中");
  window.addEventListener("beforeunload", () => events.close(), { once: true });
}

function applyState(next) {
  if (!next || typeof next !== "object") return;
  const previousTable = state?.table;
  state = next;
  awaitingState = false;
  if (state.profile && !avatarTouched) selectedAvatar = avatarOf(state.profile.avatar);
  if (!state.table) {
    view = "lounge";
  } else if (state.table.settlement && !previousTable?.settlement) {
    view = "settlement";
  } else if (view === "lounge") {
    view = "table";
  }
  render();
}

/* ============================== rendering ============================== */

function render() {
  const table = state?.table || null;
  if (!table) view = "lounge";
  if (table && view === "lounge") view = "table";
  if (view === "settlement" && !table?.settlement) view = "table";
  if (view === "table" && !table) view = "lounge";

  $("#lounge-view").hidden = view !== "lounge";
  $("#table-view").hidden = view !== "table" || !table;
  $("#settlement-view").hidden = view !== "settlement" || !table;

  renderTopbar();
  renderLounge();
  if (table) {
    renderTable();
    if (view === "settlement") renderSettlement();
  }
  if ($("#modal-log")?.open) renderLogs();
  applyPending();
  updateClock();
}

function renderTopbar() {
  const table = state?.table;
  const meta = $("#top-table");
  const parts = [];
  if (table) {
    parts.push(`<span class="meta-strong">${esc(table.name || "牌桌")}</span>`);
    parts.push(`<span class="meta-sep">·</span>`);
    parts.push(`<span>第 ${esc(table.handNo ?? 0)} 手</span>`);
    parts.push(`<span class="meta-sep meta-hide">·</span>`);
    parts.push(`<span class="meta-hide">盲注 ${fmtChips(table.config?.smallBlind)} / ${fmtChips(table.config?.bigBlind)}</span>`);
    const players = Array.isArray(table.seats) ? table.seats.length : 0;
    const maxSeats = Number(table.config?.maxSeats) || players;
    parts.push(`<span class="meta-pill">${players}${maxSeats > players ? ` / ${maxSeats}` : ""} 人</span>`);
    if (DEMO) parts.push(`<span class="meta-pill">演示</span>`);
  } else {
    parts.push(`<span class="meta-strong">德州扑克</span>`);
    parts.push(`<span class="meta-sep">·</span>`);
    parts.push(`<span>本机私密牌局</span>`);
    if (DEMO) parts.push(`<span class="meta-pill">演示</span>`);
  }
  meta.innerHTML = parts.join("");

  const hasTable = Boolean(table);
  $("#btn-invite").hidden = !(hasTable && (table.invite || lastInvite));
  $("#btn-log").hidden = !hasTable;
  $("#btn-leave").hidden = !hasTable;
  $("#btn-settlement").hidden = !table?.settlement;
  $("#btn-end").hidden = !(table && state?.me?.isHost && table.phase !== "ended");

  document.title = table ? `${table.name || "牌桌"} · 德州扑克` : "德州扑克 · 牌桌";
}

function renderLounge() {
  const profile = state?.profile || {};
  setUnlessActive("#lounge-name", profile.name || "");
  setUnlessActive("#lounge-gateway", profile.gatewayUrl || "");

  const hostName = $("#host-name");
  if (hostName && !hostName.value) {
    hostName.value = profile.name ? `${profile.name}的牌局` : "周五牌局";
  }
  updateUnitHint();
  applyAvatarSelection();

  const info = $("#lounge-host-info");
  const host = state?.host;
  const keys = state?.keys;
  const pieces = [];
  if (host) {
    pieces.push(
      `<span class="host-piece"><span class="${host.tailscale ? "dot-ok" : "dot-warn"}"></span>` +
        `Tailscale <strong>${host.tailscale ? "已连接" : "未连接"}</strong></span>`,
    );
    if (host.name) pieces.push(`<span class="host-piece">主机 <strong>${esc(host.name)}</strong></span>`);
    if (host.ip) pieces.push(`<span class="host-piece">IP <strong>${esc(host.ip)}</strong></span>`);
    if (host.dns) pieces.push(`<span class="host-piece">DNS <strong>${esc(host.dns)}</strong></span>`);
    if (host.serve) pieces.push(`<span class="host-piece">Serve <strong>${esc(host.serve)}</strong></span>`);
    if (host.error) pieces.push(`<span class="host-piece">Tailscale <strong>${esc(host.error)}</strong></span>`);
  } else {
    pieces.push(`<span class="host-piece">等待本机牌局服务…</span>`);
  }
  if (keys?.issuer?.name) {
    pieces.push(`<span class="host-piece">银行 key <strong>${esc(keys.issuer.name)}</strong></span>`);
  }
  info.innerHTML = pieces.join("");
}

function renderTable() {
  const table = state?.table;
  if (!table) return;
  const myId = table.me?.id || state.me?.id;
  const mySeatIndex = Number(table.me?.seat ?? state.me?.seat ?? 0);
  const seats = Array.isArray(table.seats) ? table.seats : [];
  const maxSeats = clampInt(Number(table.config?.maxSeats) || 9, 2, 9);

  renderSeats(table, seats, myId, mySeatIndex, maxSeats);
  renderBoard(table);
  renderPot(table);
  renderCenterMeta(table, myId);
  renderActionBar(table, myId);
}

function renderSeats(table, seats, myId, mySeatIndex, maxSeats) {
  const holder = $("#seats");
  holder.textContent = "";
  const bySeat = new Map(seats.map((seat) => [Number(seat.seat), seat]));
  const meIndex = ((mySeatIndex % maxSeats) + maxSeats) % maxSeats;

  for (let index = 0; index < maxSeats; index += 1) {
    const seat = bySeat.get(index);
    const angle = Math.PI / 2 - (((index - meIndex + maxSeats) % maxSeats) * 2 * Math.PI) / maxSeats;
    const x = 50 + Math.cos(angle) * 41;
    const y = 50 + Math.sin(angle) * 37;
    const node = document.createElement("div");
    node.className = "seat";
    node.style.left = `${x.toFixed(3)}%`;
    node.style.top = `${y.toFixed(3)}%`;
    node.dataset.seat = String(index);

    if (!seat) {
      node.classList.add("is-empty");
      node.innerHTML = `<div class="seat-inner"><span>空座 ${index + 1}</span></div>`;
      holder.append(node);
      continue;
    }

    const isMe = Boolean(seat.isMe || seat.id === myId);
    const isTurn = table.phase === "hand" && table.toAct === seat.id;
    const folded = Boolean(seat.folded);
    const allIn = Boolean(seat.allIn);
    const out = Boolean(seat.out);
    const sittingOut = Boolean(seat.sittingOut);
    if (isMe) node.classList.add("is-me");
    if (isTurn) node.classList.add("is-turn");
    if (folded) node.classList.add("is-folded");
    if (allIn) node.classList.add("is-allin");
    if (out) node.classList.add("is-out");
    if (sittingOut) node.classList.add("is-sittingout");

    const badges = [];
    if (seat.isDealer) badges.push(`<span class="badge badge-dealer" title="庄家">D</span>`);
    if (seat.isSB) badges.push(`<span class="badge badge-sb" title="小盲">SB</span>`);
    if (seat.isBB) badges.push(`<span class="badge badge-bb" title="大盲">BB</span>`);
    if (allIn) badges.push(`<span class="badge badge-allin">全下</span>`);
    if (folded) badges.push(`<span class="badge badge-folded">弃牌</span>`);
    if (out) badges.push(`<span class="badge badge-out">淘汰</span>`);
    if (sittingOut && !out) badges.push(`<span class="badge badge-folded">暂离</span>`);

    const lastAction = seat.lastAction || (sittingOut ? "暂离" : folded ? "已弃牌" : allIn ? "全下" : "");
    const labelParts = [
      seat.name || "玩家",
      `${fmtChips(seat.chips)} 筹码`,
      seat.isDealer ? "庄家" : "",
      seat.isSB ? "小盲" : "",
      seat.isBB ? "大盲" : "",
      folded ? "已弃牌" : "",
      allIn ? "全下" : "",
      out ? "已淘汰" : "",
    ].filter(Boolean);
    node.setAttribute("role", "group");
    node.setAttribute("aria-label", labelParts.join("，"));

    const avKey = avatarKey(seat.avatar);
    const avUrl = avKey ? skinAsset(avKey) : "";
    const avAttr = avUrl ? ` data-avatar-key="${avKey}" style="--avatar-art:url('${avUrl}')"` : "";
    node.innerHTML =
      `<div class="seat-tags">${badges.join("")}</div>` +
      `<div class="seat-inner">` +
      `<div class="avatar-wrap"><div class="avatar"${avAttr} aria-hidden="true">${esc(avatarOf(seat.avatar))}</div>` +
      `<div class="timer-ring" aria-hidden="true"></div></div>` +
      `<div class="seat-info">` +
      `<div class="seat-name">${esc(seat.name || "玩家")}${isMe ? `<span class="seat-you">你</span>` : ""}</div>` +
      `<div class="seat-chips">${fmtChips(seat.chips)}</div>` +
      `<div class="seat-last">${esc(lastAction)}</div>` +
      `</div><div class="seat-cards"></div></div>` +
      (Number(seat.bet) > 0
        ? `<div class="seat-bet" data-chip="${chipFor(seat.bet)}">${fmtChips(seat.bet)}</div>`
        : "");

    const cardsHolder = $(".seat-cards", node);
    const shownCards =
      Array.isArray(seat.cards) && seat.cards.length
        ? seat.cards
        : isMe && Array.isArray(table.me?.cards) && table.me.cards.length
          ? table.me.cards
          : null;
    if (shownCards && !folded && !out) {
      for (const card of shownCards) cardsHolder.append(cardEl(card, { size: "sm" }));
    } else if (!folded && !out && table.phase !== "lobby" && Number(seat.cardsCount) > 0) {
      const count = Math.min(2, Number(seat.cardsCount) || 2);
      for (let i = 0; i < count; i += 1) cardsHolder.append(cardBack({ size: "sm" }));
    }

    const ring = $(".timer-ring", node);
    if (ring && isTurn) {
      ring.style.setProperty("--p", `${seatTimePercent(table, seat).toFixed(2)}`);
    }
    holder.append(node);
  }
}

function renderBoard(table) {
  const holder = $("#board");
  holder.textContent = "";
  const board = Array.isArray(table.board) ? table.board : [];
  for (let index = 0; index < 5; index += 1) {
    holder.append(board[index] ? cardEl(board[index], { size: "lg" }) : emptyCard({ size: "lg" }));
  }
}

function renderPot(table) {
  const pot = Number(table.pot) || 0;
  $("#pot-amount").textContent = fmtChips(pot);
  $("#pot-fiat").textContent = fiatFromChips(pot, table.config);
  const pots = Array.isArray(table.pots) ? table.pots : [];
  const side = $("#pot-side");
  if (pots.length > 1) {
    side.innerHTML = pots
      .map((entry, index) => {
        const label = index === 0 ? "主池" : `边池 ${index}`;
        return `<span>${label} · ${fmtChips(entry.amount)}</span>`;
      })
      .join("");
  } else {
    side.textContent = "";
  }
}

function renderCenterMeta(table, myId) {
  const meta = $("#hand-meta");
  const config = table.config || {};
  const bits = [
    `第 ${table.handNo ?? 0} 手`,
    `盲注 ${fmtChips(config.smallBlind)} / ${fmtChips(config.bigBlind)}`,
  ];
  if (Number(config.ante) > 0) bits.push(`前注 ${fmtChips(config.ante)}`);
  bits.push(`${fmtChips(config.startingChips)} 带入`);
  meta.textContent = bits.join(" · ");

  const hint = $("#turn-hint");
  hint.className = "turn-hint";
  if (table.phase === "lobby") {
    hint.textContent = "等待开局";
  } else if (table.phase === "showdown") {
    hint.textContent = "摊牌";
    hint.classList.add("is-active");
  } else if (table.phase === "ended") {
    hint.textContent = "牌局已结束";
  } else if (table.phase === "hand") {
    const actor = table.seats?.find((seat) => seat.id === table.toAct);
    if (!actor) {
      hint.textContent = "等待发牌";
    } else {
      const seconds = secondsLeft(table, actor);
      const mine = actor.id === myId || actor.isMe;
      hint.textContent = mine ? `轮到你行动 · ${seconds}s` : `${actor.name || "对手"} 思考中 · ${seconds}s`;
      hint.classList.add(mine ? "is-mine" : "is-active");
    }
  }
}

function renderActionBar(table, myId) {
  const bar = $("#action-bar");
  const mySeat = table.seats?.find((seat) => seat.id === myId || seat.isMe);
  const legal = table.me?.legal;
  const myTurn = table.phase === "hand" && Boolean(legal) && table.toAct === myId;
  const canAct = myTurn && !mySeat?.folded && !mySeat?.out && !mySeat?.allIn;

  if (canAct) {
    renderActiveBar(bar, table, mySeat, legal);
    return;
  }
  if (rebuyOpen && table.phase === "hand") rebuyOpen = false;
  renderWaitingBar(bar, table, myId, mySeat);
}

function renderActiveBar(bar, table, mySeat, legal) {
  const callAmount = Math.max(0, Number(legal.call) || 0);
  const bounds = raiseBounds(table, legal, mySeat);
  const initial = bounds.hasRaise ? bounds.min : 0;
  const raiseLabel = bounds.callTo > 0 ? "加注到" : "下注";
  const label = legalLabel(table);
  const checkDisabled = legal.check ? "" : ' disabled data-locked="1"';
  const foldDisabled = legal.fold ? "" : ' disabled data-locked="1"';
  const callDisabled = callAmount > 0 ? "" : ' disabled data-locked="1"';
  const raiseDisabled = bounds.hasRaise ? "" : ' disabled data-locked="1"';

  bar.innerHTML =
    `<div class="action-hand">` +
    `<div class="card-row" id="action-hand-cards"></div>` +
    `<div class="action-hand-copy"><span class="action-hand-label">我的手牌</span>` +
    `<span class="action-hand-value">${esc(label)}</span></div></div>` +
    `<div class="action-main">` +
    `<button class="btn btn-fold" type="button" data-action="fold" data-pending-key="action" title="快捷键 F"${foldDisabled}>弃牌</button>` +
    `<button class="btn btn-check" type="button" data-action="check" data-pending-key="action" title="快捷键 C"${checkDisabled}>过牌</button>` +
    `<button class="btn btn-call" type="button" data-action="call" data-pending-key="action" title="快捷键 C"${callDisabled}>跟注 ${fmtChips(callAmount)}</button>` +
    `</div>`;

  if (bounds.hasRaise) {
    const raiseBlock = document.createElement("div");
    raiseBlock.className = "action-raise";
    raiseBlock.innerHTML =
      `<div class="raise-top">` +
      `<span class="raise-label">${raiseLabel}</span>` +
      `<input class="raise-slider" id="raise-slider" type="range" min="${bounds.min}" max="${bounds.max}" step="1" value="${initial}" aria-label="${raiseLabel}金额">` +
      `<input class="raise-number" id="raise-number" type="number" min="${bounds.min}" max="${bounds.max}" step="1" value="${initial}" inputmode="numeric" aria-label="${raiseLabel}金额">` +
      `<button class="btn btn-raise btn-sm" type="button" id="raise-submit" data-action="raise" data-pending-key="action"${raiseDisabled}>${esc(raiseButtonText(bounds, initial))}</button>` +
      `</div>` +
      `<div class="quick-row">` +
      `<button class="quick" type="button" data-quick="half">1/2 池</button>` +
      `<button class="quick" type="button" data-quick="pot">池</button>` +
      `<button class="quick quick-allin" type="button" data-quick="allin">全下</button>` +
      `<span class="keyboard-hint"><kbd>F</kbd> 弃牌 · <kbd>C</kbd> 过/跟 · <kbd>R</kbd> 加注</span>` +
      `</div>`;
    bar.append(raiseBlock);
  }

  const row = $("#action-hand-cards");
  const cards = Array.isArray(table.me?.cards) ? table.me.cards : Array.isArray(mySeat?.cards) ? mySeat.cards : [];
  if (cards.length) {
    for (const card of cards) row.append(cardEl(card, { size: "md" }));
  } else {
    row.append(cardBack({ size: "md" }), cardBack({ size: "md" }));
  }

  bindRaiseInputs(table, legal, mySeat);
}

function renderWaitingBar(bar, table, myId, mySeat) {
  const player = table.seats?.find((seat) => seat.id === table.toAct);
  const seconds = player ? secondsLeft(table, player) : 0;
  const isHost = Boolean(state.me?.isHost);
  let title = "";
  let sub = "";

  if (table.phase === "lobby") {
    title = isHost ? "牌桌已就绪" : "等待房主开局";
    sub = "把邀请码发给朋友，人齐就能开始。";
  } else if (table.phase === "showdown") {
    title = "本手结束";
    sub = isHost ? "确认结果后进入下一手，或结束牌局结算。" : "等待房主发下一手。";
  } else if (table.phase === "ended") {
    title = "牌局已结束";
    sub = "结算完成后，欠款方签发 key，收款方验证。";
  } else if (mySeat?.out) {
    title = "你已淘汰";
    sub = "可以补码后继续，或离桌。";
  } else if (mySeat?.sittingOut) {
    title = "你暂离牌桌";
    sub = "下一手开始前可以回到牌桌。";
  } else if (mySeat?.folded) {
    title = "你已弃牌";
    sub = "本手继续，等待其他玩家行动。";
  } else if (mySeat?.allIn) {
    title = "你已全下";
    sub = "等待公共牌与摊牌。";
  } else if (player) {
    title = `等待 ${player.name || "对手"} 行动`;
    sub = `第 ${table.handNo ?? 0} 手 · 盲注 ${fmtChips(table.config?.smallBlind)} / ${fmtChips(table.config?.bigBlind)}`;
  } else {
    title = "等待发牌";
    sub = "下一手即将开始。";
  }

  const hostButtons = [];
  if (isHost) {
    if (table.phase === "lobby") {
      hostButtons.push(
        `<button class="btn btn-primary btn-sm" type="button" data-host="start" data-pending-key="start">开始牌局</button>`,
      );
    }
    if (table.phase === "showdown") {
      hostButtons.push(
        `<button class="btn btn-primary btn-sm" type="button" data-host="next" data-pending-key="next">下一手</button>`,
      );
    }
    if (table.phase !== "ended") {
      hostButtons.push(
        `<button class="btn btn-ghost btn-sm" type="button" data-host="end" data-pending-key="end">结束牌局</button>`,
      );
    }
  }

  const rebuyDisabled = table.phase === "hand" ? ' disabled data-locked="1" title="两手之间才能补码"' : "";
  const rebuyValue = Math.max(1, Math.round((Number(table.config?.bigBlind) || 50) * 20));

  bar.innerHTML =
    `<div class="action-wait">` +
    `<div class="wait-copy"><span class="wait-title">${esc(title)}</span>` +
    `<span class="wait-sub">${esc(sub)}</span></div>` +
    (player ? `<span class="wait-timer" id="wait-timer">${seconds}s</span>` : "") +
    `</div>` +
    (hostButtons.length ? `<div class="host-controls">${hostButtons.join("")}</div>` : "") +
    `<div class="rebuy-wrap">` +
    `<button class="btn btn-ghost btn-sm rebuy-toggle" type="button" id="rebuy-toggle"${rebuyDisabled}>补码</button>` +
    (rebuyOpen && table.phase !== "hand"
      ? `<form class="rebuy-form" id="rebuy-form">` +
        `<input class="input" id="rebuy-amount" type="number" min="1" step="1" value="${rebuyValue}" inputmode="numeric" aria-label="补码筹码">` +
        `<button class="btn btn-primary btn-sm" type="submit" data-pending-key="rebuy">加入</button>` +
        `</form>`
      : "") +
    `</div>`;
}

/* ============================== actions ============================== */

async function sendAction(type, amount) {
  const table = state?.table;
  if (!table || table.phase !== "hand" || !table.me?.legal) return;
  if (pending.has("action") || awaitingState) return;
  pending.add("action");
  if (!DEMO) awaitingState = true;
  applyPending();
  try {
    if (DEMO) {
      const result = demoAction(type, amount);
      if (!result.ok) toast(result.error || "这个动作现在不可用", "warn");
      render();
      return;
    }
    const body = { type };
    if (amount !== undefined && amount !== null) body.amount = Math.round(Number(amount));
    const data = await request("/api/action", body);
    if (data?.state) applyState(data.state);
  } catch (error) {
    awaitingState = false;
    toast(error.message || "操作失败", "error");
  } finally {
    pending.delete("action");
    applyPending();
  }
}

async function commitRaise() {
  const table = state?.table;
  const legal = table?.me?.legal;
  const mySeat = table?.seats?.find((seat) => seat.isMe || seat.id === table.me?.id);
  if (!table || !legal || !mySeat) return;
  const bounds = raiseBounds(table, legal, mySeat);
  if (!bounds.hasRaise) return;
  const input = $("#raise-number");
  let value = Number(input?.value);
  if (!Number.isFinite(value)) value = bounds.min;
  value = clampInt(value, bounds.min, bounds.max);
  if (value < bounds.min || value > bounds.max) {
    toast(`下注需要在 ${fmtChips(bounds.min)} – ${fmtChips(bounds.max)} 之间`, "warn");
    return;
  }
  const isAllIn = value >= bounds.max && bounds.max >= bounds.fullStackTo;
  const type = isAllIn ? "allin" : bounds.callTo > 0 ? "raise" : "bet";
  await sendAction(type, value);
}

function raiseBounds(table, legal, mySeat) {
  const callTo = Math.max(0, Number(table.currentBet) || 0);
  const myBet = Math.max(0, Number(mySeat?.bet) || 0);
  const allIn = Math.max(0, Number(legal.allIn) || 0);
  let max = Number(legal.raise?.max);
  if (!Number.isFinite(max)) max = myBet + allIn;
  max = Math.max(0, Math.floor(max));
  let min = Number(legal.raise?.min);
  if (!Number.isFinite(min)) {
    if (typeof legal.bet === "number" && Number.isFinite(legal.bet)) min = legal.bet;
    else min = Math.max(callTo + (Number(table.config?.bigBlind) || 1), myBet + 1);
  }
  min = Math.ceil(min);
  if (min < callTo) min = callTo;
  if (min > max) min = max;
  const hasBet = legal.bet !== null && legal.bet !== undefined && legal.bet !== false;
  const canAggress = Boolean(legal.raise) || hasBet;
  return {
    hasRaise: canAggress && max > myBet && min > 0 && max >= min,
    min,
    max,
    callTo,
    myBet,
    fullStackTo: myBet + allIn,
  };
}

function bindRaiseInputs(table, legal, mySeat) {
  const slider = $("#raise-slider");
  const number = $("#raise-number");
  if (!slider || !number) return;
  const bounds = raiseBounds(table, legal, mySeat);
  const sync = (value) => {
    const next = clampInt(Number(value) || bounds.min, bounds.min, bounds.max);
    slider.value = String(next);
    number.value = String(next);
    const submit = $("#raise-submit");
    if (submit) submit.textContent = raiseButtonText(bounds, next);
  };
  slider.addEventListener("input", () => {
    number.value = slider.value;
    const submit = $("#raise-submit");
    if (submit) submit.textContent = raiseButtonText(bounds, Number(slider.value));
  });
  number.addEventListener("input", () => {
    const value = Number(number.value);
    if (Number.isFinite(value)) {
      slider.value = String(clampInt(value, bounds.min, bounds.max));
      const submit = $("#raise-submit");
      if (submit) submit.textContent = raiseButtonText(bounds, Number(slider.value));
    }
  });
  number.addEventListener("blur", () => sync(number.value));
}

function raiseButtonText(bounds, value) {
  if (value >= bounds.max) return `全下 ${fmtChips(bounds.max)}`;
  return `${bounds.callTo > 0 ? "加注到" : "下注"} ${fmtChips(value)}`;
}

function legalLabel(table) {
  const hand = table.me?.handStrength;
  if (hand?.label) return hand.label;
  if (table.phase === "hand") return "等待行动";
  return "—";
}

/* ============================== settlement ============================== */

function renderSettlement() {
  const table = state?.table;
  const settlement = table?.settlement;
  if (!settlement) return;
  const transfers = Array.isArray(settlement.transfers) ? settlement.transfers : [];
  const myId = state?.me?.id || table.me?.id;
  const config = table.config || {};
  const pendingCount = transfers.filter((item) => item.status !== "verified").length;

  $("#settle-sub").textContent =
    `${table.name || "牌桌"} · 第 ${table.handNo ?? 0} 手 · ${transfers.length} 笔转账 · ${pendingCount} 笔未结清`;

  const toIssue = transfers.filter(
    (t) => t.from === myId && (t.status === "pending" || t.status === "failed"),
  ).length;
  const toVerify = transfers.filter(
    (t) => t.to === myId && t.key && t.status !== "verified",
  ).length;
  const issueAll = $("#settle-issue-all");
  const verifyAll = $("#settle-verify-all");
  if (issueAll) {
    issueAll.hidden = toIssue === 0;
    issueAll.textContent = `一键签发（${toIssue}）`;
  }
  if (verifyAll) {
    verifyAll.hidden = toVerify === 0;
    verifyAll.textContent = `一键验证（${toVerify}）`;
  }

  const unit = transfers[0]?.unit || config.unit || "cost";
  const totalOut = transfers.filter((t) => t.from === myId).reduce((sum, t) => sum + (Number(t.amount) || 0), 0);
  const totalIn = transfers.filter((t) => t.to === myId).reduce((sum, t) => sum + (Number(t.amount) || 0), 0);
  $("#settle-summary").innerHTML =
    `<div class="summary-cell"><div class="summary-label">转账</div><div class="summary-value">${transfers.length} <small>笔</small></div></div>` +
    `<div class="summary-cell"><div class="summary-label">你需支付</div><div class="summary-value">${esc(formatAmount(totalOut, unit, config.currency))}</div></div>` +
    `<div class="summary-cell"><div class="summary-label">你应收</div><div class="summary-value">${esc(formatAmount(totalIn, unit, config.currency))}</div></div>`;

  const holder = $("#settle-cards");
  holder.textContent = "";
  if (!transfers.length) {
    holder.innerHTML = `<div class="settle-empty">本局没有需要结算的转账。</div>`;
    return;
  }

  for (const transfer of transfers) {
    holder.append(settlementCard(transfer, myId, config));
  }
}

function settlementCard(transfer, myId, config) {
  const isPayer = transfer.from === myId;
  const isPayee = transfer.to === myId;
  const status = transfer.status || "pending";
  const hasKey = Boolean(transfer.key);
  const card = document.createElement("article");
  card.className = "transfer-card";
  card.dataset.status = status;

  const role = isPayer
    ? `<span class="transfer-role role-pay">你需支付</span>`
    : isPayee
      ? `<span class="transfer-role">你应收</span>`
      : "";
  const keyHelp = isPayee
    ? hasKey
      ? "key 只显示给你"
      : "等待对方签发"
    : isPayer
      ? status === "issued" || status === "verified"
        ? "已签发，收款方可见"
        : "key 只发给收款人，本机不显示"
      : "仅交易双方可见";

  const keyHtml =
    `<div class="key-box">` +
    `<div class="key-label"><span>GATEWAY KEY</span><span>${esc(keyHelp)}</span></div>` +
    (hasKey
      ? `<code class="key-value">${esc(transfer.key)}</code>` +
        `<div class="key-actions">` +
        `<button class="btn btn-outline btn-sm" type="button" data-copy-value="${esc(transfer.key)}">复制 key</button>` +
        `<button class="btn btn-ghost btn-sm" type="button" data-copy-transfer="${esc(transfer.id)}">复制接入信息</button>` +
        `</div>`
      : `<span class="key-value is-empty">${isPayee ? "尚未收到 key" : "签发后由收款方验证"}</span>`) +
    `</div>`;

  const actions = [];
  if (isPayer && (status === "pending" || status === "failed")) {
    actions.push(
      `<button class="btn btn-primary btn-sm" type="button" data-issue="${esc(transfer.id)}" data-pending-key="settle:${esc(transfer.id)}">签发 key</button>`,
    );
    if (!state.profile?.gatewayUrl) {
      actions.push(
        `<button class="btn btn-ghost btn-sm" type="button" data-share-gateway data-pending-key="share-gateway">共享网关</button>`,
      );
    }
  }
  if (isPayee && hasKey && status !== "verified") {
    actions.push(
      `<button class="btn btn-primary btn-sm" type="button" data-verify="${esc(transfer.id)}" data-pending-key="settle:${esc(transfer.id)}">验证 key</button>`,
    );
  }
  if (actions.length === 0) {
    if (status === "verified") actions.push(`<span class="inline-status ok">已结清</span>`);
    else if (status === "failed") actions.push(`<span class="inline-status warn">等待重新签发</span>`);
    else if (status === "issued") actions.push(`<span class="inline-status">等待验证</span>`);
  }

  card.innerHTML =
    `<div class="transfer-head">` +
    `<div class="transfer-parties">` +
    `<div class="party"><span class="party-dot" aria-hidden="true">${esc(initialOf(transfer.fromName))}</span>` +
    `<span class="party-name">${esc(transfer.fromName || "玩家")}</span></div>` +
    `<span class="arrow" aria-hidden="true">→</span>` +
    `<div class="party"><span class="party-dot" aria-hidden="true">${esc(initialOf(transfer.toName))}</span>` +
    `<span class="party-name">${esc(transfer.toName || "玩家")}</span></div>` +
    `</div>` +
    `<span class="status-badge" data-status="${esc(status)}">${esc(statusLabel(status, isPayer, isPayee))}</span>` +
    `</div>` +
    `<div class="transfer-amount"><strong>${esc(formatAmount(transfer.amount, transfer.unit || config.unit, config.currency))}</strong>` +
    `<span>${fmtChips(transfer.amountChips)} 筹码</span>${role}</div>` +
    keyHtml +
    `<div class="transfer-meta">` +
    (transfer.expires ? `<span>到期 ${esc(formatExpiry(transfer.expires))}</span>` : "") +
    (transfer.error ? `<span class="transfer-error">${esc(transfer.error)}</span>` : "") +
    `</div>` +
    (actions.length ? `<div class="transfer-actions">${actions.join("")}</div>` : "");
  return card;
}

function askConfirm(text, title = "确认") {
  if (confirmResolve) return Promise.resolve(false);
  return new Promise((resolve) => {
    confirmResolve = resolve;
    $("#confirm-title").textContent = title;
    $("#confirm-text").textContent = text;
    const dialog = $("#modal-confirm");
    if (dialog && !dialog.open) dialog.showModal();
  });
}

function finishConfirm(value) {
  const resolve = confirmResolve;
  confirmResolve = null;
  const dialog = $("#modal-confirm");
  if (dialog?.open) dialog.close();
  if (resolve) resolve(value);
}

async function issueTransfer(id) {
  const result = await post("/api/settlement/issue", { transferId: id }, `settle:${id}`);
  if (result.ok) toast("已签发 key，等待对方验证", "info");
}

async function verifyTransfer(id) {
  const result = await post("/api/settlement/verify", { transferId: id }, `settle:${id}`);
  if (result.ok) toast("key 验证通过，这笔已结清", "info");
}

async function bulkIssue() {
  const transfers = state?.table?.settlement?.transfers || [];
  const myId = state?.me?.id;
  const ids = transfers
    .filter((t) => t.from === myId && (t.status === "pending" || t.status === "failed"))
    .map((t) => t.id);
  if (!ids.length) return;
  const btn = $("#settle-issue-all");
  let done = 0;
  for (const id of ids) {
    if (btn) btn.textContent = `签发中 ${done + 1}/${ids.length}…`;
    const result = await post("/api/settlement/issue", { transferId: id }, "settle-issue-all");
    if (!result.ok) break;
    done += 1;
  }
  if (btn) btn.textContent = `一键签发（${Math.max(0, ids.length - done)}）`;
  if (done) toast(`已签发 ${done} 笔 key`, "info");
}

async function bulkVerify() {
  const transfers = state?.table?.settlement?.transfers || [];
  const myId = state?.me?.id;
  const ids = transfers
    .filter((t) => t.to === myId && t.key && t.status !== "verified")
    .map((t) => t.id);
  if (!ids.length) return;
  const btn = $("#settle-verify-all");
  let done = 0;
  for (const id of ids) {
    if (btn) btn.textContent = `验证中 ${done + 1}/${ids.length}…`;
    const result = await post("/api/settlement/verify", { transferId: id }, "settle-verify-all");
    if (!result.ok) break;
    done += 1;
  }
  if (btn) btn.textContent = `一键验证（${Math.max(0, ids.length - done)}）`;
  if (done) toast(`已验证 ${done} 笔 key`, "info");
}

async function shareGateway() {
  const result = await post("/api/tailscale/serve-gateway", { port: 8444 }, "share-gateway");
  if (result.ok) toast(`网关已共享：${result.url}`, "info");
}

function copyTransferInfo(id) {
  const transfer = state?.table?.settlement?.transfers?.find((t) => t.id === id);
  if (!transfer?.key) {
    toast("还没有收到 key", "warn");
    return;
  }
  const url = transfer.gatewayUrl || state?.profile?.gatewayUrl || "(对方没有填网关地址)";
  copyText(
    `网关地址：${url}\nAPI key：${transfer.key}\n用法：OpenAI 兼容客户端，base URL 指向网关（通常加 /v1），API key 填上一行。`,
    "接入信息已复制",
  );
}

function statusLabel(status, isPayer, isPayee) {
  if (status === "pending") return isPayee ? "待对方签发" : "待签发";
  if (status === "issued") return isPayee ? "待验证" : "已签发";
  if (status === "failed") return "失败";
  if (status === "small") return "低于最小额";
  if (status === "verified") return "已验证";
  return String(status || "未知");
}

/* ============================== host / table lifecycle ============================== */

async function startHand() {
  const result = await post("/api/table/start", {}, "start");
  if (result.ok) {
    view = "table";
    render();
  }
}

async function nextHand() {
  const result = await post("/api/table/next", {}, "next");
  if (result.ok) {
    view = "table";
    render();
  }
}

async function endTable() {
  const table = state?.table;
  if (!table) return;
  if (!(await askConfirm("结束牌局并进入结算？", "结束牌局"))) return;
  const result = await post("/api/table/end", {}, "end");
  if (result.ok) toast("正在生成结算…", "info");
}

async function leaveTable() {
  const table = state?.table;
  if (!table) return;
  const host = Boolean(state.me?.isHost);
  if (host && table.phase === "hand") {
    toast("先打完这一手再离桌", "warn");
    return;
  }
  const unsettled = (table.settlement?.transfers || []).filter(
    (t) => t.status === "pending" || t.status === "issued",
  ).length;
  let message = host ? "你是房主，离桌会解散牌局。" : "";
  if (unsettled) message += `还有 ${unsettled} 笔未结清，离桌后不会自动完成。`;
  message += "确认离桌？";
  if (!(await askConfirm(message, "离桌"))) return;
  const result = await post("/api/table/leave", { force: unsettled > 0 }, "leave");
  if (result.ok && DEMO) {
    view = "lounge";
    render();
  }
}

async function submitRebuy() {
  const input = $("#rebuy-amount");
  const amount = Number(input?.value);
  if (!Number.isInteger(amount) || amount <= 0) {
    toast("补码数量需要是正整数", "warn");
    return;
  }
  const result = await post("/api/table/rebuy", { amount }, "rebuy");
  if (result.ok) {
    rebuyOpen = false;
    toast(`已补入 ${fmtChips(amount)} 筹码`, "info");
    render();
  }
}

/* ============================== lounge handlers ============================== */

function bindLounge() {
  $("#host-form").addEventListener("submit", onHostSubmit);
  $("#host-form").addEventListener("input", updateUnitHint);
  $("#host-unit").addEventListener("change", () => {
    const unit = $("#host-unit").value;
    const input = $("#host-chip-value");
    const current = Number(input.value);
    const otherDefault = unit === "tokens" ? 0.0005 : 100;
    if (!Number.isFinite(current) || current <= 0 || current === otherDefault) {
      input.value = unit === "tokens" ? "100" : "0.0005";
    }
    updateUnitHint();
  });
  $("#join-form").addEventListener("submit", onJoinSubmit);
  $("#lounge-profile-form").addEventListener("submit", (event) => {
    event.preventDefault();
    saveProfile("#lounge-name", "#lounge-gateway");
  });
  document.addEventListener("click", (event) => {
    const avatarButton = event.target.closest?.("[data-avatar]");
    if (avatarButton) {
      selectedAvatar = avatarButton.dataset.avatar || "🦊";
      avatarTouched = true;
      applyAvatarSelection();
      return;
    }
    const copyButton = event.target.closest?.("[data-copy-value]");
    if (copyButton) {
      copyText(copyButton.dataset.copyValue || "", "已复制");
      return;
    }
    const closeButton = event.target.closest?.("[data-close]");
    if (closeButton) {
      const dialog = document.getElementById(DIALOG_IDS[closeButton.dataset.close]);
      if (dialog?.open) dialog.close();
    }
  });
}

async function onHostSubmit(event) {
  event.preventDefault();
  const form = event.currentTarget;
  if (!form.reportValidity()) return;
  const name = ($("#host-name").value || "").trim() || `${state?.profile?.name || "我的"}牌局`;
  const sb = Number($("#host-sb").value);
  const bb = Number($("#host-bb").value);
  const ante = Number($("#host-ante").value);
  const starting = Number($("#host-chips").value);
  const maxSeats = Number($("#host-seats").value);
  const seconds = Number($("#host-seconds").value);
  const unit = $("#host-unit").value === "tokens" ? "tokens" : "cost";
  const chipValue = Number($("#host-chip-value").value);

  if (!Number.isFinite(sb) || !Number.isFinite(bb) || sb < 1 || bb < sb) {
    toast("大盲需要大于等于小盲", "warn");
    return;
  }
  if (!Number.isFinite(starting) || starting < bb * 2) {
    toast("带入至少是两个大盲", "warn");
    return;
  }
  if (!Number.isFinite(chipValue) || chipValue <= 0) {
    toast("筹码价值需要大于 0", "warn");
    return;
  }
  if (!Number.isFinite(maxSeats) || maxSeats < 2 || maxSeats > 9) {
    toast("座位数需要在 2 – 9 之间", "warn");
    return;
  }

  const config = {
    smallBlind: Math.round(sb),
    bigBlind: Math.round(bb),
    ante: Math.max(0, Math.round(Number.isFinite(ante) ? ante : 0)),
    startingChips: Math.round(starting),
    maxSeats: Math.round(maxSeats),
    unit,
    chipValue,
    currency: unit === "tokens" ? "TOKENS" : "USD",
    actionSeconds: clampInt(Number(seconds) || 30, 5, 300),
  };
  const result = await post("/api/table/host", { name, config }, "host");
  if (result.ok) {
    lastInvite = result.invite || state?.table?.invite || lastInvite;
    toast("牌桌已创建", "info");
    if (lastInvite) showInvite(lastInvite);
  }
}

async function onJoinSubmit(event) {
  event.preventDefault();
  const raw = ($("#join-code").value || "").trim();
  const code = extractInviteCode(raw);
  if (!code) {
    toast("先粘贴邀请码或带邀请码的链接", "warn");
    return;
  }
  const result = await post("/api/table/join", { code }, "join");
  if (result.ok) toast("已加入牌桌", "info");
}

async function saveProfile(nameSelector, gatewaySelector) {
  const name = ($(nameSelector).value || "").trim();
  if (!name) {
    toast("昵称不能为空", "warn");
    return { ok: false };
  }
  const gatewayUrl = ($(gatewaySelector).value || "").trim();
  const result = await post("/api/profile", { name, avatar: selectedAvatar, gatewayUrl }, "profile");
  if (result.ok) {
    avatarTouched = false;
    toast("资料已保存", "info");
  }
  return result;
}

function updateUnitHint() {
  const hint = $("#host-unit-hint");
  if (!hint) return;
  const unit = $("#host-unit").value;
  const chipValue = Number($("#host-chip-value").value) || 0;
  const chips = Number($("#host-chips").value) || 10000;
  const sample = chips * chipValue;
  if (unit === "tokens") {
    hint.textContent = `1 筹码 = ${trimNumber(chipValue)} tokens · ${fmtChips(chips)} 筹码 ≈ ${trimNumber(sample)} tokens`;
  } else {
    hint.textContent = `1 筹码 = $${trimNumber(chipValue)} · ${fmtChips(chips)} 筹码 ≈ $${sample.toFixed(2)}`;
  }
}

/* ============================== action bar handlers ============================== */

function bindActionBar() {
  const bar = $("#action-bar");
  bar.addEventListener("click", (event) => {
    const button = event.target.closest?.("button");
    if (!button) return;
    if (button.dataset.action) {
      const action = button.dataset.action;
      if (action === "raise") commitRaise();
      else sendAction(action);
      return;
    }
    if (button.dataset.host === "start") {
      startHand();
      return;
    }
    if (button.dataset.host === "next") {
      nextHand();
      return;
    }
    if (button.dataset.host === "end") {
      endTable();
      return;
    }
    if (button.dataset.quick) {
      const table = state?.table;
      const mySeat = table?.seats?.find((seat) => seat.isMe || seat.id === table.me?.id);
      const legal = table?.me?.legal;
      if (!table || !mySeat || !legal) return;
      const bounds = raiseBounds(table, legal, mySeat);
      let value = bounds.min;
      if (button.dataset.quick === "allin") {
        value = bounds.max;
      } else {
        const fraction = button.dataset.quick === "half" ? 0.5 : 1;
        const base = bounds.callTo > 0 ? bounds.callTo : 0;
        value = base + Math.round((Number(table.pot) || 0) * fraction);
      }
      value = clampInt(value, bounds.min, bounds.max);
      const slider = $("#raise-slider");
      const number = $("#raise-number");
      if (slider) slider.value = String(value);
      if (number) number.value = String(value);
      const submit = $("#raise-submit");
      if (submit) submit.textContent = raiseButtonText(bounds, value);
      return;
    }
    if (button.id === "rebuy-toggle") {
      rebuyOpen = !rebuyOpen;
      renderActionBar(state.table, state.me?.id || state.table.me?.id);
      applyPending();
    }
  });

  bar.addEventListener("submit", (event) => {
    if (event.target.id === "rebuy-form") {
      event.preventDefault();
      submitRebuy();
    }
  });
}

/* ============================== dialogs ============================== */

function bindDialogs() {
  $("#btn-invite").addEventListener("click", () => showInvite());
  $("#btn-settlement").addEventListener("click", () => setView("settlement"));
  $("#btn-settings").addEventListener("click", openSettings);
  $("#btn-log").addEventListener("click", openLog);
  $("#btn-leave").addEventListener("click", leaveTable);
  $("#btn-end").addEventListener("click", endTable);
  $("#confirm-ok").addEventListener("click", () => finishConfirm(true));
  $("#confirm-cancel").addEventListener("click", () => finishConfirm(false));
  $("#modal-confirm").addEventListener("close", () => {
    if (confirmResolve) {
      const resolve = confirmResolve;
      confirmResolve = null;
      resolve(false);
    }
  });
  $("#settle-back").addEventListener("click", () => setView("table"));
  $("#settle-issue-all").addEventListener("click", bulkIssue);
  $("#settle-verify-all").addEventListener("click", bulkVerify);
  $("#settle-cards").addEventListener("click", (event) => {
    if (event.target.closest?.("[data-share-gateway]")) {
      shareGateway();
      return;
    }
    const info = event.target.closest?.("[data-copy-transfer]");
    if (info) {
      copyTransferInfo(info.dataset.copyTransfer);
      return;
    }
    const issue = event.target.closest?.("[data-issue]");
    if (issue) {
      issueTransfer(issue.dataset.issue);
      return;
    }
    const verify = event.target.closest?.("[data-verify]");
    if (verify) verifyTransfer(verify.dataset.verify);
  });

  $("#copy-code").addEventListener("click", () => copyText($("#invite-code").value, "邀请码已复制"));
  $("#copy-direct").addEventListener("click", () => copyText($("#invite-direct").value, "直连地址已复制"));

  $("#settings-profile-form").addEventListener("submit", (event) => {
    event.preventDefault();
    saveProfile("#settings-name", "#settings-gateway");
  });
  $("#health-check").addEventListener("click", checkHealth);
  $("#btn-tailscale-serve").addEventListener("click", () => serveTailscale("table"));
  $("#btn-tailscale-gateway").addEventListener("click", () => serveTailscale("gateway"));
  $("#btn-bank-key").addEventListener("click", createBankKey);
  $("#verify-form").addEventListener("submit", onVerifyKey);
  $("#chat-form").addEventListener("submit", onChat);

  for (const id of Object.values(DIALOG_IDS)) {
    const dialog = document.getElementById(id);
    if (!dialog) continue;
    dialog.addEventListener("click", (event) => {
      if (event.target === dialog) dialog.close();
    });
  }
}

function bindTopbar() {
  $("#btn-skin").addEventListener("click", () => {
    setActiveSkin(getSkin() === "gilded" ? "saintdenis" : "gilded");
  });
  document.addEventListener("click", (event) => {
    const option = event.target.closest?.("[data-skin-choice]");
    if (option) setActiveSkin(option.dataset.skinChoice);
  });
  for (const id of ["modal-invite", "modal-settings", "modal-log"]) {
    const dialog = document.getElementById(id);
    if (!dialog) continue;
    dialog.addEventListener("close", () => {
      if (id === "modal-settings") $("#settings-key-list")?.replaceChildren();
    });
  }
}

function showInvite(invite) {
  const value = invite || state?.table?.invite || lastInvite;
  if (!value?.code) {
    toast("邀请码还没生成，稍等一下", "warn");
    return;
  }
  lastInvite = value;
  $("#invite-code").value = value.code || "";
  const direct = value.direct || value.served || "";
  $("#invite-direct").value = direct;
  $("#invite-served").textContent = value.served ? `Serve：${value.served}` : "";
  const dialog = $("#modal-invite");
  if (dialog && !dialog.open) dialog.showModal();
}

function openLog() {
  if (!state?.table) {
    toast("还没有牌桌日志", "warn");
    return;
  }
  renderLogs();
  const dialog = $("#modal-log");
  if (dialog && !dialog.open) dialog.showModal();
  requestAnimationFrame(() => {
    const list = $("#log-list");
    if (list) list.scrollTop = list.scrollHeight;
  });
}

function renderLogs() {
  const list = $("#log-list");
  if (!list) return;
  const logs = Array.isArray(state?.table?.log) ? state.table.log : [];
  list.textContent = "";
  const kindLabels = { system: "系统", deal: "发牌", action: "行动", chat: "聊天", result: "结果" };
  for (const entry of logs) {
    const item = document.createElement("li");
    item.className = "log-item";
    item.dataset.kind = entry.kind || "system";
    const time = document.createElement("span");
    time.className = "log-time";
    time.textContent = formatClock(entry.at);
    const kind = document.createElement("span");
    kind.className = "log-kind";
    kind.textContent = kindLabels[entry.kind] || entry.kind || "系统";
    const text = document.createElement("span");
    text.className = "log-text";
    text.textContent = entry.text || "";
    item.append(time, kind, text);
    list.append(item);
  }
  if (!logs.length) {
    const item = document.createElement("li");
    item.className = "log-item";
    item.innerHTML = `<span class="log-time"></span><span class="log-kind"></span><span class="log-text">还没有记录</span>`;
    list.append(item);
  }
}

async function onChat(event) {
  event.preventDefault();
  const input = $("#chat-text");
  const text = (input.value || "").trim();
  if (!text) return;
  const result = await post("/api/chat", { text }, "chat");
  if (result.ok) {
    input.value = "";
    renderLogs();
    const list = $("#log-list");
    if (list) list.scrollTop = list.scrollHeight;
  }
}

function openSettings() {
  const profile = state?.profile || {};
  $("#settings-name").value = profile.name || "";
  $("#settings-gateway").value = profile.gatewayUrl || "";
  applyAvatarSelection();
  renderSettingsData();
  const dialog = $("#modal-settings");
  if (dialog && !dialog.open) dialog.showModal();
  loadSettingsData();
}

async function loadSettingsData() {
  if (DEMO) {
    keysInfo = state?.keys || {};
    tailscaleInfo = {
      installed: Boolean(state?.host?.tailscale),
      name: state?.host?.name || "",
      ip: state?.host?.ip || "",
      dns: state?.host?.dns || "",
      serve: state?.host?.serve || "",
      serveGateway: state?.host?.serveGateway || "",
    };
    healthInfo = { ok: true, uptime: 3725, version: state?.version || "0.1.0" };
    renderSettingsData();
    return;
  }
  const [keysResult, tailscaleResult, healthResult] = await Promise.allSettled([
    request("/api/keys"),
    request("/api/tailscale"),
    request("/api/health"),
  ]);
  keysInfo =
    keysResult.status === "fulfilled"
      ? keysResult.value
      : { magpie: false, error: keysResult.reason?.message || "读取失败" };
  tailscaleInfo =
    tailscaleResult.status === "fulfilled"
      ? tailscaleResult.value
      : { installed: false, error: tailscaleResult.reason?.message || "读取失败" };
  healthInfo =
    healthResult.status === "fulfilled"
      ? healthResult.value
      : { ok: false, error: healthResult.reason?.message || "读取失败" };
  renderSettingsData();
}

function renderSettingsData() {
  const keys = keysInfo || state?.keys || {};
  const tailscale = tailscaleInfo || {};
  const health = healthInfo || null;

  const healthStatus = $("#health-status");
  if (healthStatus) {
    healthStatus.innerHTML = health
      ? `<div><dt>服务</dt><dd class="${health.ok === false ? "warn" : "ok"}">${health.ok === false ? "不可用" : "正常"}</dd></div>` +
        `<div><dt>版本</dt><dd>${esc(health.version || state?.version || "—")}</dd></div>` +
        `<div><dt>运行</dt><dd>${esc(formatUptime(health.uptime))}</dd></div>` +
        (health.error ? `<div><dt>错误</dt><dd class="warn">${esc(health.error)}</dd></div>` : "")
      : `<div><dt>状态</dt><dd>未检查</dd></div>`;
  }

  const tailStatus = $("#settings-tailscale-status");
  if (tailStatus) {
    const installed = tailscale.installed === true;
    tailStatus.innerHTML =
      `<div><dt>Tailscale</dt><dd class="${installed ? "ok" : "warn"}">${installed ? "已安装" : "未连接或未安装"}</dd></div>` +
      (tailscale.name ? `<div><dt>主机</dt><dd>${esc(tailscale.name)}</dd></div>` : "") +
      (tailscale.ip ? `<div><dt>IP</dt><dd>${esc(tailscale.ip)}</dd></div>` : "") +
      (tailscale.dns ? `<div><dt>DNS</dt><dd>${esc(tailscale.dns)}</dd></div>` : "") +
      `<div><dt>Serve</dt><dd>${esc(tailscale.serve || tailscale.serveGateway || "未开启")}</dd></div>` +
      (tailscale.error ? `<div><dt>错误</dt><dd class="warn">${esc(tailscale.error)}</dd></div>` : "");
  }

  const keyStatus = $("#settings-keys-status");
  if (keyStatus) {
    const magpie = keys.magpie === true;
    keyStatus.innerHTML =
      `<div><dt>magpie CLI</dt><dd class="${magpie ? "ok" : "warn"}">${magpie ? `已就绪 · ${esc(keys.bin || "magpie")}` : "未找到"}</dd></div>` +
      `<div><dt>issuer</dt><dd>${keys.issuer?.name ? `${esc(keys.issuer.name)} <span class="muted">${esc(keys.issuer.id || "")}</span>` : "未设置"}</dd></div>` +
      (keys.error ? `<div><dt>错误</dt><dd class="warn">${esc(keys.error)}</dd></div>` : "");
  }

  const keyList = $("#settings-key-list");
  if (keyList) {
    const all = Array.isArray(keys.all) ? keys.all : [];
    keyList.innerHTML = all.length
      ? all
          .map(
            (key) =>
              `<div class="key-item"><span class="key-name">${esc(key.name || key.id || "key")}</span>` +
              `<span class="key-state">${esc(key.state || "")}${key.limit ? ` · ${esc(key.limit)}` : ""}</span></div>`,
          )
          .join("")
      : `<div class="key-item"><span class="key-state">还没有 gateway key</span></div>`;
  }

  lockButton($("#btn-tailscale-serve"), DEMO || tailscale.installed !== true, DEMO ? "演示模式不支持" : "Tailscale 未就绪");
  lockButton($("#btn-tailscale-gateway"), DEMO || tailscale.installed !== true, DEMO ? "演示模式不支持" : "Tailscale 未就绪");
  lockButton($("#btn-bank-key"), DEMO || keys.magpie !== true, DEMO ? "演示模式不支持" : "magpie 未就绪");
  applyPending();
}

function lockButton(button, locked, title) {
  if (!button) return;
  if (locked) {
    button.dataset.locked = "1";
    if (title) button.title = title;
  } else {
    delete button.dataset.locked;
    button.removeAttribute("title");
  }
}

async function checkHealth() {
  if (DEMO) {
    healthInfo = { ok: true, uptime: 3725, version: state?.version || "0.1.0" };
    renderSettingsData();
    toast("演示模式：本机服务状态为本示例", "info");
    return;
  }
  try {
    healthInfo = await request("/api/health");
    toast("本机牌局服务正常", "info");
  } catch (error) {
    healthInfo = { ok: false, error: error.message };
    toast(`服务检查失败：${error.message}`, "error");
  }
  renderSettingsData();
}

async function serveTailscale(kind) {
  if (DEMO) {
    toast("演示模式不支持 Tailscale 操作", "warn");
    return;
  }
  const path = kind === "gateway" ? "/api/tailscale/serve-gateway" : "/api/tailscale/serve";
  const port = kind === "gateway" ? 8444 : 8443;
  const result = await post(path, { port }, kind === "gateway" ? "tailscale-gateway" : "tailscale-serve");
  if (result.ok) {
    toast(`Tailscale Serve 已更新（:${port}）`, "info");
    loadSettingsData();
  }
}

async function createBankKey() {
  if (DEMO) {
    toast("演示模式不支持签发真实 key", "warn");
    return;
  }
  const result = await post("/api/keys/bank", {}, "bank-key");
  if (result.ok) {
    toast("poker-bank key 已设为 issuer", "info");
    loadSettingsData();
  }
}

async function onVerifyKey(event) {
  event.preventDefault();
  const url = ($("#verify-url").value || "").trim();
  const key = ($("#verify-key").value || "").trim();
  const status = $("#verify-status");
  if (!url || !key) {
    if (status) {
      status.textContent = "网关地址和 key 都需要填写";
      status.className = "inline-status warn";
    }
    return;
  }
  const result = await post("/api/keys/verify", { url, key }, "verify-key");
  if (status) {
    status.textContent = result.ok ? "验证通过" : result.error || "验证失败";
    status.className = `inline-status ${result.ok ? "ok" : "warn"}`;
  }
  if (result.ok) toast("对方 key 可用", "info");
}

/* ============================== keyboard & clock ============================== */

function bindKeyboard() {
  document.addEventListener("keydown", (event) => {
    if (event.defaultPrevented || event.repeat) return;
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (document.querySelector("dialog[open]")) return;
    if (event.target.closest?.("input, textarea, select, [contenteditable='true']")) return;
    if (view !== "table" || !state?.table || state.table.phase !== "hand") return;
    const legal = state.table.me?.legal;
    if (!legal) return;
    const key = String(event.key || "").toLowerCase();
    if (key === "f" && legal.fold) {
      event.preventDefault();
      sendAction("fold");
    } else if (key === "c") {
      if (legal.check) {
        event.preventDefault();
        sendAction("check");
      } else if (Number(legal.call) > 0) {
        event.preventDefault();
        sendAction("call");
      }
    } else if (key === "r") {
      event.preventDefault();
      commitRaise();
    }
  });
}

function startClock() {
  window.setInterval(updateClock, 250);
}

function updateClock() {
  const table = state?.table;
  if (!table) return;
  const actor = table.phase === "hand" ? table.seats?.find((seat) => seat.id === table.toAct) : null;
  const total = Math.max(1, (Number(table.config?.actionSeconds) || 30) * 1000);
  const leftMs = table.deadline ? Math.max(0, Number(table.deadline) - Date.now()) : 0;
  const left = table.deadline ? Math.ceil(leftMs / 1000) : Math.max(0, Number(actor?.timeLeft) || 0);

  for (const node of $$(".seat")) {
    if (!node.classList.contains("is-turn")) continue;
    const ring = $(".timer-ring", node);
    if (!ring) continue;
    const percent = table.deadline
      ? Math.max(0, Math.min(100, (leftMs / total) * 100))
      : Math.max(0, Math.min(100, (left / (total / 1000)) * 100));
    ring.style.setProperty("--p", percent.toFixed(2));
    ring.classList.toggle("is-urgent", leftMs > 0 && leftMs < 7000);
  }

  const hint = $("#turn-hint");
  if (hint) {
    if (table.phase === "hand" && actor) {
      const mine = actor.id === table.me?.id || actor.isMe;
      hint.textContent = mine ? `轮到你行动 · ${left}s` : `${actor.name || "对手"} 思考中 · ${left}s`;
    } else if (table.phase === "showdown") {
      hint.textContent = "摊牌";
    }
  }

  const wait = $("#wait-timer");
  if (wait && actor) wait.textContent = `${left}s`;

  const seatNodes = $$(".seat.is-turn .seat-last");
  for (const node of seatNodes) {
    const seat = table.seats?.find((entry) => entry.id === table.toAct);
    if (seat) node.textContent = seat.lastAction || "";
  }
}

/* ============================== pending / view ============================== */

function applyPending() {
  for (const button of $$("[data-pending-key]")) {
    const key = button.dataset.pendingKey;
    const busy = pending.has(key) || (key === "action" && awaitingState);
    const locked = button.dataset.locked === "1";
    button.disabled = busy || locked;
    button.classList.toggle("is-busy", busy);
    button.setAttribute("aria-busy", busy ? "true" : "false");
  }
}

function setView(next) {
  if (next === "settlement" && !state?.table?.settlement) return;
  if (next === "table" && !state?.table) return;
  view = next;
  render();
}

/* ============================== API ============================== */

async function request(path, body) {
  const options = {
    method: body === undefined ? "GET" : "POST",
    cache: "no-store",
    headers: { accept: "application/json" },
  };
  if (body !== undefined) {
    options.headers["content-type"] = "application/json";
    options.body = JSON.stringify(body);
  }
  const response = await fetch(path, options);
  const text = await response.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!response.ok || data?.ok === false) {
    throw new Error(data?.error || `请求失败（HTTP ${response.status}）`);
  }
  return data ?? { ok: true };
}

async function post(path, body, key = "post") {
  if (pending.has(key)) return { ok: false, error: "请求进行中" };
  pending.add(key);
  applyPending();
  try {
    const data = DEMO ? await demoPost(path, body) : await request(path, body);
    if (data?.ok === false) throw new Error(data.error || "请求失败");
    if (data?.state && !DEMO) applyState(data.state);
    if (DEMO) render();
    return data ?? { ok: true };
  } catch (error) {
    toast(error.message || "请求失败", "error");
    return { ok: false, error: error.message };
  } finally {
    pending.delete(key);
    applyPending();
  }
}

/* ============================== demo adapter ============================== */

function demoAction(type, amount) {
  const table = state?.table;
  if (!table || table.phase !== "hand") return { ok: false, error: "当前不在手牌中" };
  const mySeat = table.seats?.find((seat) => seat.isMe || seat.id === table.me?.id);
  const legal = table.me?.legal;
  if (!mySeat || !legal) return { ok: false, error: "还没轮到你" };
  clearTimeout(demoBotTimer);
  demoBotTimer = null;
  const now = Date.now();

  if (type === "fold") {
    mySeat.folded = true;
    mySeat.lastAction = "弃牌";
    table.log.push({ at: now, kind: "action", text: `${mySeat.name} 弃牌` });
  } else if (type === "check") {
    if (!legal.check) return { ok: false, error: "现在不能过牌" };
    mySeat.lastAction = "过牌";
    table.log.push({ at: now, kind: "action", text: `${mySeat.name} 过牌` });
  } else if (type === "call") {
    const toCall = Math.max(0, Number(legal.call) || 0);
    if (toCall <= 0) return { ok: false, error: "现在没有需要跟注的金额" };
    const pay = Math.min(toCall, mySeat.chips);
    mySeat.chips -= pay;
    mySeat.bet += pay;
    mySeat.committed += pay;
    mySeat.lastAction = pay < toCall ? `全下 ${pay}` : `跟注 ${pay}`;
    if (mySeat.chips === 0) mySeat.allIn = true;
    table.log.push({ at: now, kind: "action", text: `${mySeat.name} ${mySeat.lastAction}` });
  } else if (type === "bet" || type === "raise" || type === "allin") {
    const maxTo = mySeat.bet + mySeat.chips;
    let target = Number(amount);
    if (!Number.isFinite(target)) target = maxTo;
    target = clampInt(target, Math.min(mySeat.bet + 1, maxTo), maxTo);
    const delta = Math.min(target - mySeat.bet, mySeat.chips);
    if (delta <= 0) return { ok: false, error: "下注金额无效" };
    const previousBet = Number(table.currentBet) || 0;
    mySeat.chips -= delta;
    mySeat.bet += delta;
    mySeat.committed += delta;
    if (mySeat.chips === 0) mySeat.allIn = true;
    if (mySeat.bet > previousBet) {
      const raiseSize = Math.max(Number(table.config?.bigBlind) || 1, mySeat.bet - previousBet);
      table.currentBet = mySeat.bet;
      table.minRaiseTo = mySeat.bet + raiseSize;
    }
    mySeat.lastAction = mySeat.allIn ? `全下 ${mySeat.bet}` : `${type === "bet" ? "下注" : "加注到"} ${mySeat.bet}`;
    table.log.push({ at: now, kind: "action", text: `${mySeat.name} ${mySeat.lastAction}` });
  } else {
    return { ok: false, error: "未知动作" };
  }

  table.pot = sumCommitted(table);
  table.pots = computePots(table);
  table.me.legal = null;
  table.me.handStrength = null;
  table.toAct = null;
  table.deadline = null;
  demoBotBudget = table.seats.filter((seat) => !seat.isMe && !seat.folded && !seat.out && !seat.allIn).length;
  demoAdvance(table, mySeat.seat);
  return { ok: true };
}

function demoAdvance(table, fromSeat) {
  const seats = table.seats || [];
  const live = seats.filter((seat) => !seat.folded && !seat.out && !seat.allIn);
  if (live.length <= 1) {
    demoShowdown(table);
    return;
  }
  const size = clampInt(Number(table.config?.maxSeats) || 9, 2, 9);
  let target = null;
  for (let step = 1; step <= size; step += 1) {
    const index = (fromSeat + step) % size;
    const candidate = seats.find((seat) => Number(seat.seat) === index);
    if (candidate && !candidate.folded && !candidate.out && !candidate.allIn && !candidate.sittingOut) {
      target = candidate;
      break;
    }
  }
  if (!target) {
    demoShowdown(table);
    return;
  }
  table.toAct = target.id;
  table.deadline = Date.now() + (Number(table.config?.actionSeconds) || 30) * 1000;
  if (target.isMe) {
    table.me.legal = demoLegalFor(table, target);
    table.me.handStrength = table.me.handStrength || { label: "等待行动", category: 0, score: 0 };
    return;
  }
  if (demoBotBudget <= 0) {
    demoShowdown(table);
    return;
  }
  scheduleDemoBot();
}

function demoLegalFor(table, seat) {
  const currentBet = Math.max(0, Number(table.currentBet) || 0);
  const bet = Math.max(0, Number(seat.bet) || 0);
  const chips = Math.max(0, Number(seat.chips) || 0);
  const toCall = Math.max(0, currentBet - bet);
  const maxTo = bet + chips;
  const bb = Math.max(1, Number(table.config?.bigBlind) || 50);
  let minTo = currentBet > 0 ? Math.max(Number(table.minRaiseTo) || 0, currentBet + bb) : Math.min(bb, maxTo);
  if (minTo > maxTo) minTo = maxTo;
  const canRaise = maxTo > currentBet && maxTo > bet;
  return {
    fold: true,
    check: toCall === 0,
    call: toCall,
    bet: currentBet === 0 ? minTo : null,
    raise: canRaise && currentBet > 0 ? { min: minTo, max: maxTo } : null,
    allIn: chips,
  };
}

function scheduleDemoBot() {
  if (!DEMO) return;
  clearTimeout(demoBotTimer);
  const table = state?.table;
  if (!table || table.phase !== "hand") return;
  const actor = table.seats?.find((seat) => seat.id === table.toAct);
  if (!actor || actor.isMe || actor.folded || actor.allIn) return;
  demoBotTimer = window.setTimeout(() => {
    const currentTable = state?.table;
    if (!currentTable || currentTable.phase !== "hand") return;
    const current = currentTable.seats?.find((seat) => seat.id === currentTable.toAct);
    if (!current || current.isMe) return;
    const toCall = Math.max(0, (Number(currentTable.currentBet) || 0) - (Number(current.bet) || 0));
    const pay = Math.min(toCall, current.chips);
    current.chips -= pay;
    current.bet += pay;
    current.committed += pay;
    if (current.chips === 0) current.allIn = true;
    current.lastAction = pay > 0 ? `跟注 ${pay}` : "过牌";
    currentTable.log.push({
      at: Date.now(),
      kind: "action",
      text: `${current.name} ${current.lastAction}`,
    });
    currentTable.pot = sumCommitted(currentTable);
    currentTable.pots = computePots(currentTable);
    currentTable.toAct = null;
    currentTable.deadline = null;
    demoBotBudget -= 1;
    demoAdvance(currentTable, current.seat);
    render();
  }, 1100);
}

function demoShowdown(table) {
  clearTimeout(demoBotTimer);
  demoBotTimer = null;
  table.phase = "showdown";
  table.toAct = null;
  table.deadline = null;
  table.me.legal = null;
  table.showdown = null;
  table.log.push({ at: Date.now(), kind: "system", text: "本手结束" });
}

function sumCommitted(table) {
  return (table.seats || []).reduce((sum, seat) => sum + (Number(seat.committed) || 0), 0);
}

function computePots(table) {
  const seats = (table.seats || []).filter((seat) => (Number(seat.committed) || 0) > 0);
  const levels = Array.from(new Set(seats.map((seat) => Number(seat.committed) || 0))).sort((a, b) => a - b);
  const pots = [];
  let previous = 0;
  for (const level of levels) {
    const contributors = seats.filter((seat) => (Number(seat.committed) || 0) >= level);
    const amount = (level - previous) * contributors.length;
    if (amount > 0) {
      pots.push({
        amount,
        eligible: contributors.filter((seat) => !seat.folded && !seat.out).map((seat) => seat.id),
      });
    }
    previous = level;
  }
  return pots;
}

function demoLobbyTable(name, config) {
  const base = structuredClone(demoBase.table);
  const meSeat = {
    ...structuredClone(base.seats[0]),
    chips: config.startingChips,
    bet: 0,
    committed: 0,
    folded: false,
    allIn: false,
    out: false,
    sittingOut: false,
    isDealer: true,
    isSB: false,
    isBB: false,
    cards: null,
    cardsCount: 0,
    lastAction: "",
    isMe: true,
    timeLeft: null,
  };
  return {
    ...base,
    name,
    phase: "lobby",
    handNo: 0,
    config,
    dealerSeat: 0,
    toAct: null,
    currentBet: 0,
    minRaiseTo: config.bigBlind,
    pot: 0,
    pots: [],
    board: [],
    seats: [meSeat],
    me: { id: state.me?.id || "p_mio", seat: 0, cards: null, legal: null, handStrength: null },
    log: [{ at: Date.now(), kind: "system", text: `${name} 已创建，等待开局` }],
    showdown: null,
    deadline: null,
  };
}

function demoStartTable(table) {
  const config = table?.config ? { ...table.config } : structuredClone(demoBase.table.config);
  const name = table?.name || demoBase.table.name;
  const handNo = Math.max(1, (Number(table?.handNo) || 0) + 1);
  clearTimeout(demoBotTimer);
  demoBotTimer = null;
  const next = structuredClone(demoBase.table);
  next.name = name;
  next.config = config;
  next.handNo = handNo;
  next.phase = "hand";
  next.settlement = undefined;
  next.showdown = null;
  next.deadline = Date.now() + (Number(config.actionSeconds) || 30) * 1000;
  next.log = [
    { at: Date.now(), kind: "system", text: `${name} 第 ${handNo} 手开始，盲注 ${fmtChips(config.smallBlind)} / ${fmtChips(config.bigBlind)}` },
    ...(next.log || []),
  ];
  state.table = next;
  state.mode = "host";
  if (state.me) state.me.isHost = true;
  view = "table";
}

function demoEndTable(table) {
  clearTimeout(demoBotTimer);
  demoBotTimer = null;
  table.phase = "ended";
  table.toAct = null;
  table.deadline = null;
  table.me.legal = null;
  const settlement = structuredClone(demoSettlementData);
  settlement.at = Date.now();
  settlement.transfers = settlement.transfers.map((transfer) => ({
    ...transfer,
    unit: table.config?.unit || "cost",
  }));
  table.settlement = settlement;
  table.showdown = {
    revealed: [
      {
        id: "p_mio",
        name: "Mio",
        cards: ["Ah", "Kh"],
        hand: "两对，A 与 K",
        won: 0,
      },
    ],
    pots: [{ amount: table.pot, winners: [] }],
  };
  table.log.push({ at: Date.now(), kind: "result", text: "牌局结束，进入结算" });
  view = "settlement";
}

function demoPostFailure(message) {
  return { ok: false, error: message };
}

async function demoPost(path, body) {
  const table = state?.table;
  const me = state?.me;
  switch (path) {
    case "/api/profile": {
      const name = String(body?.name || "").trim() || state.profile?.name || "Mio";
      const avatar = String(body?.avatar || selectedAvatar || "🦊");
      if (state.profile) {
        state.profile.name = name;
        state.profile.avatar = avatar;
        state.profile.gatewayUrl = String(body?.gatewayUrl || "");
      }
      if (me) me.name = name;
      if (table?.seats) {
        for (const seat of table.seats) {
          if (seat.isMe || seat.id === me?.id) {
            seat.name = name;
            seat.avatar = avatar;
          }
        }
      }
      selectedAvatar = avatarOf(avatar);
      avatarTouched = false;
      return { ok: true };
    }
    case "/api/table/host": {
      const name = String(body?.name || "").trim() || "周五牌局";
      const config = body?.config || structuredClone(demoBase.table.config);
      state.mode = "host";
      if (me) me.isHost = true;
      if (state.profile && body?.name) state.profile.name = state.profile.name || me?.name;
      state.table = demoLobbyTable(name, config);
      view = "table";
      return { ok: true, invite: state.table.invite };
    }
    case "/api/table/join": {
      const code = String(body?.code || "").trim();
      if (!code) return demoPostFailure("邀请码为空");
      const next = structuredClone(demoBase.table);
      next.name = "周五牌局";
      next.phase = "hand";
      next.deadline = Date.now() + (Number(next.config.actionSeconds) || 30) * 1000;
      state.table = next;
      state.mode = "guest";
      if (me) me.isHost = false;
      view = "table";
      return { ok: true };
    }
    case "/api/table/leave": {
      clearTimeout(demoBotTimer);
      demoBotTimer = null;
      state.table = null;
      state.mode = "idle";
      view = "lounge";
      rebuyOpen = false;
      return { ok: true };
    }
    case "/api/table/start": {
      if (!me?.isHost) return demoPostFailure("只有房主可以开始");
      demoStartTable(table);
      return { ok: true };
    }
    case "/api/table/next": {
      if (!me?.isHost) return demoPostFailure("只有房主可以发下一手");
      demoStartTable(table);
      return { ok: true };
    }
    case "/api/table/end": {
      if (!me?.isHost) return demoPostFailure("只有房主可以结束牌局");
      if (!table) return demoPostFailure("没有牌桌");
      demoEndTable(table);
      return { ok: true };
    }
    case "/api/table/rebuy": {
      if (!table) return demoPostFailure("没有牌桌");
      const amount = Math.round(Number(body?.amount));
      if (!Number.isInteger(amount) || amount <= 0) return demoPostFailure("补码数量无效");
      const mySeat = table.seats?.find((seat) => seat.isMe || seat.id === me?.id);
      if (!mySeat) return demoPostFailure("你不在牌桌上");
      mySeat.chips += amount;
      mySeat.out = false;
      mySeat.folded = false;
      mySeat.sittingOut = false;
      table.log.push({ at: Date.now(), kind: "system", text: `${mySeat.name} 补码 ${fmtChips(amount)}` });
      return { ok: true };
    }
    case "/api/action": {
      return demoAction(body?.type, body?.amount);
    }
    case "/api/chat": {
      const text = String(body?.text || "").trim();
      if (!text) return demoPostFailure("消息为空");
      if (table) table.log.push({ at: Date.now(), kind: "chat", text: `${me?.name || "我"}：${text}` });
      return { ok: true };
    }
    case "/api/settlement/issue": {
      const transfer = table?.settlement?.transfers?.find((item) => item.id === body?.transferId);
      if (!transfer) return demoPostFailure("找不到这笔转账");
      if (transfer.from !== me?.id) return demoPostFailure("只有欠款方本机可以签发");
      transfer.status = "issued";
      transfer.error = null;
      return { ok: true };
    }
    case "/api/settlement/verify": {
      const transfer = table?.settlement?.transfers?.find((item) => item.id === body?.transferId);
      if (!transfer) return demoPostFailure("找不到这笔转账");
      if (transfer.to !== me?.id) return demoPostFailure("只有收款方本机可以验证");
      if (!transfer.key) return demoPostFailure("还没有收到 key");
      transfer.status = "verified";
      transfer.error = null;
      return { ok: true };
    }
    case "/api/keys/bank": {
      state.keys = state.keys || { magpie: true, all: [] };
      state.keys.magpie = true;
      const issuer = { id: `demo-${Date.now().toString(36)}`, name: body?.id || "poker-bank" };
      state.keys.issuer = issuer;
      state.keys.all = [issuer, ...(state.keys.all || [])];
      return { ok: true, issuer };
    }
    case "/api/keys/verify": {
      if (!body?.url || !body?.key) return demoPostFailure("地址和 key 都需要");
      return { ok: true, url: body.url };
    }
    case "/api/tailscale/serve": {
      if (state.host) state.host.serve = `https://${state.host.dns || "host"}:${body?.port || 8443}`;
      return { ok: true };
    }
    case "/api/tailscale/serve-gateway": {
      if (state.host) state.host.serveGateway = `https://${state.host.dns || "host"}:${body?.port || 8444}`;
      return { ok: true };
    }
    case "/api/health":
      return { ok: true, uptime: 3725, version: state?.version || "0.1.0" };
    default:
      return demoPostFailure("演示模式暂不支持这个操作");
  }
}

function resetDemoDeadline(table) {
  if (!table) return;
  if (table.phase === "hand") {
    const actor = table.seats?.find((seat) => seat.id === table.toAct);
    const seconds = Math.max(1, Number(actor?.timeLeft) || Number(table.config?.actionSeconds) || 30);
    table.deadline = Date.now() + seconds * 1000;
  }
}

/* ============================== helpers ============================== */

function setConnection(kind, text) {
  const box = $("#conn");
  const label = $("#conn-text");
  if (box) box.dataset.state = kind;
  if (label) {
    label.textContent =
      text ||
      (kind === "open"
        ? "已连接"
        : kind === "demo"
          ? "演示模式"
          : kind === "error"
            ? "重连中"
            : "连接中");
  }
}


function applyAvatarSelection() {
  for (const button of $$("[data-avatar]")) {
    button.classList.toggle("is-selected", button.dataset.avatar === selectedAvatar);
    button.setAttribute("aria-pressed", button.dataset.avatar === selectedAvatar ? "true" : "false");
  }
}

function setUnlessActive(selector, value) {
  const input = $(selector);
  if (!input) return;
  if (document.activeElement !== input && input.value !== value) input.value = value;
}

function extractInviteCode(text) {
  const raw = String(text || "").trim();
  if (!raw) return "";
  const query = raw.match(/[?&#]code=([^&\s]+)/i);
  if (query) {
    try {
      return decodeURIComponent(query[1]);
    } catch {
      return query[1];
    }
  }
  return raw;
}

async function copyText(text, message) {
  const value = String(text || "");
  if (!value) {
    toast("没有可复制的内容", "warn");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    toast(message || "已复制", "info");
    return;
  } catch {
    /* fallback below */
  }
  const area = document.createElement("textarea");
  area.value = value;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.left = "-9999px";
  area.style.opacity = "0";
  document.body.append(area);
  area.select();
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } catch {
    copied = false;
  }
  area.remove();
  toast(copied ? message || "已复制" : "复制失败，请手动选择文本", copied ? "info" : "warn");
}

function toast(text, level = "info") {
  const holder = $("#toasts");
  if (!holder) return;
  const node = document.createElement("div");
  node.className = "toast";
  node.dataset.level = level;
  const mark = document.createElement("span");
  mark.className = "toast-mark";
  mark.setAttribute("aria-hidden", "true");
  mark.textContent = level === "error" ? "!" : level === "warn" ? "△" : "♠";
  const body = document.createElement("span");
  body.textContent = String(text || "");
  node.append(mark, body);
  holder.append(node);
  while (holder.children.length > 4) holder.firstElementChild.remove();
  window.setTimeout(() => {
    node.classList.add("is-leaving");
    window.setTimeout(() => node.remove(), 220);
  }, 4200);
}

function avatarKey(value) {
  return AVATAR_KEYS[String(value ?? "")] || "";
}

function tagAvatarOptions() {
  for (const button of $$("[data-avatar]")) {
    const key = avatarKey(button.dataset.avatar);
    const url = key ? skinAsset(key) : "";
    if (url) button.style.setProperty("--avatar-art", `url("${url}")`);
    else button.style.removeProperty("--avatar-art");
  }
}

function setActiveSkin(name) {
  if (!SKINS[name]) return;
  applySkin(name);
  try {
    localStorage.setItem("poker-skin", name);
  } catch {
    /* private mode */
  }
  tagAvatarOptions();
  updateSkinButtons();
  render();
}

function updateSkinButtons() {
  const current = getSkin();
  const button = $("#btn-skin");
  if (button) button.textContent = SKINS[current].short;
  for (const option of $$("[data-skin-choice]")) {
    option.classList.toggle("is-selected", option.dataset.skinChoice === current);
  }
}

function chipFor(amount) {
  const n = Number(amount) || 0;
  if (n >= 5000) return "chip-5000";
  if (n >= 1000) return "chip-1000";
  if (n >= 500) return "chip-500";
  if (n >= 100) return "chip-100";
  return "chip-25";
}

function avatarOf(value) {
  const text = String(value || "").trim();
  if (!text) return "♠";
  if (AVATAR_ALIASES[text.toLowerCase()]) return AVATAR_ALIASES[text.toLowerCase()];
  return Array.from(text).length <= 4 ? text : "♠";
}

function initialOf(name) {
  const text = String(name || "").trim();
  return text ? Array.from(text)[0].toUpperCase() : "?";
}

function fmtChips(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "0";
  return Math.round(number).toLocaleString("en-US");
}

function trimNumber(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "0";
  return number.toLocaleString("en-US", { maximumFractionDigits: 4 });
}

function currencySymbol(currency) {
  const code = String(currency || "USD").toUpperCase();
  if (code === "USD") return "$";
  if (code === "CNY" || code === "RMB" || code === "JPY") return "¥";
  if (code === "EUR") return "€";
  if (code === "GBP") return "£";
  return `${code} `;
}

function formatAmount(amount, unit, currency) {
  const number = Number(amount);
  if (!Number.isFinite(number)) return "—";
  if (unit === "tokens") return `${trimNumber(number)} tokens`;
  return `${currencySymbol(currency)}${number.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  })}`;
}

function fiatFromChips(chips, config) {
  if (!config || !Number.isFinite(Number(config.chipValue))) return "";
  const value = (Number(chips) || 0) * Number(config.chipValue);
  if (config.unit === "tokens") return `≈ ${trimNumber(value)} tokens`;
  return `≈ ${currencySymbol(config.currency)}${value.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  })}`;
}

function formatExpiry(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}

function formatClock(value) {
  const date = new Date(Number(value) || value);
  if (Number.isNaN(date.getTime())) return "--:--";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function formatUptime(seconds) {
  const total = Number(seconds);
  if (!Number.isFinite(total) || total < 0) return "—";
  const minutes = Math.floor(total / 60);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  return `${hours} 小时 ${minutes % 60} 分`;
}

function secondsLeft(table, seat) {
  if (table.deadline) return Math.max(0, Math.ceil((Number(table.deadline) - Date.now()) / 1000));
  return Math.max(0, Math.ceil(Number(seat?.timeLeft) || 0));
}

function seatTimePercent(table, seat) {
  const total = Math.max(1, Number(table.config?.actionSeconds) || 30);
  return Math.max(0, Math.min(100, (secondsLeft(table, seat) / total) * 100));
}

function clampInt(value, min, max) {
  const number = Math.round(Number(value));
  if (!Number.isFinite(number)) return Math.round(Number(min) || 0);
  return Math.min(Math.max(number, Math.round(Number(min) || 0)), Math.round(Number(max) || 0));
}

function esc(value) {
  return String(value ?? "").replace(/[&<>"']/g, (character) => {
    if (character === "&") return "&amp;";
    if (character === "<") return "&lt;";
    if (character === ">") return "&gt;";
    if (character === '"') return "&quot;";
    return "&#39;";
  });
}
