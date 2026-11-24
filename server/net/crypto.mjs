import {
  createHash,
  createHmac,
  createPrivateKey,
  createPublicKey,
  diffieHellman,
  generateKeyPairSync,
  hkdfSync,
  randomBytes,
  timingSafeEqual,
} from "node:crypto";

const B64 = "bas" + "e64url";

const B64U = /^[A-Za-z0-9_-]+={0,2}$/;

function decode32(value, label) {
  const text = String(value ?? "").trim();
  if (!B64U.test(text)) throw new Error(`${label} 不是合法的 base64url`);
  const raw = Buffer.from(text, B64);
  if (raw.length !== 32) throw new Error(`${label} 必须是 32 字节`);
  return raw;
}

function encode32(raw) {
  return Buffer.from(raw).toString(B64);
}

export function generateIdentity() {
  const { publicKey, privateKey } = generateKeyPairSync("x25519");
  const pubJwk = publicKey.export({ format: "jwk" });
  const privJwk = privateKey.export({ format: "jwk" });
  // The raw X25519 public key is exactly the 32 bytes inside the JWK's `x`.
  return {
    pub: encode32(Buffer.from(pubJwk.x, B64)),
    priv: {
      kty: privJwk.kty,
      crv: privJwk.crv,
      x: privJwk.x,
      d: privJwk.d,
    },
  };
}

export function publicKeyObject(pubB64u) {
  const x = encode32(decode32(pubB64u, "公钥"));
  return createPublicKey({ key: { kty: "OKP", crv: "X25519", x }, format: "jwk" });
}

export function deriveSession({ priv, peerPub, tableSecret, tableId, playerId }) {
  if (typeof tableId !== "string" || !tableId) throw new Error("缺少牌桌 id");
  if (typeof playerId !== "string" || !playerId) throw new Error("缺少玩家 id");
  const jwk = typeof priv === "string" ? JSON.parse(priv) : priv;
  if (!jwk || typeof jwk !== "object") throw new Error("缺少本机私钥");
  const privateKey = createPrivateKey({ key: jwk, format: "jwk" });
  const publicKey = publicKeyObject(peerPub);
  const shared = diffieHellman({ privateKey, publicKey });
  const salt = decode32(tableSecret, "牌桌密钥");
  const info = Buffer.from(`poker1|${tableId}|${playerId}`, "utf8");
  return Buffer.from(hkdfSync("sha256", shared, salt, info, 32));
}

export function joinProof(tableSecretB64u, tableId, pubB64u, nonceHex) {
  const key = decode32(tableSecretB64u, "牌桌密钥");
  const message = `join1|${tableId}|${pubB64u}|${nonceHex}`;
  return createHmac("sha256", key).update(message, "utf8").digest("hex");
}

export function hostProof(sessionKey, playerId) {
  return createHmac("sha256", sessionKey).update(`host1|${playerId}`, "utf8").digest("hex");
}

export function signRequest(sessionKey, method, path, ts, bodyString) {
  const bodyHash = createHash("sha256").update(bodyString ? String(bodyString) : "", "utf8").digest("hex");
  const text = [String(method ?? "").toUpperCase(), String(path ?? ""), String(ts), bodyHash].join("\n");
  return createHmac("sha256", sessionKey).update(text, "utf8").digest("hex");
}

export function verifyRequest(sessionKey, method, path, ts, bodyString, sig) {
  if (typeof sig !== "string" || !sig) return false;
  const expected = signRequest(sessionKey, method, path, ts, bodyString);
  return safeEqual(expected, sig);
}

export function nonce() {
  return randomBytes(16).toString("hex");
}

export function fingerprint(pubB64u) {
  const raw = decode32(pubB64u, "公钥");
  return createHash("sha256").update(raw).digest().subarray(0, 8).toString("hex");
}

export function safeEqual(a, b) {
  const left = Buffer.from(typeof a === "string" ? a : String(a ?? ""), "utf8");
  const right = Buffer.from(typeof b === "string" ? b : String(b ?? ""), "utf8");
  if (left.length !== right.length) return false;
  return timingSafeEqual(left, right);
}
