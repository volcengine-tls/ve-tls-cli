---
name: volclog-log
description: Use when querying, writing, exporting, consuming, or analyzing logs with volclog, or when handling log public API tasks such as cursor time, log context, histogram, download tasks, saved searches, favourites, web tracking, session answers, or assistant attachments, including Chinese intents such as 检索日志, 游标时间, 日志上下文, 收藏查询, 会话答案 and English intents such as search logs, cursor time, log context, saved search, web tracking, or session answer.
---

# volclog Log

## Overview

日志场景按 `shortcut -> reference -> api-explorer` 走。先读 shared，再按查询、写入、导出、消费拆 reference。`log` 只覆盖高频链路；官网文档之外的接口或低频原始能力，转 `volclog-api-explorer`。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)；如用户在排查环境问题，先跑 `doctor`。

## Shortcut First

- search：看 [references/log-search.md](references/log-search.md)
- ingest：看 [references/log-ingest.md](references/log-ingest.md)
- put：看 [references/log-put.md](references/log-put.md)
- export：看 [references/log-export.md](references/log-export.md)
- export-analysis：看 [references/log-export-analysis.md](references/log-export-analysis.md)
- consume：看 [references/log-consume.md](references/log-consume.md)

## API Only

- 分析任务 / 字段快速分析：看 [references/log-analysis-jobs.md](references/log-analysis-jobs.md)
- 下载任务：看 [references/log-download.md](references/log-download.md)
- 保存检索 / 收藏：看 [references/log-saved-search.md](references/log-saved-search.md)
- 归档检索：看 [references/log-archive-search.md](references/log-archive-search.md)
- WebTracking / Kafka：看 [references/log-webtracking-kafka.md](references/log-webtracking-kafka.md)
- 会话答案 / 附件 / LogApp：看 [references/log-session-logapp.md](references/log-session-logapp.md)
- 其他零散 log API-only 动作：看 [references/log-api-only.md](references/log-api-only.md)

## References

- 普通检索、分析检索、分页和输出模式：看 [references/log-search.md](references/log-search.md)
- 批量导入文本/JSON、默认时间、批次和统计头：看 [references/log-ingest.md](references/log-ingest.md)
- 原始 PutLogs 请求、压缩和精确控制：看 [references/log-put.md](references/log-put.md)
- 原始导出与分析导出的分流：看 [references/log-export.md](references/log-export.md)
- SQL / 聚合 / 行结果导出：看 [references/log-export-analysis.md](references/log-export-analysis.md)
- shard / cursor / 原始消费链路：看 [references/log-consume.md](references/log-consume.md)
- 分析任务 / 字段快速分析：看 [references/log-analysis-jobs.md](references/log-analysis-jobs.md)
- 下载任务：看 [references/log-download.md](references/log-download.md)
- 保存检索 / 收藏：看 [references/log-saved-search.md](references/log-saved-search.md)
- 归档检索：看 [references/log-archive-search.md](references/log-archive-search.md)
- WebTracking / Kafka：看 [references/log-webtracking-kafka.md](references/log-webtracking-kafka.md)
- 会话答案 / 附件 / LogApp：看 [references/log-session-logapp.md](references/log-session-logapp.md)
- 其他零散 log API-only 动作：看 [references/log-api-only.md](references/log-api-only.md)

## Fallback

- shortcut 不够表达需求，或用户明确要官网接口全集时，转 `volclog-api-explorer`
- 只有在 group/action 已明确且 shortcut 不覆盖时，才看 `api log <Action> --describe`
