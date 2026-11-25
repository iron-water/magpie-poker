import { spawn } from "node:child_process";
import { chmodSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(fileURLToPath(import.meta.url));
function binaryName() {
  if (process.platform === "win32") return "magpie-poker.exe";
  if (process.platform === "darwin" && process.arch === "arm64") return "magpie-poker-darwin-arm64";
  if (process.platform === "linux" && process.arch === "x64") return "magpie-poker-linux-amd64";
  return "magpie-poker";
}
const bin = join(root, "bin", binaryName());
const VERSION = "0.3.0";
const HEALTH_MS = 1500;
const START_MS = 15000;

let child = null;
let starting = null;
let monitor = null;
let misses = 0;

async function health(port) {
  try {
    const res = await fetch(`http://127.0.0.1:${port}/api/health`, {
      signal: AbortSignal.timeout(HEALTH_MS),
    });
    if (!res.ok) return null;
    const j = await res.json();
    if (j?.name !== "magpie-poker") return null;
    return j;
  } catch {
    return null;
  }
}

async function waitFor(port) {
  const until = Date.now() + START_MS;
  while (Date.now() < until) {
    const j = await health(port);
    if (j && j.version === VERSION) return j;
    await new Promise((r) => setTimeout(r, 250));
  }
  return null;
}

function startMonitor(port, tablePort) {
  if (monitor) return;
  monitor = setInterval(async () => {
    const j = await health(port);
    if (j && j.version === VERSION) {
      misses = 0;
      return;
    }
    misses += 1;
    if (misses < 3) return;
    misses = 0;
    console.error(`[poker] service on :${port} stopped; restarting`);
    if (child && !child.killed) {
      try { child.kill(); } catch { /* gone */ }
    }
    child = null;
    starting = null;
    try {
      await start({ port, tablePort });
    } catch (err) {
      console.error(`[poker] restart failed: ${err?.message || err}`);
    }
  }, 2000);
  monitor.unref?.();
}

async function start(options = {}) {
  if (starting) return starting;
  starting = (async () => {
    const port = Number(options?.port) || 3481;
    const tablePort = Number(options?.tablePort) || 3482;
    const j = await health(port);
    if (j && j.version === VERSION) {
      console.log(`[poker] reusing Go service on http://127.0.0.1:${port}`);
      startMonitor(port, tablePort);
      return;
    }
    if (j && j.version !== VERSION) {
      throw new Error(`service on :${port} is ${j.version}, expected ${VERSION}; stop the old service first`);
    }
    if (!existsSync(bin)) {
      throw new Error(`missing Go binary: ${bin} (build it with: go build -o bin/magpie-poker.exe ./cmd/magpie-poker)`);
    }
    if (process.platform !== "win32") {
      try { chmodSync(bin, 0o755); } catch { /* a read-only install still gets an EACCES below */ }
    }
    let spawnError = null;
    child = spawn(bin, [
      "--port", String(port),
      "--table-port", String(tablePort),
      "--web-root", root,
    ], {
      cwd: root,
      stdio: ["ignore", "inherit", "inherit"],
      windowsHide: true,
    });
    child.on("exit", (code) => {
      console.log(`[poker] Go service exited (${code})`);
      child = null;
    });
    child.on("error", (err) => {
      spawnError = err;
      console.error(`[poker] Go service failed: ${err?.message || err}`);
      child = null;
    });
    const up = await waitFor(port);
    if (!up) {
      throw new Error(spawnError
        ? `cannot start ${bin}: ${spawnError.message}`
        : `Go service did not answer on 127.0.0.1:${port}`);
    }
    console.log(`[poker] Go service ready on http://127.0.0.1:${port}`);
    startMonitor(port, tablePort);
  })().finally(() => {
    starting = null;
  });
  return starting;
}

function stopChild() {
  if (child && !child.killed) {
    try { child.kill(); } catch { /* ignore */ }
  }
}
process.on("exit", stopChild);
process.on("SIGTERM", () => { stopChild(); process.exit(0); });
process.on("SIGINT", () => { stopChild(); process.exit(0); });

// magpie's Bun host calls exported functions as plugin entries.
export default {
  id: "magpie-poker",
  server: async (_input, options) => {
    await start(options);
    return {};
  },
};
