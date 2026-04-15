---
name: volclog-log
description: Use when querying, writing, exporting, consuming, or analyzing logs with volclog, including Chinese intents such as 检索日志, 写日志, 导出日志, 消费日志, 拉取原始日志, 游标消费, shard 日志 and English intents such as search logs, put logs, ingest logs, export logs, consume logs, pull logs by cursor, or analysis query.
---

# volclog Log

## Overview

这个 skill 负责把“查日志、写日志、导出日志、分析日志、消费日志”五类诉求稳定路由到 `log search`、`log ingest/log put`、`log export`、`log export-analysis`、`api log/shard Consume*`。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)；如用户在排查环境问题，先跑 `doctor`。

## Agent 快速执行顺序

1. 先判断是普通检索、日志写入、原始导出、分析结果导出，还是消费日志
2. 普通检索先 `log search`
3. 结果很大再切 `--output-mode file log export`
4. 出现 `select/聚合/统计` 再切 `log export-analysis`
5. 用户说消费/游标/shard/从头拉原始日志时，先走 `DescribeShards -> DescribeCursor -> Consume*`

## Agent 禁止行为

- 不要把所有日志查询都升级成导出
- 不要把分析语句交给 `log export`
- 不要在大结果场景里继续把完整 JSON 打到 stdout
- 不要把消费日志改写成 `log search` / `log export`
- 不要只消费第一个 shard

## References

- 需要判断 search / export / export-analysis 的分流: 看 [references/search-routing.md](references/search-routing.md)
- 需要导出、文件输出、分析结果落盘模式: 看 [references/export-patterns.md](references/export-patterns.md)
- 需要第一次就用对的查法、输出模式和过滤方式: 看 [references/log-playbook.md](references/log-playbook.md)
- 需要消费日志、cursor、shard 遍历、原始日志包读取: 看 [references/consume-playbook.md](references/consume-playbook.md)
- 需要快速定位高频坑、参数边界和空结果排查: 看 [references/error-quick-reference.md](references/error-quick-reference.md)

## Default Recipes

- 先看普通检索:
  `volclog log search --describe`
- 批量写入文本或 JSON 日志:
  `volclog log ingest --describe`
- 已准备好原始 PutLogs 请求:
  `volclog log put --describe`
- 先消费原始日志:
  `volclog api shard DescribeShards --describe`
- 大结果原始日志:
  `volclog --output-mode file log export --describe`
- 分析结果导出:
  `volclog --output-mode file log export-analysis --describe`

## 场景路由

- 用户说“查有没有日志 / search logs / 看结果路径对不对”：
  先用 `volclog log search --describe`
- 用户说“消费日志 / 按 cursor 拉日志 / 遍历 shard / 从头拉原始日志”：
  先用 `volclog api shard DescribeShards --describe`
- 用户说“导出很多原始日志 / 全量拉日志 / 落文件”：
  先用 `volclog --output-mode file log export --describe`
- 用户说“SQL / 聚合 / count / group by / analysis result”：
  先用 `volclog --output-mode file log export-analysis --describe`
- 用户说“写日志 / put logs / ingest logs”：
  先用 `volclog log ingest --describe`
- 用户说“WebTracking / 前端埋点”：
  先用 `volclog api log WebTracks --describe`

## Core Rules

- 先跑 `--describe`，再决定是否打印模板。
- 大结果优先 `--output-mode file`。
- 如果要流式导出，优先 `--output jsonl log export`。
- 如果是消费日志，先枚举 shard，再逐 shard 获取 cursor 和执行 `Consume*`。
- `log export` 只适合纯检索，不适合分析语句。
- `log export-analysis` 只适合分析查询，结果是行对象，不是原始日志。
- 用户明确说“原始日志 / 原始包 / raw package”时，默认优先 `ConsumeOriginalLogs`。
- `ConsumeOriginalLogs` 更接近写入时的原始 IO 颗粒度，通常比服务端先组装再返回更省服务端处理开销。
- `ConsumeLogs` 返回的是更偏解析后的日志视图，服务端可能会把多个原始 IO 组装到同一批结果里。
- topic 级消费不是单次 API，agent 需要遍历全部 shard。
- 如果用户要保留原始包边界，不要优先 `--output jsonl`；JSONL 会展开成日志记录。
- analysis 字段可用性依赖当前索引配置；如果字段没开 `SqlFlag`，分析结果里对应列可能为空
- 新增索引字段通常偏增量生效；旧日志即使查询成功，对应列也可能仍是 `null`

## 日志写入说明

