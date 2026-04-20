# Metric Topic Routing

## Intent Split

| 用户意图 | 默认命令 |
|---|---|
| 列/查/建/改/删指标主题 | `volclog metric-topic <list|get|create|modify|delete> --describe` |
| PromQL 查询, 指标查询, time series 检索 | `volclog metric-topic search --describe` |

## Escalation

这些情况升级到 explorer:

- shortcut 不覆盖的 Prom API
- 需要更底层参数或精确 action
- 需要按组探索未知接口
