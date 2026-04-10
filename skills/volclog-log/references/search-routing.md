# Search Routing

这个 reference 用于决定日志查询应该进哪个 shortcut。

## Routing Table

| 用户意图 | 默认命令 | 适用场景 |
|---|---|---|
| 看一段时间内的日志 | `volclog log search --describe` | 普通检索, 结果量中等 |
| 按 shard / cursor 消费日志 | `volclog api shard DescribeShards --describe` | 消费型动作, 后续逐 shard `DescribeCursor` + `Consume*` |
| 拉较大范围原始日志 | `volclog log export --describe` | 原始日志导出, 更偏全量或落盘 |
| SQL / 统计 / 聚合 / 分析结果导出 | `volclog log export-analysis --describe` | 分析型查询, 输出行为结果集 |
| 写日志 / put logs / web tracking | `volclog api log PutLogs --describe` | 写入型动作, 不属于 shortcut 检索路径 |

## Query Heuristics

- `Query` 不带分析段: 优先 `log search` 或 `log export`
- `Query` 中带 `|select`, `|with`, `|insert`, 聚合函数, 统计语义: 优先 `log export-analysis`
- 用户说“写日志 / put logs / web tracking”时，直接退出这个 routing，改走 `api log`
- 用户说“consume / cursor / shard / pull logs / 原始日志消费”时，直接退出这个 routing，改走 `DescribeShards -> DescribeCursor -> Consume*`

## Escalation

如果现有 shortcut 不够表达需求，再切到 `volclog-api-explorer`。

但如果 group 已明确是 `log`，优先：

```bash
volclog capabilities --group log --view text
volclog api log <Action> --describe
```
