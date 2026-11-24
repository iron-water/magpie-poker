import { execFile, execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";

const runFile = promisify(execFile);

export function magpieConfigDir() {
  if (process.env.MAGPIE_CONFIG_DIR) return process.env.MAGPIE_CONFIG_DIR;
  if (process.env.XDG_CONFIG_HOME) return join(process.env.XDG_CONFIG_HOME, "magpie");
  return join(homedir(), ".config", "magpie");
}

let cachedBin;
export function magpieBin() {
  if (cachedBin !== undefined) return cachedBin;
  const local = process.env.LOCALAPPDATA || "";
  const candidates = [
    process.env.MAGPIE_BIN,
    local && join(local, "Programs", "magpie", "magpie.com"),
    local && join(local, "magpie", "magpie.com"),
    process.env.ProgramFiles && join(process.env.ProgramFiles, "magpie", "magpie.com"),
    "magpie.com",
    "magpie",
  ].filter(Boolean);
  for (const c of candidates) {
    if (c.includes("/") || c.includes("\\")) {
      if (existsSync(c)) return (cachedBin = c);
    } else {
      try {
        const out = execFileSync(process.platform === "win32" ? "where.exe" : "which", [c], {
          encoding: "utf8",
          windowsHide: true,
          timeout: 3000,
        });
        const first = out.split(/\r?\n/).map((s) => s.trim()).filter(Boolean)[0];
        if (first) return (cachedBin = first);
      } catch {
        /* next */
      }
    }
  }
  return (cachedBin = null);
}

export function gatewayUrl() {
  if (process.env.MAGPIE_ADDR && /^[\w.-]+:\d+$/.test(process.env.MAGPIE_ADDR)) {
    return `http://${process.env.MAGPIE_ADDR}`;
  }
  try {
    const files = [join(magpieConfigDir(), "gateway.json"), join(magpieConfigDir(), "settings.json")];
    for (const f of files) {
      const j = JSON.parse(readFileSync(f, "utf8"));
      if (typeof j.url === "string" && /^https?:\/\//.test(j.url)) return j.url.replace(/\/+$/, "");
      const port = Number(j.port ?? j.gatewayPort);
      if (Number.isFinite(port) && port > 0) return `http://127.0.0.1:${port}`;
    }
  } catch {
    /* fall through */
  }
  return "http://127.0.0.1:3425";
}

async function magpie(args, timeout = 20000) {
  const bin = magpieBin();
  if (!bin) throw new Error("找不到 magpie 命令行（可用 MAGPIE_BIN 指定）");
  try {
    const { stdout, stderr } = await runFile(bin, args, {
      timeout,
      maxBuffer: 4 * 1024 * 1024,
      windowsHide: true,
      encoding: "utf8",
    });
    return { stdout, stderr };
  } catch (err) {
    const msg = redact(String(err?.stderr || err?.stdout || err?.message || err));
    throw new Error(msg.trim() || "magpie 命令失败");
  }
}

// The list is a fixed-width table; the 24-hex id starts every row. Keep the
// remaining columns raw so a multi-word state ("until ... (issued by ...)")
// doesn't shift parsing.
export async function listKeys() {
  const { stdout } = await magpie(["gateway-key", "list"]);
  const keys = [];
  for (const line of stdout.split(/\r?\n/)) {
    const m = line.match(/^([0-9a-f]{24})\s{2,}(.+)$/);
    if (!m) continue;
    const parts = m[2].split(/\s{2,}/);
    keys.push({ id: m[1], name: parts[0] || "", state: parts[1] || "", raw: m[2] });
  }
  return keys;
}

export async function ensureBank(preferredId) {
  const before = await listKeys();
  let key = preferredId ? before.find((k) => k.id === preferredId) : before.find((k) => k.name === "poker-bank");
  if (!key) {
    // `add` prints the secret on stdout. We deliberately drop it: the key is
    // only an issuer, and the CLI issues under it locally.
    await magpie(["gateway-key", "add", "poker-bank"]);
    const after = await listKeys();
    const old = new Set(before.map((k) => k.id));
    key = after.find((k) => k.name === "poker-bank" && !old.has(k.id)) || after.find((k) => k.name === "poker-bank");
  }
  if (!key) throw new Error("创建 poker-bank 失败");
  await magpie(["gateway-key", "issuer", key.id, "on"]);
  return { id: key.id, name: key.name };
}

export async function issueKey({ issuerId, name, unit, amount, ttl = "168h" }) {
  if (!issuerId) throw new Error("没有可用的 issuer key");
  const n = Number(amount);
  if (!Number.isFinite(n) || n <= 0) throw new Error("结算额度不合法");
  const args = [
    "gateway-key",
    "issue",
    "--from",
    issuerId,
    "--ttl",
    ttl,
    "--name",
    String(name || "poker-settlement").replace(/[^A-Za-z0-9._-]/g, "-").slice(0, 48),
    "--json",
  ];
  if (unit === "tokens") args.push("--tokens", String(Math.max(1, Math.round(n))));
  else args.push("--cost", n.toFixed(4));
  const { stdout } = await magpie(args, 30000);
  const start = stdout.indexOf("{");
  const end = stdout.lastIndexOf("}");
  if (start < 0 || end <= start) throw new Error("magpie 没有返回 key");
  let data;
  try {
    data = JSON.parse(stdout.slice(start, end + 1));
  } catch {
    throw new Error("magpie 返回的 key 不是 JSON");
  }
  if (!data.key || typeof data.key !== "string") throw new Error("magpie 没有返回 key");
  return { key: data.key, expires: data.expires || null, limit: data.limit || null };
}

export async function verifyKey({ url, key }) {
  let base;
  try {
    base = new URL(String(url).trim());
  } catch {
    return { ok: false, error: "网关地址不合法" };
  }
  if (!/^https?:$/.test(base.protocol)) return { ok: false, error: "只支持 http/https" };
  const path = base.pathname.replace(/\/+$/, "");
  base.pathname = /\/v1\/models$/.test(path)
    ? path
    : /\/v1$/.test(path)
      ? `${path}/models`
      : `${path}/v1/models`;
  base.search = "";
  base.hash = "";
  let res;
  try {
    res = await fetch(base, {
      headers: { authorization: `Bearer ${key}` },
      signal: AbortSignal.timeout(8000),
    });
  } catch (err) {
    return { ok: false, error: redact(String(err?.message || err)) };
  }
  if (!res.ok) return { ok: false, status: res.status, error: `HTTP ${res.status}` };
  let models = 0;
  try {
    const j = await res.json();
    models = Array.isArray(j?.data) ? j.data.length : Array.isArray(j?.models) ? j.models.length : 0;
  } catch {
    /* a 200 is enough */
  }
  return { ok: true, status: res.status, models };
}

export function redact(text) {
  return String(text).replace(/sk-[A-Za-z0-9_-]{8,}/g, "sk-***");
}
