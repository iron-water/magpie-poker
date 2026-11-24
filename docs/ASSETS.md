# 素材清单（给 GPT Image 2.6）

页面已经在用这 6 张图；缺图时全部有 CSS/SVG 兜底，不会破版。生成后按文件名放进
仓库根目录的 `assets/`，不用改代码。牌面、头像、筹码面值都是 CSS/DOM 画的，**不要**
生成 52 张牌或头像图。

## 统一风格

- 色调：牌桌绿 `#0F2A22` / `#12332B`，金 `#D9B45B`，奶油 `#F2EAD7`，酒红 `#7A2E2E`。
  暗色、低饱和、暖色顶光；不要霓虹。
- 物体图：透明背景、无投影（阴影由 CSS 加）、正俯视、边缘干净。
- 背景图：满幅、无文字、无人物、无水印；中心区域留安静，UI 会盖在上面。
- 禁止：文字/数字/品牌 logo/签名/水印/牌面点数。筹码中心必须空白（面值由 CSS 叠字）。
- 同一轮生成时固定 seed 和风格参考，四枚筹码必须像同一套。

## 1. 桌面（必须）

**`assets/table-felt.webp`** · 2560×1440 · 满幅

```text
Top-down view of a dark emerald poker table felt, subtle diamond weave texture,
soft warm overhead light in the center, gentle vignette at the edges,
no cards, no chips, no text, no logos, photorealistic, quiet center area for UI
```

## 2. 牌背（必须）

**`assets/card-back.webp`** · 512×720 · 圆角外透明

```text
Poker card back design, deep emerald green with a thin antique gold art-deco border
and a centered gold spade motif, perfectly symmetrical, flat vector style,
crisp edges, no text, no letters, transparent outside the rounded card,
slight inner border, printable playing-card proportions
```

## 3. 筹码（必须，4 枚一套）

统一提示词模板：

```text
Single casino poker chip viewed perfectly from above, {COLOR} clay with {SPOT} edge spots,
worn matte texture, blank center with a thin gold ring, no numbers, no text,
transparent background, soft even studio light, same set as the other chips
```

| 文件 | COLOR | SPOT |
|---|---|---|
| `assets/chip-red.webp` | `deep oxblood red` | `cream` |
| `assets/chip-gold.webp` | `antique gold` | `dark green` |
| `assets/chip-blue.webp` | `muted navy blue` | `cream` |
| `assets/chip-dark.webp` | `charcoal black` | `gold` |

尺寸 256×256。四枚的直径、光照、边缘厚度要一致；中心留空给 CSS 写面值。

## 4. 可选（现在没接线，先不用画）

- `assets/og-cover.png` 1200×630：GitHub 社交预览，深绿桌面 + 中央金色黑桃筹码，无文字。
- `assets/settlement-bg.webp` 1600×900：结算页背景，暗绿 + 金色径向光，无物体无文字。
- 头像目前用 emoji（🦊🐼🐯🐸🦉🐙🦄🐱）；以后要做成图片再说。

## 5. 落地

- 转 WebP：`cwebp -q 88`，透明图保留 alpha；单张 ≤ 300KB，桌面 ≤ 600KB。
- 文件名必须与上表完全一致；页面在 `web/style.css` 末尾的 `html.use-art` 里引用。
- `assets/logo.svg` 已是手写 SVG，不用生成；`assets/icon.svg` 是 magpie 插件图标。
