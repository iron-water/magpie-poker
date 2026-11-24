> 实际两套皮肤已落地：`assets/skins/gilded`（ChatGPT · 鎏金象牙）与
> `assets/skins/saintdenis`（Gemini · 圣丹尼）。本文件保留生成时的提示词与风格基准。

# 素材清单与确切提示词（RDR2 南部城市牌局风）

参考基准：**Red Dead Redemption 2 圣丹尼（Saint Denis）的 La Bastille Saloon**——绿呢牌桌、
黄铜吊灯、桃花心木、红丝绒/锦缎墙、煤气灯、雪茄薄雾、1899 年南方城市的浮华感。不是现代赌场，
不是霓虹，不是塑料亮面。参考的是氛围与材质，不复制游戏原画、角色或 logo。

- 资料：<https://reddead.fandom.com/wiki/Bastille_Saloon>（Saint Denis 的上流酒馆）
- 画面要点：绿色台呢、黄铜吊灯、华丽吧台、红丝绒墙、暖色煤气灯、正式着装。

## 全局风格块（每条提示词都追加）

```text
1899 Southern Gothic saloon poker room in a New Orleans-inspired city, Red Dead Redemption 2
Saint Denis atmosphere. Carved mahogany, aged brass, deep bottle-green baize, oxblood leather,
red damask, aged ivory paper, warm gaslight, faint cigar haze. Palette: deep green #0B3A2B,
brass #C9A35C, candlelight #E8CF9A, oxblood #6E2A26, aged ivory #EFE4C9, shadow #1B120D.
Cinematic, muted, photorealistic, warm highlights, deep shadows.
No modern objects, no neon, no plastic, no chrome, no text, no logos, no watermark.
```

物体统一加：`centered, isolated on a transparent background, no shadow`。
背景统一加：`no people, no text, no logos, cinematic, 1600x900`。

**负面词**：`modern casino, neon, LED, plastic, glossy 3D render, cartoon, anime, oversaturated, text, numbers, watermark, logo, brand, people, hands`。

---

## P0 · 现在生效（6 张）

### `table-felt.webp` · 2560×1440

```text
Top-down view of a deep bottle-green baize poker table surface, subtle worn damask weave,
a thin hand-tooled brass border line near the edge, faint cigar haze, warm gaslight falling
from the upper left, soft vignette, carved mahogany rail just visible at the extreme edge,
1899 Southern Gothic saloon, Red Dead Redemption 2 Saint Denis atmosphere, cinematic, muted,
photorealistic, no cards, no chips, no text, no logos, 2560x1440.
```

### `card-back.webp` · 512×720

```text
1890s playing card back, oxblood burgundy paper, ornate gold art-nouveau fleur-de-lis and
scrollwork border, perfectly symmetrical, aged edges, letterpress texture, no text, no letters,
no numbers, rounded corners, isolated on transparent background, 512x720.
```

### 四枚通用筹码 · 256×256

模板：

```text
Single 1890s clay poker chip viewed from above, {COLOR} clay with {SPOT} edge spots, worn matte
surface, chipped edges, blank center with a thin brass ring, no numbers, no text, isolated on
transparent background, soft warm gaslight, 256x256.
```

| 文件 | COLOR | SPOT |
|---|---|---|
| `chip-red.webp` | `oxblood #6E2A26` | `aged ivory` |
| `chip-gold.webp` | `aged brass #C9A35C` | `deep bottle green` |
| `chip-blue.webp` | `faded navy #2F4858` | `aged ivory` |
| `chip-dark.webp` | `charcoal #1B1B18` | `aged brass` |

---

## P1 · 已接钩子（29 张）

### 头像 · 8 张 · 256×256

模板：

```text
Oil-painted bust portrait of an anthropomorphic {ANIMAL}, 1899 Southern gentleman or lady,
{OUTFIT}, dark mahogany saloon background, warm gaslight rim light, sepia and muted green/brass
palette, Red Dead Redemption 2 Saint Denis atmosphere, centered, square, opaque background,
no text, no border, no logo, 256x256.
```

