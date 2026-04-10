# Log Playbook

这个 reference 用于日志高频场景的首命令选择。

## 场景路由

| 用户场景 | 第一条命令 | 不要先做什么 |
|---|---|---|
| 查一段时间内日志 | `volclog log search --describe` | 不要先跑导出 |
| 按游标或 shard 消费日志 | `volclog api shard DescribeShards --describe` | 不要先跑 `search/export` |
| 导出很多原始日志 | `volclog --output-mode file log export --describe` | 不要先跑分析导出 |
| 做统计/聚合/分析 | `volclog --output-mode file log export-analysis --describe` | 不要继续用 `log export` |
| 写日志 / put logs | `volclog api log PutLogs --describe` | 不要继续留在 shortcut |

## Plain Search

用户说“查某段时间的日志”时，先用：

```bash
volclog log search --describe
```

不要先升级到 `log export`。

## Large Raw Export

用户说“导出很多原始日志”时，先用：

```bash
volclog --output-mode file log export --describe
```

## Consume

用户说“消费日志 / 按 cursor 拉日志 / 逐 shard 拉取 / 从头消费原始日志”时，先用：

```bash
volclog api shard DescribeShards --describe
```

然后按固定顺序继续：

```bash
volclog api log DescribeCursor --describe
volclog api log ConsumeOriginalLogs --describe
```

关键提醒：

- 先枚举 shard，再逐 shard 处理
- topic 级消费要求 agent 遍历全部 shard
- 原始日志包优先 `ConsumeOriginalLogs`
- 解析后的日志再用 `ConsumeLogs`
- `ConsumeOriginalLogs` 更接近写入时的原始 IO，通常更省服务端组装成本
- `ConsumeLogs` 可能看到服务端组合后的日志结果
- 要保留原始包边界时，优先普通 `json` 输出，不要先用 `jsonl`
- 不要改走 `log search` 或 `log export`

## Analysis Export

用户说“做统计/聚合/分析导出”时，先用：

```bash
volclog --output-mode file log export-analysis --describe
```

高频提醒：

- analysis 列可用性依赖索引字段是否开启 `SqlFlag`
- 新加索引字段通常偏增量生效；旧日志可能查询成功但列值仍为 `null`

## First Filters

如果结果很大，优先：

- 先缩字段
- 再落盘
- 再做后续链式处理

## 未命中时下一步

如果 shortcut 不够：

```bash
volclog capabilities --group log --view text
volclog api log <Action> --describe
```

不要直接退到 `api call`。
