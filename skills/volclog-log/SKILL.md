---
name: volclog-log
description: Use when querying, searching, exporting, or analyzing logs with volclog, including Chinese intents such as 检索日志 or 导出日志 and English intents such as search logs, export logs, query logs, or analysis query.
---

# volclog Log

## Overview

这个 skill 负责把“查日志、导出日志、分析日志”三类诉求稳定路由到 `log search`、`log export`、`log export-analysis`。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)；如用户在排查环境问题，先跑 `doctor`。

## Agent 快速执行顺序

1. 先判断是普通检索、原始导出，还是分析结果导出
2. 普通检索先 `log search`
3. 结果很大再切 `--output-mode file log export`
4. 出现 `select/聚合/统计` 再切 `log export-analysis`

## Agent 禁止行为

- 不要把所有日志查询都升级成导出
- 不要把分析语句交给 `log export`
- 不要在大结果场景里继续把完整 JSON 打到 stdout

## References

- 需要判断 search / export / export-analysis 的分流: 看 [references/search-routing.md](references/search-routing.md)
- 需要导出、文件输出、分析结果落盘模式: 看 [references/export-patterns.md](references/export-patterns.md)
- 需要第一次就用对的查法、输出模式和过滤方式: 看 [references/log-playbook.md](references/log-playbook.md)
- 需要快速定位高频坑、参数边界和空结果排查: 看 [references/error-quick-reference.md](references/error-quick-reference.md)

## Default Recipes

- 先看普通检索:
  `volclog log search --describe`
- 大结果原始日志:
  `volclog --output-mode file log export --describe`
- 分析结果导出:
  `volclog --output-mode file log export-analysis --describe`

## 场景路由

- 用户说“查有没有日志 / search logs / 看结果路径对不对”：
  先用 `volclog log search --describe`
- 用户说“导出很多原始日志 / 全量拉日志 / 落文件”：
  先用 `volclog --output-mode file log export --describe`
- 用户说“SQL / 聚合 / count / group by / analysis result”：
  先用 `volclog --output-mode file log export-analysis --describe`
- 用户说“写日志 / put logs / web tracking”：
  先用 `volclog api log PutLogs --describe` 或 `volclog api log WebTracks --describe`

## Core Rules

- 先跑 `--describe`，再决定是否打印模板。
- 大结果优先 `--output-mode file`。
- 如果要流式导出，优先 `--output jsonl log export`。
- `log export` 只适合纯检索，不适合分析语句。
- `log export-analysis` 只适合分析查询，结果是行对象，不是原始日志。
- analysis 字段可用性依赖当前索引配置；如果字段没开 `SqlFlag`，分析结果里对应列可能为空
- 新增索引字段通常偏增量生效；旧日志即使查询成功，对应列也可能仍是 `null`

## 日志写入说明

`log` 组不只有检索，也包含写入类 API。用户说“写日志 / put logs / web tracking / 前端埋点”时，不要继续留在 `log search`。

- 服务端或离线批量写入：
  `volclog api log PutLogs --describe`
- 前端埋点或 WebTracking：
  `volclog api log WebTracks --describe`
- 写入前先拿模板：
  `volclog api log PutLogs --print-request-template=full`

写入场景的关键提醒：

- 写入通常直接走 `api log <Action>`，当前没有对应 shortcut
- `PutLogs` 的请求体结构和普通查询完全不同，先看模板，不要凭记忆硬写
- 如果用户只是“验证能不能写进去”，先构造最小样例，再决定是否批量化

## Query Routing

- `Query` 不带分析段：优先 `log search` 或 `log export`
- `Query` 中带 `|select`, `|with`, `|insert`：优先 `log export-analysis`

## Log 心智模型

- `log search` 适合先确认路径对不对、有没有数据
- `log export` 适合大量原始日志
- `log export-analysis` 适合结果集型分析，不是原始日志流

## Common Mistakes

- 不要把分析查询交给 `log export`
- 不要对分析查询使用普通检索分页心智
- 不要忘记先生成模板：`volclog log search --print-request-template=full`
- 普通日志检索不要先跑 `capabilities`
- analysis 查询列为 `null` 时，不要先怀疑 `export-analysis`；先检查索引字段和 `SqlFlag`

## 参数边界

- 普通检索优先 `log search`；“很多原始日志”才升级到 `log export`
- `log export-analysis` 只适合结果集型分析，不适合拿原始日志行
- 大结果默认 `--output-mode file`

## 未命中时下一步

- shortcut 不够时：
  `volclog capabilities --group log --view text`
- 锁定 action 后：
  `volclog api log <Action> --describe`
- 只在 method/path 已完全明确时才退到 `api call`
