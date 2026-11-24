# 素材来源与分类

从 `C:\Downloads` 的 2026-11-25 批次里按下载来源分成两套；原始文件在
`C:\tmp\work\2026-11-25-poker-assets\classified\`（仓库只放压缩后的成品）。

| 来源 | 数量 | 识别依据 | 用途 |
|---|---|---|---|
| **ChatGPT** | 48 | 命名文件 + ChatGPT 下载的 UUID PNG，透明底 | **鎏金象牙**皮肤 |
| **Gemini** | 19 | UUID WebP，白底/卡片切图 | **圣丹尼**皮肤 |

## 鎏金象牙（ChatGPT）

- 牌面：ChatGPT 的 Q/J/K/A 宫廷牌 + 四花色雕版图；2–10 用精确点阵重绘（不是 GPT 画点数）。
- 牌背：`Vintage Ornate Gold Filigree Card Back-4`。
- 头像：狐狸、熊猫、老虎、猫头鹰、章鱼、独角兽 6 张油画。
- 筹码/图标/标记：5 枚筹码、5 个动作徽章、象牙/黄铜/酒红标记、PAID 印章。
- 桌面/背景：绿呢木沿桌面、牌房、大厅、结算书桌。

## 圣丹尼（Gemini）

- 牌面：Gemini 的 2–A 大牌面（13 张）逐张切出；原图星星位置换成蜡封式花色徽章。
- 花色字形：两套共用 ChatGPT 的雕版花色（圣丹尼用于蜡封，鎏金用于点阵/角标）。
- 牌背：Gemini 酒红金鸢尾牌背。
- 头像：Gemini 8 格动物肖像（狐/熊猫/虎/猫头鹰/章鱼/独角兽/猫/狼）。
- 筹码/图标：Gemini 5 枚筹码、5 个线稿动作图标。
- PAID 印章：Gemini 原图的 PAID 被画布裁切，两套共用 ChatGPT 的完整 PAID 印章。
- 桌面/背景：Gemini 绿呢桌面、圣丹尼酒馆牌房。

## 牌面重建

两套各 52 张，统一 512×768 WebP，文件名 `{rank}{suit}.webp`（`T` 表示 10）：

- 鎏金：2–10 精确点阵；J/Q/K 用 ChatGPT 宫廷图，只把角上的黑桃用同色纸质花色徽章盖掉；
  A♠ 用原图，A♥/A♦/A♣ 用大花色 + 边框。
- 圣丹尼：13 张大牌面 + 两个蜡封花色徽章（原星星位置），4 花色 × 13 点数。
- 两套都经过 52 张预览图人工检查；`test/skins.test.mjs` 校验 52 个文件与牌背存在。

## 成品位置

- `assets/skins/gilded/`：`cards/` 52 张、`card-back.webp`、头像、筹码、图标、标记、背景。
- `assets/skins/saintdenis/`：同上。
- UI 通过 `web/skins.js` 设置 CSS 变量，`web/cards.js` 读取当前皮肤并加载对应牌面。
