import { randomSeed, seededShuffle, formatCard, rankOf, suitOf, RANKS } from "./cards.mjs";
import { evaluate } from "./eval.mjs";
import { computePots, awardPots } from "./pots.mjs";

const STREETS = ["preflop", "flop", "turn", "river"];

export class PokerError extends Error {
  constructor(message, code = "bad_action") {
    super(message);
    this.code = code;
  }
}

export class Table {
  constructor({ id, name, config, hostId, restore = false }) {
    this.id = String(id);
    this.name = String(name || "牌局").slice(0, 40);
    this.hostId = String(hostId || "");
    this.config = normalizeConfig(config);
    this.phase = "lobby";
    this.players = [];
    this.seats = Array(this.config.maxSeats).fill(null);
    this.dealerSeat = -1;
    this.handNo = 0;
    this.street = "preflop";
    this.board = [];
    this.deck = [];
    this.deckPos = 0;
    this.handPlayers = [];
    this.currentBet = 0;
    this.minRaiseTo = 0;
    this.lastRaiseSize = this.config.bigBlind;
    this.toAct = null;
    this.deadline = null;
    this.aggressor = null;
    this.showdown = null;
    this.settlement = null;
    this.log = [];
    this.hand = null;
    this.createdAt = Date.now();
    this.updatedAt = this.createdAt;
    if (!restore) this.note("system", `牌桌「${this.name}」已创建`);
  }

  static fromJSON(data) {
    const t = new Table({
      id: data.id,
      name: data.name,
      config: data.config,
      hostId: data.hostId,
      restore: true,
    });
    Object.assign(t, data);
    t.config = normalizeConfig(data.config);
    t.seats = Array(t.config.maxSeats).fill(null);
    for (const p of t.players) t.seats[p.seat] = p.id;
    return t;
  }

  toJSON() {
    return JSON.parse(JSON.stringify({ ...this }));
  }

  // ---- players -----------------------------------------------------------

  get maxSeats() {
    return this.config.maxSeats;
  }

  player(id) {
    return this.players.find((p) => p.id === id) || null;
  }

  seatOf(id) {
    const p = this.player(id);
    return p ? p.seat : -1;
  }

  occupied() {
    return this.players.filter((p) => p.seat >= 0).sort((a, b) => a.seat - b.seat);
  }

  nextSeat(fromSeat, pred = () => true) {
    for (let i = 1; i <= this.maxSeats; i++) {
      const seat = (fromSeat + i + this.maxSeats * 2) % this.maxSeats;
      const id = this.seats[seat];
      if (!id) continue;
      const p = this.player(id);
      if (p && pred(p)) return p;
    }
    return null;
  }

  orderFrom(seat, pred = () => true) {
    const out = [];
    for (let i = 1; i <= this.maxSeats; i++) {
      const s = (seat + i + this.maxSeats * 2) % this.maxSeats;
      const id = this.seats[s];
      const p = id && this.player(id);
      if (p && pred(p)) out.push(p);
    }
    return out;
  }

  addPlayer({ id, name, avatar, gatewayUrl, tailscale, connected = true }) {
    if (this.phase === "ended") throw new PokerError("牌局已经结束", "ended");
    const clean = String(name || "玩家").trim().slice(0, 16) || "玩家";
    const existing = this.players.find((p) => p.name === clean && !p.connected && p.seat >= 0);
    if (existing) {
      existing.connected = connected;
      existing.sittingOut = false;
      existing.avatar = avatar || existing.avatar;
      existing.gatewayUrl = gatewayUrl || existing.gatewayUrl;
      existing.tailscale = tailscale || existing.tailscale;
      this.note("system", `${clean} 重新连接`);
      this.touch();
      return existing;
    }
    if (this.phase === "ended") throw new PokerError("牌局已经结束", "ended");
    const free = this.seats.findIndex((s) => s === null);
    if (free < 0) throw new PokerError("牌桌已满", "full");
    const taken = new Set(this.players.filter((p) => p.seat >= 0 && !p.left).map((p) => p.name));
    let finalName = clean;
    if (taken.has(finalName)) {
      let n = 2;
      while (taken.has(`${clean} ${n}`)) n += 1;
      finalName = `${clean} ${n}`;
    }
    const p = {
      id: String(id),
      name: finalName,
      avatar: avatar || "fox",
      seat: free,
      chips: this.config.startingChips,
      buyIn: this.config.startingChips,
      gatewayUrl: String(gatewayUrl || ""),
      tailscale: tailscale || null,
      connected: !!connected,
      sittingOut: this.phase === "hand",
      out: false,
      left: false,
      cards: [],
      bet: 0,
      committed: 0,
      folded: false,
      allIn: false,
      acted: false,
      mayRaise: true,
      lastAction: "",
      won: 0,
    };
    this.players.push(p);
    this.players.sort((a, b) => a.seat - b.seat);
    this.seats[free] = p.id;
    this.note("system", `${p.name} 入座（${this.config.startingChips} 筹码）`);
    this.touch();
    return p;
  }