| 文件 | ANIMAL | OUTFIT |
|---|---|---|
| `avatar-fox.webp` | `red fox` | `burgundy waistcoat, cream cravat, bowler hat` |
| `avatar-panda.webp` | `giant panda` | `black tailcoat, white gloves, emerald cravat` |
| `avatar-tiger.webp` | `tiger` | `cream linen suit, panama hat, brass watch chain` |
| `avatar-frog.webp` | `tree frog` | `green brocade vest, monocle, ivory cravat` |
| `avatar-owl.webp` | `barn owl` | `black frock coat, ivory ascot, brass spectacles` |
| `avatar-octopus.webp` | `octopus` | `navy frock coat, pearl pin, burgundy cravat` |
| `avatar-unicorn.webp` | `white unicorn` | `cream suit, gold watch chain, ivory cravat` |
| `avatar-cat.webp` | `black cat` | `burgundy velvet dress, lace collar, cameo brooch` |

背景必须不透明，否则底下的 emoji 会透出来。

### 背景 · 3 张 · 1600×900

**`bg-lobby.webp`**

```text
Interior of an opulent 1899 Southern Gothic saloon at night, carved mahogany bar, red damask
wallpaper, brass chandelier, gas lamps, oxblood leather chairs, a deep green baize poker table
in the distance, warm sepia light, faint cigar haze, no people, no text, no logos, dark negative
space on the left for UI, cinematic, 1600x900.
```

**`bg-room.webp`**

```text
Overhead view of a private 1899 Southern saloon card room, deep green baize table, carved
mahogany chairs, red damask walls, brass chandelier, warm gaslight, faint cigar haze, no people,
no text, no logos, quiet, cinematic, 1600x900.
```

**`bg-settlement.webp`**

```text
Close view of an 1899 saloon desk, dark green baize blotter, carved mahogany, brass desk lamp,
aged ledger paper, red wax seal, sepia, warm gaslight, shallow depth of field, no text, no people,
no logos, 1600x900.
```

### 桌面标记 · 5 张 · 透明

| 文件 | 尺寸 | 提示词 |
|---|---|---|
| `dealer-button.webp` | 128×128 | `1890s ivory dealer puck with an aged brass rim, blank face, top view, isolated on transparent background, no text, 128x128.` |
| `empty-seat.webp` | 256×256 | `Vintage oval brass nameplate resting on green baize, blank center, worn edges, top-down, isolated on transparent background, no text, 256x256.` |
| `marker-sb.webp` | 128×128 | `Small ivory ceramic marker with a thin brass rim, blank face, top view, isolated on transparent background, no text, 128x128.` |
| `marker-bb.webp` | 128×128 | `Small oxblood ceramic marker with a thin brass rim, blank face, top view, isolated on transparent background, no text, 128x128.` |
| `stamp-allin.webp` | 256×256 | `Distressed oxblood rubber-stamp ring, blank center, ink texture, isolated on transparent background, no text, 256x256.` |

文字 `D / SB / BB / 全下` 由 CSS 叠上去。

### 筹码面值 · 5 张 · 256×256

同一模板：

```text
Single 1890s clay poker chip viewed from above, {COLOR} clay with {SPOT} edge spots, worn matte
surface, chipped edges, blank center with a thin brass ring, no numbers, no text, isolated on
transparent background, soft warm gaslight, 256x256.
```

| 文件 | COLOR | SPOT |
|---|---|---|
| `chip-25.webp` | `deep bottle green` | `aged ivory` |
| `chip-100.webp` | `charcoal` | `aged brass` |
| `chip-500.webp` | `oxblood` | `aged ivory` |
| `chip-1000.webp` | `faded plum` | `aged brass` |
| `chip-5000.webp` | `aged brass` | `deep bottle green` |

UI 只显示 10px 小圆点，靠颜色区分；不要画数字。

### 动作图标 · 5 张 · 128×128

模板：

