# 对等协议（v1）

牌桌服务只监听房主机器的 `0.0.0.0:<tablePort>`（默认 3482），只暴露 `/peer/*`。
页面永远连本机 UI 服务（默认 3481）；客人的 UI 服务再作为客户端连房主。

## 邀请码

`POKER1.` + base64url(JSON)：

```json
{
  "v": 1,
  "tableId": "t_7f3a1c2d",
  "tableName": "周五牌局",
  "host": "100.76.46.108",
  "port": 3482,
  "tls": false,
  "hostPub": "<32-byte X25519 public key, base64url>",
  "secret": "<32-byte table secret, base64url>",
  "hostName": "ironwater",
  "expiresAt": null
}
```

`secret` 是入场券，也是 HKDF 的 salt。它从不在网络上明文传输，只用于入桌证明。

Tailscale Serve 模式下，`host` 换成 MagicDNS 名，`port` 是 Serve 的 HTTPS 端口，`tls` 为 true。

## 入桌握手

1. 客人生成 X25519 身份 `(priv, pub)` 和 16 字节 `nonce`。
2. `POST /peer/join`：

```json
{
  "v": 1,
  "tableId": "t_…",
  "name": "客人",
  "avatar": "fox",
  "pub": "<base64url>",
  "nonce": "<hex>",
  "proof": "HMAC-SHA256(secret, `join1|tableId|pub|nonce`)",
  "profile": { "gatewayUrl": "https://…:8444", "tailscale": true }
}
```

3. 房主验证 `proof`，分配 `playerId`，用自己的身份做 X25519：

```
shared   = X25519(hostPriv, guestPub)
session  = HKDF-SHA256(shared, salt=secret, info=`poker1|tableId|playerId`, 32)
```

4. 响应：

```json
{ "ok": true, "tableId": "t_…", "playerId": "p_…", "seat": 2,
  "hostPub": "<base64url>", "hostProof": "HMAC(session, `host1|playerId`)", "sessionId": "…" }
```

客人算同一把 `session`，校验 `hostProof`；双方互相证明知道 `secret` 且派生的是同一把会话密钥。

## 签名请求

除 `join` / `peek` / `health` 外，每个请求：

```
x-poker-player: p_…
x-poker-ts: 1764000000000
x-poker-sig: HMAC-SHA256(session, [METHOD, path, ts, sha256hex(body)].join("\n"))
```

- `path` 只含路径（如 `/peer/action`），不含 query。
- 时间窗 ±120 秒；相同 `sig` 只接受一次（防重放）。
- SSE 用 query 传 `playerId`、`ts`、`sig`，签的是 `GET /peer/events` + 空 body。
- 房主不签下行事件：下行只走这条已由 Tailscale 加密、且由客人签名打开的长连接。

## 路由

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/peer/health`、`/peer/peek` | 牌桌名称、阶段、人数（无鉴权） |
| POST | `/peer/join` | 握手 |
| GET | `/peer/events` | SSE：`state`（按玩家裁剪）、`notice` |
| POST | `/peer/action` | `{type, amount}` |
| POST | `/peer/chat` | `{text}` |
| POST | `/peer/rebuy` | `{amount}` |
| POST | `/peer/settle` | 输家提交 `{transferId, key, expires, gatewayUrl}` |
| POST | `/peer/verify` | 赢家标记 `{transferId}` 已验证 |
| POST | `/peer/leave` | 离桌 |

## 状态裁剪

- 每个玩家的 `state` 由 `Table.viewFor(playerId)` 生成：别人的底牌是 `null`；摊牌时只亮
  进入摊牌且未弃牌的玩家。
- `settlement.transfers[].key` 只给 `to === viewer` 的玩家；其他人只看到 `issued` 状态。
- 房主进程持有完整牌桌，技术上能看到所有底牌和 key。这是信任边界，不是密码学保证。

## 断线

- SSE 断开 8 秒后房主标记该玩家 `connected=false`、`sittingOut=true`；如果正轮到他，
  立即弃牌（`markDisconnected`）。
- 同一名字的断线玩家在下一手开始前重新入桌会复用原座位和筹码（`addPlayer` 的 name reclaim）。
- 房主重启：`host.json` 保存牌桌 id、座位、X25519 身份和 `secret`，牌桌 JSON 每 250ms
  防抖落盘；重启后自动恢复并重新监听。旧邀请码继续有效。