  markDisconnected(id) {
    const p = this.player(id);
    if (!p || p.left) return false;
    p.connected = false;
    p.sittingOut = true;
    if (this.phase === "hand" && !p.folded) {
      if (this.toAct === p.id) this.act(id, { type: "fold" }, { auto: true });
      else this.note("system", `${p.name} 断线，本手轮到时会自动弃牌`);
    }
    this.touch();
    return true;
  }

  leave(id) {
    const p = this.player(id);
    if (!p) return false;
    p.left = true;
    p.connected = false;
    p.sittingOut = true;
    if (this.phase === "hand" && !p.folded) {
      p.folded = true;
      p.lastAction = "离桌";
      this.note("action", `${p.name} 离桌并弃牌`, p.id);
      if (this.toAct === p.id) this.toAct = null;
      this.progress(this.seatOf(p.id));
    } else if (this.phase === "lobby" || this.phase === "ended") {
      this.seats[p.seat] = null;
      this.players = this.players.filter((x) => x.id !== p.id);
    }
    this.touch();
    return true;
  }

  chat(id, text) {
    const p = this.player(id);
    if (!p) throw new PokerError("不在牌桌上", "not_seated");
    const t = String(text || "").trim().slice(0, 200);
    if (!t) return;
    this.note("chat", `${p.name}：${t}`, p.id);
    this.touch();
  }

  rebuy(id, amount) {
    if (this.phase === "hand") throw new PokerError("一手牌进行中，不能补码", "in_hand");
    if (this.phase === "ended") throw new PokerError("牌局已经结束", "ended");
    const n = Math.floor(Number(amount));
    if (!Number.isFinite(n) || n <= 0 || n > 100_000_000) throw new PokerError("补码数量不合法");
    const p = this.player(id);
    if (!p) throw new PokerError("不在牌桌上", "not_seated");
    p.chips += n;
    p.buyIn += n;
    p.out = false;
    p.sittingOut = false;
    this.note("system", `${p.name} 补码 ${n}`);
    this.touch();
    return p.chips;
  }

  // ---- hand lifecycle ----------------------------------------------------