`log` 组不只有检索，也包含写入类 shortcut 和 API。用户说“写日志 / put logs / ingest logs / web tracking / 前端埋点”时，不要继续留在 `log search`。

- 批量导入文本或 JSON 日志：
  `volclog log ingest --describe`
- 已经准备好 PutLogs 原始请求体：
  `volclog log put --describe`
- 前端埋点或 WebTracking：
  `volclog api log WebTracks --describe`
- 写入前需要看原始模板时：
  `volclog log put --print-request-template=full`

写入场景的关键提醒：

- 文本行或结构化 JSON 批量写入，优先 `log ingest`
- `log ingest` 默认按 500 条一批发送，默认压缩为 `lz4`
- `lines` 输入会把每行文本写入 `__content__`
- `jsonl/json-array` 会保留用户原始字段；未指定 `--time-field` 时会自动补本次命令启动时的毫秒时间戳
- 已经准备好原始 PutLogs 请求体时，再用 `log put`
- `PutLogs` 的请求体结构和普通查询完全不同，先看模板，不要凭记忆硬写
- 如果用户只是“验证能不能写进去”，先构造最小样例，再决定是否批量化

## 日志消费说明

`log` 组不只有检索和写入，也包含消费类 API。用户说“消费日志 / pull logs / 按游标拉取 / 从头消费 / 原始日志包”时，不要继续留在 `log search` 或 `log export`。

- topic 级消费的第一步：
  `volclog api shard DescribeShards --describe`
- 每个 shard 获取 cursor：
  `volclog api log DescribeCursor --describe`
- 消费解析后的日志：
  `volclog api log ConsumeLogs --describe`
- 消费原始日志包：
  `volclog api log ConsumeOriginalLogs --describe`

消费场景的关键提醒：

- 真正拉日志的数据面通常是 `shard + log` 联动，不是只看 `consumer-group`
- 用户说“消费原始日志”时，默认优先 `ConsumeOriginalLogs`
- 用户说“保持与写入时完全一致的 IO / 不要服务端组装 / 更省服务端性能”时，优先 `ConsumeOriginalLogs`
- 对每一个 shard 都要单独拿 cursor，再单独消费
- 如果目标是整条 topic 的数据，agent 必须遍历全部 shard
- 如果目标是保留原始包结构，优先 `--output json`；不要先切 `jsonl`
- 只有在需要 heartbeat / checkpoint / 消费组管理时，才额外进入 `consumer-group`

## Query Routing

- `Query` 不带分析段：优先 `log search` 或 `log export`
- `Query` 中带 `|select`, `|with`, `|insert`：优先 `log export-analysis`
- 如果诉求里出现 `consume/cursor/shard/checkpoint/pull logs`，退出 query 路由，改走消费链路

## Log 心智模型

- `log search` 适合先确认路径对不对、有没有数据
- `log export` 适合大量原始日志
- `log export-analysis` 适合结果集型分析，不是原始日志流
- `Consume*` 适合按 shard、按 cursor 拉取日志数据，不等于 search/export
- `ConsumeOriginalLogs` 偏原始 IO 读取；`ConsumeLogs` 偏解析后日志读取

## Common Mistakes

- 不要把分析查询交给 `log export`
- 不要对分析查询使用普通检索分页心智
- 不要忘记先生成模板：`volclog log search --print-request-template=full`
- 普通日志检索不要先跑 `capabilities`
- 不要把“消费日志”错误改写成“检索日志”或“导出日志”
- 不要只消费第一个 shard；topic 级消费需要遍历全部 shard
- 不要把 `consumer-group` 误当成实际拉日志的数据面接口
- 不要在用户要“原始 IO 包”时改成 `ConsumeLogs`
- 不要在用户要“保留原始包边界”时改成 `--output jsonl`
- analysis 查询列为 `null` 时，不要先怀疑 `export-analysis`；先检查索引字段和 `SqlFlag`

## 参数边界

- 普通检索优先 `log search`；“很多原始日志”才升级到 `log export`
- `log export-analysis` 只适合结果集型分析，不适合拿原始日志行
- `ConsumeLogs` / `ConsumeOriginalLogs` 按 shard 执行；topic 级意图需要 agent 自己遍历全部 shard
- `ConsumeOriginalLogs` 保留原始写入包；`ConsumeLogs` 可能对应服务端组装后的结果视图
- 大结果默认 `--output-mode file`

## 未命中时下一步

- shortcut 不够时：
  `volclog capabilities --group log --view text`
- 锁定 action 后：
  `volclog api log <Action> --describe`
- 只在 method/path 已完全明确时才退到 `api call`
