# 契约：页面 ⇄ 本地服务

页面（`web/index.html`）只和**本机**服务说话（同源），不直接连牌桌。本机服务在 host 模式下
同时跑牌桌引擎，在 guest 模式下是到 host 的对等客户端。这样 magpie 页面的 CSP、以及
每个玩家本地签发 key 都不受影响。

## 1. 页面拿到的状态

`GET /api/state` 与 SSE `GET /api/events` 的 `state` 事件都是同一个形状。未列出的字段
前端必须忽略；后端可以追加字段。

```json
{
  "mode": "idle | host | guest",
  "version": "0.1.0",
  "me": { "id": "p_ab12", "seat": 0, "name": "Mio", "isHost": true },
  "profile": {
    "name": "Mio", "avatar": "fox",
    "gatewayUrl": "https://ironwater.example-tailnet.ts.net:8444"
  },
  "host": {
    "name": "ironwater",
    "ip": "100.76.46.108",
    "dns": "ironwater.example-tailnet.ts.net",
    "tailscale": true,
    "serve": "https://host.tailnet.ts.net:8443",
    "serveGateway": "https://host.tailnet.ts.net:8444",
    "error": null
  },
  "table": null,
  "notice": null,
  "error": null
}
```

`table` 为 null 表示未入桌。牌桌形状：

```json
{
  "id": "t_7f3a",
  "name": "周五牌局",
  "phase": "lobby | hand | showdown | ended",
  "handNo": 3,
  "config": {
    "smallBlind": 25, "bigBlind": 50, "ante": 0,
    "startingChips": 10000, "maxSeats": 9,
    "unit": "cost", "chipValue": 0.0005,
    "currency": "USD", "actionSeconds": 30
  },
  "dealerSeat": 2,
  "toAct": "p_ab12",
  "currentBet": 150,
  "minRaiseTo": 300,
  "pot": 1200,
  "pots": [{ "amount": 1000, "eligible": ["p_ab12", "p_cd34"] }],
  "board": ["As", "Kd", "7c"],
  "seats": [
    {
      "seat": 0, "id": "p_ab12", "name": "Mio", "avatar": "fox",
      "chips": 9750, "bet": 50, "committed": 250,
      "folded": false, "allIn": false, "out": false, "sittingOut": false,
      "isDealer": true, "isSB": false, "isBB": false,
      "cards": ["As", "Kd"], "cardsCount": 2,
      "lastAction": "call 50", "isMe": true, "mayRaise": true,
      "timeLeft": 24
    }
  ],
  "me": {
    "id": "p_ab12", "seat": 0, "cards": ["As", "Kd"],
    "legal": {
      "fold": true, "check": false, "call": 50,
      "bet": null,
      "raise": { "min": 300, "max": 9750 },
      "allIn": 9750
    },
    "handStrength": { "label": "一对 A", "category": 1, "score": 1234567 }
  },
  "log": [
    { "at": 1764000000000, "kind": "system|deal|action|chat|result", "text": "Mio 加注到 300" }
  ],
  "showdown": {
    "revealed": [
      { "id": "p_ab12", "name": "Mio", "cards": ["As", "Kd"], "hand": "两对，A 与 K", "won": 1200 }
    ],
    "pots": [{ "amount": 1000, "winners": ["p_ab12"] }]
  },
  "deadline": 1764000030000,
  "settlement": {
    "at": 1764000100000,
    "transfers": [
      {
        "id": "x_1", "from": "p_ab12", "fromName": "Mio",
        "to": "p_cd34", "toName": "Kai",
        "amountChips": 3000, "amount": 1.5, "unit": "cost",
        "status": "pending | issued | verified | failed | small",
        "gatewayUrl": "https://kai.example-tailnet.ts.net:8444",
        "key": "sk-… (只给收款人，其他人是 null)",
        "expires": "2026-12-01T00:00:00Z",
        "error": null
      }
    ]
  },
  "invite": {
    "code": "POKER1.eyJ2IjoxLC…",
    "direct": "http://100.76.46.108:3482/#POKER1.eyJ2IjoxLC…",
    "served": "https://host.tailnet.ts.net:8443/#POKER1.eyJ2IjoxLC…",
    "servedCode": "POKER1.eyJ2IjoxLC…",
    "tablePort": 3482
  }
}
```

规则：