  startHand(seed = randomSeed()) {
    if (this.phase === "hand") throw new PokerError("上一手还没结束", "in_hand");
    if (this.phase === "ended") throw new PokerError("牌局已经结束", "ended");
    const eligible = this.players.filter((p) => p.connected && !p.left && p.seat >= 0 && p.chips > 0);
    for (const p of this.players) {
      p.out = p.seat >= 0 && p.chips <= 0;
      if (p.connected && p.chips > 0) p.sittingOut = false;
    }
    if (eligible.length < 2) throw new PokerError("至少需要两位有筹码的玩家", "not_enough");

    this.handNo += 1;
    this.phase = "hand";
    this.street = "preflop";
    this.board = [];
    this.showdown = null;
    this.settlement = null;
    this.aggressor = null;
    this.handPlayers = eligible.map((p) => p.id);
    this.deck = seededShuffle(seed, this.handNo);
    this.deckPos = 0;
    this.lastRaiseSize = this.config.bigBlind;

    for (const p of this.players) {
      p.cards = [];
      p.won = 0;
      p.bet = 0;
      p.committed = 0;
      p.folded = false;
      p.allIn = false;
      p.acted = false;
      p.mayRaise = true;
      p.lastAction = "";
    }

    this.dealerSeat =
      this.dealerSeat < 0
        ? eligible[0].seat
        : this.nextSeat(this.dealerSeat, (p) => eligible.includes(p))?.seat ?? eligible[0].seat;

    const order = this.orderFrom(this.dealerSeat, (p) => eligible.includes(p));
    for (let i = 0; i < 2; i++) {
      for (const p of order) p.cards.push(this.deck[this.deckPos++]);
    }

    const headsUp = eligible.length === 2;
    const sb = headsUp ? this.player(this.seats[this.dealerSeat]) : this.orderFrom(this.dealerSeat, (p) => eligible.includes(p))[0];
    const bb = this.nextSeat(sb.seat, (p) => eligible.includes(p));
    this.sbSeat = sb.seat;
    this.bbSeat = bb.seat;

    if (this.config.ante > 0) {
      for (const p of eligible) this.putIn(p, Math.min(this.config.ante, p.chips));
    }
    this.putIn(sb, Math.min(this.config.smallBlind, sb.chips));
    if (sb.chips === 0) sb.allIn = true;
    this.putIn(bb, Math.min(this.config.bigBlind, bb.chips));
    if (bb.chips === 0) bb.allIn = true;

    this.currentBet = this.config.bigBlind;
    this.minRaiseTo = this.currentBet + this.config.bigBlind;
    this.toAct = (headsUp ? sb : this.nextSeat(bb.seat, (p) => eligible.includes(p)))?.id || null;
    this.hand = { no: this.handNo, seed, startedAt: Date.now(), actions: [] };
    this.note(
      "deal",
      `第 ${this.handNo} 手开始 · ${sb.name} 小盲 ${Math.min(this.config.smallBlind, sb.chips)} · ${bb.name} 大盲 ${Math.min(this.config.bigBlind, bb.chips)}`
    );
    this.touch();
    this.progress(bb.seat);
    return this.viewFor(null);
  }

  act(playerId, action, { auto = false } = {}) {
    if (this.phase !== "hand") throw new PokerError("现在没有进行中的手牌", "no_hand");
    if (this.toAct !== playerId) throw new PokerError("还没轮到你", "out_of_turn");
    const p = this.player(playerId);
    if (!p || p.folded || p.allIn) throw new PokerError("你不能行动", "inactive");
    const type = String(action?.type || "");
    const toCall = Math.max(0, this.currentBet - p.bet);
    const maxTo = p.bet + p.chips;
    let target = null;

    if (type === "fold") {
      p.folded = true;
      p.lastAction = "弃牌";
      this.note("action", `${p.name} 弃牌`, p.id);
    } else if (type === "check") {
      if (toCall > 0) throw new PokerError(`需要跟注 ${toCall}`, "must_call");
      p.lastAction = "过牌";
      this.note("action", `${p.name} 过牌`, p.id);
    } else if (type === "call") {
      if (toCall <= 0) throw new PokerError("没有需要跟注的额度", "nothing_to_call");
      const pay = Math.min(toCall, p.chips);
      this.putIn(p, pay);
      p.lastAction = p.chips === 0 ? `全下 ${p.bet}` : `跟注 ${pay}`;
      this.note("action", `${p.name} ${p.lastAction}`, p.id);
    } else if (type === "bet" || type === "raise" || type === "allin") {
      if (type === "bet" && this.currentBet > 0) throw new PokerError("请使用加注", "use_raise");
      if (type === "raise" && this.currentBet <= 0) throw new PokerError("请使用下注", "use_bet");
      if (type === "raise" && !p.mayRaise) throw new PokerError("本轮不能再次加注", "no_raise");
      if (type === "allin") {
        target = maxTo;
        if (target > this.currentBet && !p.mayRaise) throw new PokerError("本轮不能再次加注", "no_raise");
      }
      else {
        target = Math.floor(Number(action?.amount));
        if (!Number.isFinite(target) || target <= 0) throw new PokerError("下注额不合法");
        if (target > maxTo) throw new PokerError("筹码不够", "not_enough_chips");
      }
      if (target <= this.currentBet) {
        if (target !== maxTo) throw new PokerError(`至少要加到 ${this.currentBet + 1}`, "too_small");
        const pay = Math.min(toCall, p.chips);
        this.putIn(p, pay);
        p.lastAction = p.chips === 0 ? `全下 ${p.bet}` : `跟注 ${pay}`;
        this.note("action", `${p.name} ${p.lastAction}`, p.id);
      } else {
        const min = this.currentBet > 0 ? this.minRaiseTo : this.config.bigBlind;
        const isAllIn = target === maxTo;
        if (target < min && !(isAllIn && target > this.currentBet)) {
          throw new PokerError(`至少要加到 ${Math.min(min, maxTo)}`, "too_small");
        }
        const wasOpen = this.currentBet === 0;
        const raiseSize = target - this.currentBet;
        const full = raiseSize >= this.lastRaiseSize;
        const beforeActed = this.players.filter((x) => x.acted).map((x) => x.id);
        this.putIn(p, target - p.bet);
        this.currentBet = target;
        if (full) {
          this.lastRaiseSize = raiseSize;
          this.minRaiseTo = this.currentBet + this.lastRaiseSize;
          for (const x of this.players) {
            if (!x.folded && !x.allIn) x.mayRaise = true;
            x.acted = false;
          }
        } else {
          this.minRaiseTo = this.currentBet + this.lastRaiseSize;
          for (const id of beforeActed) {
            const x = this.player(id);
            if (x && x.id !== p.id) x.mayRaise = false;
          }
        }
        this.aggressor = p.id;
        p.lastAction = isAllIn
          ? `全下 ${p.bet}${full ? "" : "（短加注）"}`
          : `${wasOpen ? "下注" : "加注到"} ${p.bet}`;
        this.note("action", `${p.name} ${p.lastAction}`, p.id);
      }
    } else {
      throw new PokerError(`未知动作 ${type}`, "bad_action");
    }

    p.acted = true;
    this.hand.actions.push({ at: Date.now(), id: p.id, type, target });
    this.touch();
    this.progress(p.seat);
    return auto ? null : this.viewFor(playerId);
  }

