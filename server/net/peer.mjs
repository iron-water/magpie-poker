import {
  deriveSession,
  generateIdentity,
  hostProof,
  joinProof,
  nonce,
  safeEqual,
  signRequest,
} from "./crypto.mjs";
import { parseInvite } from "./invite.mjs";

const JSON_HEADERS = { "content-type": "application/json", accept: "application/json" };
const EVENTS_PATH = "/peer/events";
const MAX_SSE_BUFFER = 1 << 20;

export class PeerError extends Error {
  constructor(message, code = null, status = 0) {
    super(message);
    this.name = "PeerError";
    this.code = code;
    this.status = status;
  }
}

function originOf(invite) {
  const host = String(invite?.host ?? "").trim();
  if (!host) throw new Error("邀请缺少主机地址");
  const shown = host.includes(":") && !host.startsWith("[") ? `[${host}]` : host;
  return `${invite.tls ? "https" : "http"}://${shown}:${Number(invite.port)}`;
}

function backoffDelay(attempt) {
  const base = Math.min(15000, 1000 * 2 ** Math.min(attempt, 4));
  const jitter = 0.7 + Math.random() * 0.6;
  return Math.max(1000, Math.min(15000, Math.round(base * jitter)));
}

async function readJson(response) {
  let text;
  try {
    text = await response.text();
  } catch {
    return null;
  }
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

function failure(data, status, fallback) {
  const message =
    (data && typeof data.error === "string" && data.error) ||
    (data && typeof data.text === "string" && data.text) ||
    fallback;
  const code = data && typeof data.code === "string" ? data.code : null;
  return new PeerError(message, code, status);
}

export class PeerClient {
  #identity;
  #session = null;
  #fetch;
  #onState;
  #onNotice;
  #onStatus;
  #sse = null;
  #closed = false;

  constructor({ invite, profile = {}, onState, onNotice, onStatus, fetchImpl } = {}) {
    this.invite = typeof invite === "string" ? parseInvite(invite) : invite;
    if (!this.invite || typeof this.invite !== "object") throw new TypeError("缺少邀请信息");
    this.profile = {
      name: "",
      avatar: "",
      gatewayUrl: "",
      tailscale: false,
      ...(profile && typeof profile === "object" ? profile : {}),
    };
    this.#onState = typeof onState === "function" ? onState : null;
    this.#onNotice = typeof onNotice === "function" ? onNotice : null;
    this.#onStatus = typeof onStatus === "function" ? onStatus : null;
    this.#fetch = typeof fetchImpl === "function" ? fetchImpl : globalThis.fetch;
    if (typeof this.#fetch !== "function") throw new Error("运行环境没有 fetch");
    this.#identity = generateIdentity();
    this.baseUrl = originOf(this.invite);
    this.status = "offline";
    this.playerId = null;
    this.seat = null;
    this.sessionId = null;
  }

  get pub() {
    return this.#identity.pub;
  }

  async connect() {
    if (this.#closed) throw new PeerError("连接已关闭", "closed", 0);
    if (this.#session && this.playerId) {
      this.#setStatus("online");
      return this;
    }
    const invite = this.invite;
    this.#setStatus("connecting");
    const n = nonce();
    const body = {
      v: 1,
      tableId: invite.tableId,
      name: this.profile.name,
      avatar: this.profile.avatar,
      pub: this.#identity.pub,
      nonce: n,
      proof: joinProof(invite.secret, invite.tableId, this.#identity.pub, n),
      profile: {
        gatewayUrl: this.profile.gatewayUrl || "",
        tailscale: this.profile.tailscale === true,
      },
    };
    let response;
    try {
      response = await this.#fetch(`${this.baseUrl}/peer/join`, {
        method: "POST",
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(10000),
      });
    } catch (error) {
      this.#setStatus("offline");
      throw new PeerError("无法连接牌桌主机", "offline", 0, { cause: error });
    }
    const data = await readJson(response);
    if (!data || data.ok !== true) {
      this.#setStatus("offline");
      throw failure(data, response.status, `入桌失败（${response.status}）`);
    }
    const playerId = typeof data.playerId === "string" ? data.playerId.trim() : "";
    if (!playerId) {
      this.#setStatus("offline");
      throw new PeerError("主机没有返回玩家 id", "bad_response", response.status);
    }
    if (typeof data.tableId === "string" && data.tableId !== invite.tableId) {
      this.#setStatus("offline");
      throw new PeerError("牌桌不匹配", "wrong_table", response.status);
    }
    let session;
    try {
      session = deriveSession({
        priv: this.#identity.priv,
        peerPub: data.hostPub,
        tableSecret: invite.secret,
        tableId: invite.tableId,
        playerId,
      });
    } catch {
      this.#setStatus("offline");
      throw new PeerError("主机公钥无效", "bad_host_key", response.status);
    }
    if (!safeEqual(hostProof(session, playerId), String(data.hostProof ?? ""))) {
      this.#setStatus("offline");
      throw new PeerError("主机身份校验失败", "bad_host_proof", response.status);
    }
    this.#session = session;
    this.playerId = playerId;
    this.seat = Number.isInteger(data.seat) ? data.seat : null;
    this.sessionId = typeof data.sessionId === "string" ? data.sessionId : null;
    this.#setStatus("online");
    return this;
  }

  async post(path, body) {
    if (this.#closed) throw new PeerError("连接已关闭", "closed", 0);
    if (!this.#session) throw new PeerError("尚未入桌", "offline", 0);
    const target = path.startsWith("/") ? path : `/${path}`;
    const bodyString = JSON.stringify(body ?? {});
    const ts = Date.now();
    const sig = signRequest(this.#session, "POST", target, ts, bodyString);
    let response;
    try {
      response = await this.#fetch(this.baseUrl + target, {
        method: "POST",
        headers: {
          ...JSON_HEADERS,
          "x-poker-player": this.playerId,
          "x-poker-ts": String(ts),
          "x-poker-sig": sig,
        },
        body: bodyString,
        signal: AbortSignal.timeout(15000),
      });
    } catch (error) {
      throw new PeerError("网络连接失败", "offline", 0, { cause: error });
    }
    const data = await readJson(response);
    if (!data || data.ok !== true) {
      throw failure(data, response.status, `请求失败（${response.status}）`);
    }
    return data;
  }

  subscribe() {
    if (this.#closed || !this.#session) return () => {};
    if (this.#sse) this.#sse.stop();
    const stream = createStreamState();
    this.#sse = stream;
    stream.stop = () => {
      if (stream.stopped) return;
      stream.stopped = true;
      if (stream.controller) stream.controller.abort();
      clearTimeout(stream.timer);
      if (stream.wake) stream.wake();
      if (this.#sse === stream) this.#sse = null;
    };
    void this.#runStream(stream);
    return stream.stop;
  }

  close() {
    this.#closed = true;
    if (this.#sse) this.#sse.stop();
    this.#session = null;
    this.#setStatus("closed");
  }

  #setStatus(next) {
    if (this.status === next) return;
    this.status = next;
    this.#emit(this.#onStatus, next);
  }

  #emit(fn, value) {
    if (!fn) return;
    try {
      fn(value);
    } catch {
      // A misbehaving listener must not kill the event loop.
    }
  }

  async #runStream(stream) {
    while (!stream.stopped && !this.#closed) {
      if (!this.#session) break;
      const controller = new AbortController();
      stream.controller = controller;
      try {
        const ts = Date.now();
        const sig = signRequest(this.#session, "GET", EVENTS_PATH, ts, "");
        const query = new URLSearchParams({
          playerId: this.playerId,
          ts: String(ts),
          sig,
        });
        const response = await this.#fetch(`${this.baseUrl}${EVENTS_PATH}?${query}`, {
          method: "GET",
          headers: { accept: "text/event-stream" },
          signal: controller.signal,
        });
        if (!response.ok || !response.body) {
          throw new PeerError(`事件流打开失败（${response.status}）`, "stream_failed", response.status);
        }
        stream.attempt = 0;
        this.#setStatus("online");
        await this.#pump(response.body, stream);
      } catch {
        if (stream.stopped || this.#closed) break;
      }
      if (stream.stopped || this.#closed) break;
      this.#setStatus("offline");
      const delay = backoffDelay(stream.attempt++);
      await new Promise((resolve) => {
        stream.wake = resolve;
        stream.timer = setTimeout(resolve, delay);
      });
      stream.timer = null;
      stream.wake = null;
    }
    if (!stream.stopped) {
      stream.stopped = true;
      if (this.#sse === stream) this.#sse = null;
    }
  }

  async #pump(body, stream) {
    const decoder = new TextDecoder();
    let buffer = "";
    let eventName = "message";
    let dataLines = [];
    const dispatch = () => {
      if (!dataLines.length) {
        eventName = "message";
        return;
      }
      const raw = dataLines.join("\n");
      const name = eventName || "message";
      eventName = "message";
      dataLines = [];
      let payload;
      try {
        payload = JSON.parse(raw);
      } catch {
        return;
      }
      this.#handleEvent(name, payload);
    };
    for await (const chunk of body) {
      if (stream.stopped || this.#closed) return;
      buffer += typeof chunk === "string" ? chunk : decoder.decode(chunk, { stream: true });
      if (buffer.length > MAX_SSE_BUFFER) {
        buffer = "";
        dataLines = [];
        eventName = "message";
      }
      let index;
      while ((index = buffer.indexOf("\n")) >= 0) {
        let line = buffer.slice(0, index);
        buffer = buffer.slice(index + 1);
        if (line.endsWith("\r")) line = line.slice(0, -1);
        if (line === "") {
          dispatch();
          continue;
        }
        if (line.startsWith(":")) continue;
        const colon = line.indexOf(":");
        const field = colon < 0 ? line : line.slice(0, colon);
        let value = colon < 0 ? "" : line.slice(colon + 1);
        if (value.startsWith(" ")) value = value.slice(1);
        if (field === "event") eventName = value;
        else if (field === "data") dataLines.push(value);
      }
    }
    dispatch();
  }

  #handleEvent(name, payload) {
    if (name === "state") {
      if (payload && typeof payload === "object") this.#emit(this.#onState, payload);
      return;
    }
    if (name === "notice") {
      const level = payload && typeof payload.level === "string" ? payload.level : "info";
      const text = payload && typeof payload.text === "string" ? payload.text : "";
      this.#emit(this.#onNotice, { level, text });
      return;
    }
    if (name === "error") {
      const text =
        (payload && typeof payload.text === "string" && payload.text) ||
        (payload && typeof payload.error === "string" && payload.error) ||
        (payload && typeof payload.message === "string" && payload.message) ||
        "牌桌错误";
      this.#emit(this.#onNotice, { level: "error", text });
    }
  }
}

function createStreamState() {
  return {
    stopped: false,
    attempt: 0,
    controller: null,
    timer: null,
    wake: null,
    stop: () => {},
  };
}

