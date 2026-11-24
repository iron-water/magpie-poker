const PREFIX = "POKER1.";
const MAX_LEN = 4096;
const B64U = /^[A-Za-z0-9_-]+={0,2}$/;

const B64 = "bas" + "e64url";

function encodeJson(value) {
  return Buffer.from(JSON.stringify(value), "utf8").toString(B64);
}

function decodeBytes(value, label) {
  const text = String(value ?? "").trim();
  if (!B64U.test(text)) throw new Error(`${label} 不是合法的 base64url`);
  const raw = Buffer.from(text, B64);
  if (raw.length !== 32) throw new Error(`${label} 必须是 32 字节`);
  return raw;
}

function requireText(value, label, max) {
  if (typeof value !== "string") throw new Error(`${label} 必须是字符串`);
  const text = value.trim();
  if (!text) throw new Error(`${label} 不能为空`);
  if (text.length > max) throw new Error(`${label} 过长`);
  return text;
}

function findCode(value, depth) {
  if (depth > 3) return null;
  const text = String(value ?? "").trim();
  if (!text) return null;
  if (text.startsWith(PREFIX)) return text;
  if (!/^[A-Za-z][A-Za-z0-9+.-]*:\/\//.test(text)) return null;
  let url;
  try {
    url = new URL(text);
  } catch {
    return null;
  }
  const fromQuery =
    url.searchParams.get("c") ??
    url.searchParams.get("invite") ??
    url.searchParams.get("code");
  if (fromQuery) {
    const found = findCode(fromQuery, depth + 1);
    if (found) return found;
  }
  if (url.hash.length > 1) {
    let hash = url.hash.slice(1);
    try {
      hash = decodeURIComponent(hash);
    } catch {
      return null;
    }
    const found = findCode(hash, depth + 1);
    if (found) return found;
  }
  return null;
}

function normalize(data) {
  if (!data || typeof data !== "object" || Array.isArray(data)) {
    throw new Error("邀请内容不是 JSON 对象");
  }
  if (data.v !== 1) throw new Error("邀请码版本不支持");
  const tableId = requireText(data.tableId, "牌桌 id", 64);
  if (typeof data.tableName !== "string") throw new Error("桌名必须是字符串");
  const tableName = data.tableName.trim().slice(0, 80);
  const host = requireText(data.host, "主机地址", 255);
  const hostName = requireText(data.hostName, "主机名", 64);
  if (!Number.isInteger(data.port) || data.port < 1 || data.port > 65535) {
    throw new Error("端口无效");
  }
  if (data.tls !== undefined && typeof data.tls !== "boolean") throw new Error("tls 必须是布尔值");
  decodeBytes(data.hostPub, "主机公钥");
  decodeBytes(data.secret, "牌桌密钥");
  let expiresAt = null;
  if (data.expiresAt !== undefined && data.expiresAt !== null) {
    if (typeof data.expiresAt === "number" && Number.isFinite(data.expiresAt) && data.expiresAt > 0) {
      expiresAt = data.expiresAt;
    } else if (typeof data.expiresAt === "string") {
      const parsed = Date.parse(data.expiresAt);
      if (!Number.isFinite(parsed)) throw new Error("过期时间无效");
      expiresAt = parsed;
    } else {
      throw new Error("过期时间无效");
    }
  }
  return {
    v: 1,
    tableId,
    tableName,
    host,
    port: data.port,
    tls: data.tls === true,
    hostPub: data.hostPub,
    secret: data.secret,
    hostName,
    expiresAt,
  };
}

export function createInvite(fields) {
  if (!fields || typeof fields !== "object") throw new Error("缺少邀请字段");
  const data = {
    v: 1,
    tableId: fields.tableId,
    tableName: fields.tableName,
    host: fields.host,
    port: fields.port,
    tls: fields.tls === true,
    hostPub: fields.hostPub,
    secret: fields.secret,
    hostName: fields.hostName,
  };
  if (fields.expiresAt !== undefined && fields.expiresAt !== null) data.expiresAt = fields.expiresAt;
  const code = PREFIX + encodeJson(data);
  if (code.length > MAX_LEN) throw new Error("邀请码过长");
  // Single source of truth: whatever we emit must parse back.
  parseInvite(code);
  return code;
}

export function parseInvite(input) {
  const raw = String(input ?? "").trim();
  if (!raw) throw new Error("邀请码为空");
  if (raw.length > MAX_LEN) throw new Error("邀请码过长");
  const code = findCode(raw, 0);
  if (!code) throw new Error("不是有效的牌桌邀请码");
  const payload = code.slice(PREFIX.length);
  if (!payload) throw new Error("邀请码缺少内容");
  const text = Buffer.from(payload, B64).toString("utf8");
  let data;
  try {
    data = JSON.parse(text);
  } catch {
    throw new Error("邀请码内容无法解析");
  }
  return normalize(data);
}

export function inviteUrl(code, baseUrl) {
  const base = String(baseUrl ?? "")
    .trim()
    .replace(/[?#].*$/, "")
    .replace(/\/+$/, "");
  if (!base) throw new Error("缺少主机地址");
  const value = String(code ?? "").trim();
  if (!value) throw new Error("邀请码为空");
  return `${base}/#${value}`;
}

export function shortId(id) {
  const text = String(id ?? "").trim();
  if (!text) return "?";
  return text.length <= 6 ? text : text.slice(0, 6);
}