  putIn(p, amount) {
    const n = Math.max(0, Math.min(Math.floor(amount), p.chips));
    p.chips -= n;
    p.bet += n;
    p.committed += n;
    if (p.chips === 0) p.allIn = true;
  }

  progress(fromSeat) {
    const live = this.players.filter((p) => this.handPlayers.includes(p.id) && !p.folded);
    if (live.length === 0) return;
    if (live.length === 1) return this.finishByFold(live[0]);

    const active = live.filter((p) => !p.allIn);
    const pending = active.filter((p) => !p.acted || p.bet < this.currentBet);
    if (!pending.length) {
      if (this.street === "river") return this.finishShowdown();
      if (!active.length) return this.runOut();
      return this.advanceStreet();
    }
    const next = this.orderFrom(fromSeat, (p) => pending.includes(p))[0];
    this.toAct = next ? next.id : pending[0].id;
    this.deadline = this.config.actionSeconds > 0 ? Date.now() + this.config.actionSeconds * 1000 : null;
  }

  advanceStreet() {
    const idx = STREETS.indexOf(this.street);
    this.street = STREETS[Math.min(idx + 1, 3)];
    for (const p of this.players) {
      if (!this.handPlayers.includes(p.id)) continue;
      p.bet = 0;
      p.acted = false;
      p.mayRaise = true;
    }
    this.currentBet = 0;
    this.minRaiseTo = this.config.bigBlind;
    this.lastRaiseSize = this.config.bigBlind;
    this.aggressor = null;
    if (this.street === "flop") this.deal(3);
    else this.deal(1);
    this.note("deal", `${streetName(this.street)}：${this.boardText()}`);
    const active = this.players.filter((p) => this.handPlayers.includes(p.id) && !p.folded && !p.allIn);
    const first = this.orderFrom(this.dealerSeat, (p) => active.includes(p))[0];
    if (!first || active.length <= 1) return this.runOut();
    this.toAct = first.id;
    this.deadline = this.config.actionSeconds > 0 ? Date.now() + this.config.actionSeconds * 1000 : null;
  }

  deal(n) {
    this.deckPos += 1; // burn
    for (let i = 0; i < n; i++) this.board.push(this.deck[this.deckPos++]);
  }

  runOut() {
    while (this.board.length < 5) {
      this.deckPos += 1;
      if (this.board.length === 0) for (let i = 0; i < 3; i++) this.board.push(this.deck[this.deckPos++]);
      else this.board.push(this.deck[this.deckPos++]);
    }
    this.street = "river";
    this.note("deal", `发完公共牌：${this.boardText()}`);
    this.finishShowdown();
  }

