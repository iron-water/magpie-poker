import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(fileURLToPath(new URL(".", import.meta.url)), "..", "assets", "skins");
const RANKS = "23456789TJQKA";
const SUITS = "shdc";

for (const skin of ["gilded", "saintdenis"]) {
  test(`${skin}: 52 card faces and a back`, () => {
    const dir = join(root, skin, "cards");
    const files = readdirSync(dir).filter((f) => f.endsWith(".webp"));
    assert.equal(files.length, 52, `${skin} card count`);
    for (const rank of RANKS) {
      for (const suit of SUITS) {
        const file = join(dir, `${rank}${suit}.webp`);
        assert.ok(statSync(file).size > 1000, `${skin}/${rank}${suit}`);
      }
    }
    assert.ok(statSync(join(root, skin, "card-back.webp")).size > 1000);
  });
}