```text
1890s engraved brass line icon of {THING}, thick 6px strokes, art-nouveau, isolated on transparent
background, cream and brass, no text, no shadow, 128x128.
```

| 文件 | THING |
|---|---|
| `icon-fold.webp` | `a folded playing card with a small x` |
| `icon-check.webp` | `a hand tapping a green baize table` |
| `icon-call.webp` | `a stack of clay chips with a small arrow` |
| `icon-raise.webp` | `an upward arrow over a chip stack` |
| `icon-allin.webp` | `a chip stack with a small flame` |

只在 ≥1100px 显示；960px 保持纯文字。

### 结算 · 3 张

| 文件 | 尺寸 | 提示词 |
|---|---|---|
| `trophy.webp` | 512×512 | `1890s silver and brass poker trophy, a spade engraved on the cup, carved mahogany base, red wax seal, isolated on transparent background, no text, 512x512.` |
| `key-card.webp` | 1024×512 | `1890s claim ticket, aged ivory paper, engraved brass corners, deep green and oxblood accents, blank center for key text, no text, no logos, 1024x512.` |
| `stamp-paid.webp` | 512×512 | `1890s red ink rubber stamp with the word PAID in letterpress serif capitals, distressed edges, isolated on transparent background, 512x512.` |

如果 `PAID` 画糊了，重生成或改成空白印章；UI 不依赖文字。

---

## P2 · 牌面（22 张，暂不接）

不要画 52 张。画 13 张点数 + 4 张花色，用 CSS/SVG 拼：

- `rank-2.webp` … `rank-a.webp`（13 张，256×256）：
  `An engraved 1890s playing-card rank {RANK} centered on aged ivory paper, no suit, no border,
  isolated on transparent background, 256x256.`
- `suit-s.webp` / `suit-h.webp` / `suit-d.webp` / `suit-c.webp`（4 张，128×128）：
  `An engraved 1890s {SUIT} suit symbol, aged brass ink on transparent background, 128x128.`
- `joker-1.webp` / `joker-2.webp`（可选）
- `card-back-2.webp` / `card-back-3.webp`（2 张，512×720，墨绿/酒红变体）
- `card-front-texture.webp`（1 张，512×720，象牙纸纹，无点数）

接上后牌面仍是 DOM/SVG 组合，点数数量绝对正确。

---

## P3 · 营销与变体（最后）

- `app-icon-1024.png`：`App icon, brass spade over oxblood leather and deep green baize, art-nouveau gold border, flat vector, no text, 1024x1024.`
- `og-cover.png` 1200×630：圣丹尼牌房全景，左侧留标题位，无文字。
- `felt-blue.webp` / `felt-red.webp`：换色桌布。
- `table-rim-1/2/3.webp`：桃花心木/黄铜桌沿。
- `avatar-frame-1/2/3/4.webp`：黄铜/乌木/红丝绒段位框。
- `qr-frame.webp`：1890s 票据边框。
- `loader.webp`：黄铜怀表/转轮静帧。

---

## 生成顺序

1. **P0 六张**：先出桌布和牌背，确定绿呢/黄铜/象牙的基准色。
2. **头像 8 张**：用第一张狐狸作为风格参考（GPT 的 reference/image-to-image），保证同一画风。
3. **背景 3 张**：大厅 → 牌房 → 结算。
4. **标记 5 张 + 面值筹码 5 张**。
5. **动作图标 5 张 + 结算 3 张**。
6. **P2 牌面**：告诉我，我接拼牌逻辑。
7. **P3 营销图**。

## 落地

- GPT Image 2.6 出 PNG；透明图确认 alpha，不要棋盘格。
- 转 WebP `cwebp -q 88`；单张 ≤300KB，背景 ≤600KB。
- 文件名必须完全一致，放进仓库根目录 `assets/`。
- 更新插件：`magpie plugin add github:iron-water/magpie-poker`，重启/重载页面。
- `?demo=1` 永远走纯 CSS；正常页面才加载图片，缺图不会破版。