  boardText() {
    return this.board.map(formatCard).join(" ");
  }

  finishByFold(winner) {
    const total = this.players.filter((p) => this.handPlayers.includes(p.id)).reduce((n, p) => n + p.committed, 0);
    winner.chips += total;
    winner.won = total;
    for (const p of this.players) p.committed = 0;
    this.showdown = {
      reason: "fold",
      total,
      revealed: [],
      pots: [{ amount: total, eligible: [winner.id], winners: [{ id: winner.id, chips: total }] }],
    };
    this.phase = "showdown";
    this.toAct = null;
    this.deadline = null;
    this.hand.result = { reason: "fold", total, winners: [winner.id] };
    this.note("result", `${winner.name} 赢得底池 ${total}（其他人弃牌）`, winner.id);
    this.touch();
  }

  finishShowdown() {
    if (this.phase !== "hand") return;
    const live = this.players.filter((p) => this.handPlayers.includes(p.id) && !p.folded);
    const scores = new Map();
    const hands = new Map();
    for (const p of live) {
      const r = evaluate(p.cards.concat(this.board));
      scores.set(p.id, r.score);
      hands.set(p.id, r);
    }
    const { pots, refunds, total } = computePots(
      this.players.filter((p) => this.handPlayers.includes(p.id))
    );
    for (const r of refunds) {
      const p = this.player(r.id);
      if (p) p.chips += r.amount;
    }
    const awarded = awardPots(pots, (id) => scores.get(id) ?? -1, {
      dealerSeat: this.dealerSeat,
      seatCount: this.maxSeats,
      seatOf: (id) => this.seatOf(id),
    });
    const winners = [];
    for (const pot of awarded) {
      for (const w of pot.winners) {
        const p = this.player(w.id);
        if (!p) continue;
        p.chips += w.chips;
        p.won += w.chips;
        winners.push(w.id);
      }
    }
    for (const p of this.players) p.committed = 0;
    this.showdown = {
      reason: "showdown",
      total,
      revealed: live.map((p) => ({
        id: p.id,
        name: p.name,
        cards: p.cards.map(formatCard),
        hand: hands.get(p.id)?.nameZh || "",
        won: p.won,
      })),
      pots: awarded.map((pot) => ({
        amount: pot.amount,
        winners: pot.winners.map((w) => w.id),
      })),
    };
    this.phase = "showdown";
    this.toAct = null;
    this.deadline = null;
    this.hand.result = { reason: "showdown", total, winners: [...new Set(winners)] };
    for (const pot of awarded) {
      const names = pot.winners.map((w) => this.player(w.id)?.name).filter(Boolean).join("、");
      if (names) this.note("result", `${names} 赢得 ${pot.amount} 的底池`);
    }
    this.touch();
  }

  tick(now = Date.now()) {
    if (this.phase !== "hand" || !this.deadline || now < this.deadline || !this.toAct) return false;
    const p = this.player(this.toAct);
    if (!p) return false;
    const toCall = this.currentBet - p.bet;
    try {
      this.act(p.id, { type: toCall > 0 ? "fold" : "check" }, { auto: true });
    } catch {
      if (this.phase === "hand") {
        p.folded = true;
        p.lastAction = "超时弃牌";
        this.progress(p.seat);
      }
    }
    return true;
  }

  // ---- settlement --------------------------------------------------------

