import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

export function dataDir() {
  if (process.env.MAGPIE_POKER_DATA) return process.env.MAGPIE_POKER_DATA;
  const base = process.env.MAGPIE_CONFIG_DIR
    ? process.env.MAGPIE_CONFIG_DIR
    : process.env.XDG_CONFIG_HOME
      ? join(process.env.XDG_CONFIG_HOME, "magpie")
      : join(homedir(), ".config", "magpie");
  return join(base, "magpie-poker");
}

export class Store {
  constructor(dir = dataDir()) {
    this.dir = dir;
    mkdirSync(dir, { recursive: true });
  }

  path(name) {
    return join(this.dir, name);
  }

  read(name) {
    try {
      return JSON.parse(readFileSync(this.path(name), "utf8"));
    } catch {
      return null;
    }
  }

  write(name, value) {
    const file = this.path(name);
    const tmp = `${file}.tmp`;
    const text = JSON.stringify(value, null, 2);
    writeFileSync(tmp, text, { encoding: "utf8", mode: 0o600 });
    try {
      renameSync(tmp, file);
    } catch {
      writeFileSync(file, text, { encoding: "utf8", mode: 0o600 });
    }
  }

  loadProfile() {
    const p = this.read("profile.json");
    return {
      name: p?.name || defaultName(),
      avatar: p?.avatar || "fox",
      gatewayUrl: p?.gatewayUrl || "",
    };
  }

  saveProfile(profile) {
    this.write("profile.json", profile);
  }

  saveHost(host) {
    this.write("host.json", host);
  }

  loadHost() {
    return this.read("host.json");
  }

  saveTable(table) {
    this.write(`table-${table.id}.json`, table);
  }

  loadTable(id) {
    return this.read(`table-${id}.json`);
  }

  clearHost() {
    this.write("host.json", null);
  }

  exists(name) {
    return existsSync(this.path(name));
  }
}

function defaultName() {
  try {
    const u = homedir().split(/[\\/]/).filter(Boolean).pop();
    return u || "玩家";
  } catch {
    return "玩家";
  }
}