- `seats` 固定按 seat 升序；空座不出现在数组里，前端按 `seat` 放置。
- 别人的底牌：`cards` 为 `null`，`cardsCount` 为 2（或 0）。河牌摊牌时，进入过摊牌的
  玩家 `cards` 才会出现；弃牌者永远不亮牌。
- `me.legal` 只在轮到自己时非 null；`bet` 表示「下注到多少」，`raise.min` 是「加注到」的最小值，
  `allIn` 是自己剩余筹码（不是总额）。
- `timeLeft` 秒，行动倒计时；`deadline` 是毫秒时间戳。
- `settlement.transfers[].key` 只对 `to` 是本机玩家时给值。

## 2. 页面可调用的接口

全部 POST 是 JSON。响应统一 `{ ok: true, ... }` 或 `{ ok: false, error: "人类可读的原因" }`，
HTTP 400。SSE 会推新状态，页面不必轮询。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/state` | 当前状态 |
| GET | `/api/events` | SSE：`state`、`notice`、`error`，每 15s 一条注释心跳 |
| POST | `/api/profile` | `{ name, avatar, gatewayUrl }` |
| POST | `/api/table/host` | `{ name, config }`，`name` 是牌桌名；玩家名来自 profile。返回 `{ invite, table }` |
| POST | `/api/table/join` | `{ code }`，用邀请码入桌 |
| POST | `/api/table/leave` | 离桌；host 离桌 = 解散 |
| POST | `/api/table/start` | host：开始一手 |
| POST | `/api/table/next` | host：结算完进入下一手 |
| POST | `/api/table/rebuy` | `{ amount }` 补码（两手之间） |
| POST | `/api/table/end` | host：结束牌局并进入结算 |
| POST | `/api/action` | `{ type: "fold|check|call|bet|raise|allin", amount }`，`bet/raise` 的 amount 是「到」 |
| POST | `/api/chat` | `{ text }` |
| POST | `/api/settlement/issue` | `{ transferId }`：本机是欠款方时签发 key |
| POST | `/api/settlement/verify` | `{ transferId }`：本机是收款方时验证 key |
| GET | `/api/keys` | 本机 magpie gateway key 与 issuer 状态 |
| POST | `/api/keys/bank` | `{ id? }`：把某把 key 设为 issuer；不传则新建 `poker-bank` |
| POST | `/api/keys/verify` | `{ url, key }`：用 key 请求对方网关 `/v1/models` |
| GET | `/api/tailscale` | `{ installed, ip, dns, name, serve }` |
| POST | `/api/tailscale/serve` | `{ port?: 8443 }`：把牌桌服务挂到 Tailscale Serve |
| POST | `/api/tailscale/serve-gateway` | `{ port?: 8444 }`：把本机网关挂到 Serve |
| GET | `/api/health` | `{ ok, uptime, version }` |

`/api/state` 里还包含：

```json
"keys": {
  "magpie": true, "bin": "magpie.com",
  "issuer": { "id": "512b…", "name": "poker-bank" },
  "all": [{ "id": "…", "name": "…", "state": "enabled", "raw": "…" }],
  "error": null
}
```

顶层还有 `connection: { status: "online|offline|connecting|closed|null" }`（guest 的对等连接状态）。

## 3. SSE 事件

```text
event: state
data: { …完整 state… }

event: notice
data: { "level": "info|warn|error", "text": "…" }
```

前端在 `onerror` 后 1s 重连；EventSource 自带重连，但页面要处理 `mode` 从 idle 变 host/guest。

## 4. 视觉与交互要求

- 中文界面；金额同时显示筹码与本位币（`¥/$` 由 `currency` 决定）。
- 桌面 1280×800 起，最小 960×640；座位最多 9 个，自己在正下方。
- 牌桌椭圆；每个座位：头像、名字、筹码、下注、状态徽标、倒计时环。
- 公共牌 5 张居中；底池与边池在牌上方。
- 操作条：弃牌 / 过牌 / 跟注 X / 下注或加注（滑杆 + 输入 + 1/2 池、池、全下）。
- 右上：邀请按钮（复制邀请码/链接）、日志开关、设置。
- 大厅：建桌表单（盲注、带入、人数、单位与兑换率、行动秒数），加入表单（粘贴邀请码）。
- 结算：每位对手一张卡，欠款方显示「签发 key」，收款方显示 key 与复制/验证。
- 不用外部字体、图片 CDN；素材缺失时用 CSS 渐变/内联 SVG 兜底。
- `?demo=1` 用内置示例状态渲染，便于无后端预览（保留这个模式）。