  endTable() {
    if (this.phase === "hand") throw new PokerError("请先打完这一手", "in_hand");
    if (this.phase === "ended") return this.viewFor(null);
    const rows = this.players.filter((p) => p.seat >= 0);
    const net = rows.map((p) => ({ p, chips: p.chips - p.buyIn }));
    const winners = net.filter((x) => x.chips > 0).map((x) => ({ ...x, due: x.chips })).sort((a, b) => b.due - a.due);
    const losers = net.filter((x) => x.chips < 0).map((x) => ({ ...x, owed: -x.chips })).sort((a, b) => b.owed - a.owed);
    const totalWin = winners.reduce((n, x) => n + x.due, 0);
    const totalLost = losers.reduce((n, x) => n + x.owed, 0);
    if (totalWin !== totalLost) {
      throw new PokerError(`筹码不守恒（赢 ${totalWin} / 输 ${totalLost}），已阻止结算`, "not_zero_sum");
    }
    const transfers = [];
    let wi = 0;
    let li = 0;
    while (wi < winners.length && li < losers.length) {
      const w = winners[wi];
      const l = losers[li];
      const chips = Math.min(w.due, l.owed);
      if (chips > 0) {
        const amount = round4(chips * this.config.chipValue);
        transfers.push({
          id: `x${transfers.length + 1}`,
          from: l.p.id,
          fromName: l.p.name,
          to: w.p.id,
          toName: w.p.name,
          amountChips: chips,
          amount,
          unit: this.config.unit,
          status: amount < this.config.minSettlement ? "small" : "pending",
          gatewayUrl: l.p.gatewayUrl || "",
          key: null,
          expires: null,
          error: null,
        });
        w.due -= chips;
        l.owed -= chips;
      }
      if (w.due === 0) wi++;
      if (l.owed === 0) li++;
    }
    this.settlement = { at: Date.now(), totalChips: totalLost, transfers };
    this.phase = "ended";
    this.toAct = null;
    this.deadline = null;
    this.note("result", `牌局结束：${winners.length} 位赢家，${losers.length} 位输家，共 ${transfers.length} 笔结算`, null);
    this.touch();
    return this.viewFor(null);
  }

  markIssued(transferId, actorId, { key, expires, gatewayUrl }) {
    const t = this.settlement?.transfers.find((x) => x.id === transferId);
    if (!t) throw new PokerError("没有这笔结算", "no_transfer");
    if (t.from !== actorId) throw new PokerError("只有欠款方能签发", "not_payer");
    if (!key || typeof key !== "string" || key.length < 16) throw new PokerError("key 不合法");
    t.key = key;
    t.expires = expires || null;
    if (gatewayUrl) t.gatewayUrl = String(gatewayUrl);
    t.status = "issued";
    t.error = null;
    this.note("result", `${t.fromName} 已签发 ${t.amount} 的 key 给 ${t.toName}`, null);
    this.touch();
    return t;
  }

  markVerified(transferId, actorId) {
    const t = this.settlement?.transfers.find((x) => x.id === transferId);
    if (!t) throw new PokerError("没有这笔结算", "no_transfer");
    if (t.to !== actorId) throw new PokerError("只有收款方能验证", "not_payee");
    t.status = "verified";
    this.touch();
    return t;
  }

  // ---- views -------------------------------------------------------------

  viewFor(viewerId, now = Date.now()) {
    const me = viewerId ? this.player(viewerId) : null;
    const revealed = new Set(this.showdown?.revealed?.map((r) => r.id) || []);
    const seatRows = this.players
      .filter((p) => p.seat >= 0)
      .sort((a, b) => a.seat - b.seat)
      .map((p) => {
        const show = p.id === viewerId || (revealed.has(p.id) && !p.folded);
        return {
          seat: p.seat,
          id: p.id,
          name: p.name,
          avatar: p.avatar,
          chips: p.chips,
          bet: p.bet,
          committed: p.committed,
          folded: p.folded,
          allIn: p.allIn,
          out: p.out,
          left: p.left,
          sittingOut: p.sittingOut,
          connected: p.connected,
          isDealer: p.seat === this.dealerSeat,
          isSB: p.seat === this.sbSeat,
          isBB: p.seat === this.bbSeat,
          cards: show ? p.cards.map(formatCard) : null,
          cardsCount: this.phase === "hand" && this.handPlayers.includes(p.id) ? p.cards.length : 0,
          lastAction: p.lastAction,
          isMe: p.id === viewerId,
          mayRaise: p.mayRaise,
          timeLeft:
            this.toAct === p.id && this.deadline
              ? Math.max(0, Math.ceil((this.deadline - now) / 1000))
              : null,
        };
      });

    let pots = [];
    let pot = 0;
    if (this.phase === "hand") {
      const built = computePots(this.players.filter((p) => this.handPlayers.includes(p.id)));
      pots = built.pots;
      pot = built.total;
    } else if (this.showdown) {
      pots = this.showdown.pots.map((x) => ({ amount: x.amount, eligible: x.winners, winners: x.winners }));
      pot = 0;
    }

    const legal = this.phase === "hand" && this.toAct === viewerId && me ? this.legalFor(me) : null;
    const strength = me && me.cards.length === 2 ? handStrength(me.cards, this.board) : null;

    return {
      id: this.id,
      name: this.name,
      phase: this.phase,
      handNo: this.handNo,
      config: this.config,
      dealerSeat: this.dealerSeat,
      toAct: this.phase === "hand" ? this.toAct : null,
      currentBet: this.currentBet,
      minRaiseTo: this.minRaiseTo,
      pot,
      pots,
      board: this.board.map(formatCard),
      seats: seatRows,
      me: me
        ? {
            id: me.id,
            seat: me.seat,
            cards: me.cards.map(formatCard),
            legal,
            handStrength: strength,
          }
        : null,
      log: this.log.slice(-80),
      showdown: this.showdown,
      deadline: this.phase === "hand" ? this.deadline : null,
      settlement: this.settlement ? redactSettlement(this.settlement, viewerId) : null,
    };
  }

