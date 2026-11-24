import { execFile } from "node:child_process";
import { existsSync } from "node:fs";
import { promisify } from "node:util";

const runFile = promisify(execFile);

export function tailscaleBin() {
  const candidates = [
    process.env.TAILSCALE_BIN,
    "C:\\Program Files\\Tailscale\\tailscale.exe",
    "C:\\Program Files (x86)\\Tailscale\\tailscale.exe",
    "/usr/bin/tailscale",
    "/usr/local/bin/tailscale",
    "/Applications/Tailscale.app/Contents/MacOS/Tailscale",
  ].filter(Boolean);
  return candidates.find((p) => p.includes("/") || p.includes("\\") ? existsSync(p) : false) || "tailscale";
}

async function run(args, timeout = 15000) {
  try {
    const { stdout, stderr } = await runFile(tailscaleBin(), args, {
      timeout,
      windowsHide: true,
      encoding: "utf8",
      maxBuffer: 4 * 1024 * 1024,
    });
    return { ok: true, stdout, stderr };
  } catch (err) {
    return { ok: false, error: String(err?.stderr || err?.message || err).trim() };
  }
}

export async function tailscaleStatus() {
  const r = await run(["status", "--json"]);
  if (!r.ok) return { installed: false, error: r.error };
  let j;
  try {
    j = JSON.parse(r.stdout);
  } catch {
    return { installed: false, error: "tailscale status 不是 JSON" };
  }
  const self = j.Self || {};
  const ips = Array.isArray(self.TailscaleIPs) ? self.TailscaleIPs : [];
  const dns = String(self.DNSName || "").replace(/\.$/, "");
  return {
    installed: true,
    name: self.HostName || "",
    dns,
    ip: ips.find((x) => /^\d+\./.test(x)) || "",
    ipv6: ips.find((x) => x.includes(":")) || "",
    online: !!self.Online,
    suffix: j.MagicDNSSuffix || "",
    error: null,
  };
}

export async function serveStatus() {
  const r = await run(["serve", "status", "--json"]);
  if (!r.ok) return { ok: false, error: r.error, config: null };
  try {
    return { ok: true, config: JSON.parse(r.stdout) };
  } catch {
    return { ok: true, config: null, raw: r.stdout };
  }
}

export async function enableServe({ targetPort, httpsPort = 8443 }) {
  const r = await run(["serve", "--bg", `--https=${httpsPort}`, `http://127.0.0.1:${targetPort}`], 30000);
  if (!r.ok) return { ok: false, error: r.error };
  const st = await tailscaleStatus();
  const host = st.dns || st.name;
  if (!host) return { ok: false, error: "没有 Tailscale DNS 名称（tailnet 可能未启用 HTTPS）" };
  return { ok: true, url: `https://${host}:${httpsPort}`, output: r.stdout.trim() };
}
