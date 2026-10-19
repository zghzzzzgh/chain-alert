# chain-alert

一个用 Go 写的**链上事件监控告警机器人**：实时监听 EVM 链，按规则匹配后推送到 Telegram。

> 适合：巨鲸转账监控、ERC20 大额异动、合约事件告警、指定地址动向追踪。

## 特性

- **4 种规则**：原生币转账 / ERC20 转账 / 合约事件 / 地址监控
- **单次 RPC 调用**：每个区块只发一次 `eth_getLogs`，把所有关心的事件一次拉回
- **去重**：同一 `(txHash, rule)` 24 小时内只告警一次
- **优雅退出**：`Ctrl+C` 干净停止
- **可测试**：`-once` 只扫一个区块，方便冒烟测试
- **无 Telegram 也能跑**：未配置 bot 时自动降级为 stdout 输出

## 快速开始

```bash
git clone https://github.com/<your-name>/chain-alert.git
cd chain-alert
cp config.example.yaml config.yaml
# 编辑 config.yaml，填入 rpc_url 和 telegram 凭证
go build -o chain-alert .
./chain-alert -config config.yaml
```

只想试一下、不发 Telegram：

```bash
./chain-alert -config config.yaml -once
```

## 实测输出

在以太坊主网第 `26224365` 区块上跑 `-once`，抓到：

```
2026/10/19 12:55:06 scanning block 26224365 once
[ALERT] <b>Whale ETH transfer</b>
Rule: ETH whale
Amount: <b>1420.2800 ETH</b>
From: 0xA9Ac43f5b5e38155A288d1A01D2cbc4478E14573
To:   0x0003B5Aa5E30E97FcC596BB5D0F3A75255E08D4e
Block: 26224365
Tx: https://etherscan.io/tx/0xc3422218...

[ALERT] <b>USDT large transfer</b>
Amount: <b>1552787.48 USDT</b>
...
[ALERT] <b>USDT large transfer</b>
Amount: <b>3528000.00 USDT</b>
...
```

## 两种运行模式

| 模式 | 传输 | 延迟 | 适用 |
|---|---|---|---|
| `poll`（默认） | HTTP 轮询 `eth_blockNumber` | 12–24 秒 | 任何 HTTP RPC |
| `ws` | WebSocket `eth_subscribe("newHeads")` | **~1 秒** | 支持 wss:// 的节点 |

```yaml
chain:
  rpc_url: "https://ethereum-rpc.publicnode.com"
  ws_url: "wss://ethereum-rpc.publicnode.com"
  use_websocket: true   # false = 轮询
```

> ⚠️ WebSocket 模式下 `confirmations` **不生效** —— 告警在区块头到达时立即发出。
> 需要等待确认数时请用 `poll` 模式。

实测（WS 模式，区块 26226363 到达后立即触发）：

```
websocket mode: subscribed to newHeads (confirmations ignored)
[ALERT] Whale ETH transfer
Amount: 12.9794 ETH
Block: 26226363
```

## 配置说明

```yaml
chain:
  name: ethereum
  rpc_url: "https://ethereum-rpc.publicnode.com"   # 换成自己的节点
  start_block: 0          # 0 = 从最新区块开始
  confirmations: 1        # 等待 N 个确认
  poll_interval_seconds: 12

telegram:
  bot_token: "123456:ABC..."   # @BotFather 创建
  chat_id: "123456789"         # 给 bot 发消息后查 getUpdates

rules:
  - name: "ETH whale"
    type: native_transfer
    threshold_eth: 1000

  - name: "USDT large"
    type: erc20_transfer
    token: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    token_symbol: "USDT"
    decimals: 6
    threshold: 1000000

  - name: "Uniswap Swap"
    type: contract_event
    address: "0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"
    event_signature: "Swap(address,uint256,uint256,uint256,uint256,address)"

  - name: "Watched"
    type: address_watch
    addresses:
      - "0x00000000219ab540356cBB839Cbe05303d7705Fa"
```

### 规则类型

| type | 必填字段 | 说明 |
|---|---|---|
| `native_transfer` | `threshold_eth` | 原生币（ETH）转账超过阈值 |
| `erc20_transfer` | `token` `decimals` `threshold` | ERC20 转账超过阈值 |
| `contract_event` | `address` `event_signature` | 指定合约的指定事件 |
| `address_watch` | `addresses` | 关注地址的任何交易 |

> `event_signature` 只写类型、不写参数名：`Swap(address,uint256,uint256)`。
> 它会自动 `keccak256` 成 topic0 去匹配日志。

## 架构

```
main.go
  └─ internal/config    YAML 解析 + 校验
  └─ internal/monitor   区块轮询、规则匹配、去重
  └─ internal/notify    Telegram / stdout 输出
```

数据流：

```
ticker ──▶ BlockNumber ──▶ BlockByNumber ──▶ 遍历交易
                                      └──▶ eth_getLogs（一次，按地址过滤）
                                              └──▶ 规则匹配 ──▶ 去重 ──▶ Telegram
```

## 关键实现细节

- **事件匹配**：不依赖 ABI 文件，直接用 `keccak256(签名)` 算出 `topic0`，从 `Topics`/`Data` 里解出地址和金额。
- **单次 RPC**：把多条日志规则的地址合并成一个 `addresses` 过滤，避免每个区块发多次 `eth_getLogs`。
- **去重**：`map[txHash|ruleName]time.Time`，`prune()` 每轮清理 24h 前的条目。
- **优雅退出**：`signal.NotifyContext` + `context` 贯穿整个调用链。

## 已知限制 / Roadmap

- [ ] 支持多链（当前 `txLink` 写死 etherscan，可改成配置项）
- [ ] 支持 WebSocket 订阅（`eth_subscribe newHeads`），比轮询更实时
- [ ] 支持持久化去重（重启后不重复告警）
- [ ] 支持正则/表达式规则
- [ ] 支持 Discord / 钉钉 / 企业微信

## License

MIT