  legalFor(p) {
    const toCall = Math.max(0, this.currentBet - p.bet);
    const maxTo = p.bet + p.chips;
    return {
      fold: true,
      check: toCall === 0,
      call: toCall > 0 ? Math.min(toCall, p.chips) : null,
      bet: this.currentBet === 0 ? { min: Math.min(this.config.bigBlind, p.chips), max: p.chips } : null,
      raise:
        this.currentBet > 0 && p.mayRaise && maxTo > this.currentBet
          ? { min: Math.min(Math.max(this.minRaiseTo, this.currentBet + 1), maxTo), max: maxTo }
          : null,
      allIn: p.chips,
    };
  }

  note(kind, text, playerId = null) {
    this.log.push({ at: Date.now(), kind, text, playerId });
    if (this.log.length > 240) this.log.splice(0, this.log.length - 240);
  }

  touch() {
    this.updatedAt = Date.now();
  }
}

export function normalizeConfig(raw = {}) {
  const num = (v, d, min, max) => {
    const n = Math.floor(Number(v));
    return Number.isFinite(n) ? Math.min(Math.max(n, min), max) : d;
  };
  const bb = num(raw.bigBlind, 50, 2, 10_000_000);
  const sb = num(raw.smallBlind, Math.max(1, Math.floor(bb / 2)), 1, bb);
  const unit = raw.unit === "tokens" ? "tokens" : "cost";
  const chipValue = Number(raw.chipValue);
  return {
    smallBlind: sb,
    bigBlind: bb,
    ante: num(raw.ante, 0, 0, bb * 100),
    startingChips: num(raw.startingChips, bb * 100, bb * 2, 1_000_000_000),
    maxSeats: num(raw.maxSeats, 9, 2, 9),
    unit,
    chipValue: Number.isFinite(chipValue) && chipValue > 0 ? chipValue : unit === "tokens" ? 100 : 0.0005,
    currency: String(raw.currency || "USD").slice(0, 4),
    actionSeconds: num(raw.actionSeconds, 30, 0, 600),
    minSettlement: Number.isFinite(Number(raw.minSettlement)) ? Math.max(0, Number(raw.minSettlement)) : 0.01,
  };
}

function redactSettlement(settlement, viewerId) {
  const copy = JSON.parse(JSON.stringify(settlement));
  for (const t of copy.transfers) {
    if (t.to !== viewerId) t.key = null;
  }
  return copy;
}

function handStrength(cards, board) {
  if (board.length >= 3) {
    const r = evaluate(cards.concat(board));
    return { label: r.nameZh, category: r.category, score: r.score };
  }
  const [a, b] = cards.slice().sort((x, y) => rankOf(y) - rankOf(x));
  const ra = RANKS[rankOf(a) - 2];
  const rb = RANKS[rankOf(b) - 2];
  const suited = suitOf(a) === suitOf(b);
  const label =
    rankOf(a) === rankOf(b)
      ? `对 ${ra}`
      : `${ra}${rb} ${suited ? "同花" : "杂色"}${Math.abs(rankOf(a) - rankOf(b)) === 1 ? " · 连牌" : ""}`;
  return { label, category: null, score: null };
}

function streetName(street) {
  return { preflop: "翻牌前", flop: "翻牌", turn: "转牌", river: "河牌" }[street] || street;
}

function round4(n) {
  return Math.round(n * 10000) / 10000;
}
