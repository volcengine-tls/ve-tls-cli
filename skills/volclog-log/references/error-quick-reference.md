# Log Error Quick Reference

这个 reference 用于日志查询第一次失败后的收敛，不用于未知 API 探索。

## 空结果 / 查不到日志

先排查：

1. `volclog doctor`
2. 确认 `TopicId`
3. 用最小查询验证路径：

```bash
volclog log search --topic-id <TopicId> --query "*" --from "2026-03-14 00:00:00" --to "2026-03-14 01:00:00"
```

不要一上来就怀疑 `log export-analysis`。

如果只是想验证“路径对不对”，先坚持最小 `log search`，不要直接导出。

## 分析查询结果不在 `Logs`

症状：

- 用户用了 `select`
- 结果主要在分析结果而不是原始日志

做法：

- 改走 `volclog --output-mode file log export-analysis --describe`
- 不要继续用 `log export`

## 分析结果列为 `null`

症状：

- 查询执行成功
- `AnalysisResult` 里有列名，但值大量为 `null`

做法：

- 先检查相关索引字段是否开启 `SqlFlag`
- 再确认这些索引字段是不是新增后才开始生效
- 不要先怀疑 `log export-analysis` 路由选错

## 命令选错

症状：

- 普通检索却跑了 `log export`
- 分析语句却跑了 `log export`
- 写日志却还留在 `log search`

做法：

- 普通检索：`volclog log search --describe`
- 原始导出：`volclog --output-mode file log export --describe`
- 分析导出：`volclog --output-mode file log export-analysis --describe`
- 写日志：`volclog api log PutLogs --describe`

## stdout 结果过大

做法：

```bash
volclog --output-mode file log export ...
volclog --output-mode file log export-analysis ...
```

## 高频坑

- `log export` 不是分析导出
- 查询路径没确认前，不要直接上导出
- 结果太大先落盘，不要继续在 stdout 里反复试
- analysis 字段是否可用，先看索引和 `SqlFlag`
