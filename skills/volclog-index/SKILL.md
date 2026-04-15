---
name: volclog-index
description: Use when reading or changing TLS index configuration with volclog, or when handling index public API tasks such as index config, rebuild tasks, logid search, or logid distribution, including Chinese intents such as 查看索引, 重建索引, LogId 分布 and English intents such as get index, update index, index rebuild, or logid search.
---

# volclog Index

## Overview

索引 skill 只负责 index shortcut 的第一跳。公开 shortcut 之外的 config / rebuild / logid 相关 API-only 内容，拆到更小的 reference；shortcut 外的能力再交给 api-explorer。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Shortcut First

- get: 优先看 [references/index-get.md](references/index-get.md)
- create / modify: 优先看 [references/index-write.md](references/index-write.md)
- delete: 先看 [references/index-delete.md](references/index-delete.md)

## API Only

- config 视图：看 [references/index-config.md](references/index-config.md)
- rebuild 任务：看 [references/index-rebuild.md](references/index-rebuild.md)
- logid / 分布：看 [references/index-logid.md](references/index-logid.md)
- 总索引：看 [references/index-api-only.md](references/index-api-only.md)

## Fallback

- shortcut 不够时，先切到 [`../volclog-api-explorer/SKILL.md`](../volclog-api-explorer/SKILL.md)
- 如果只是想改字段解析，不要先跳到别的 group

## Core Rules

- `TopicId` 永远走外层 flag，不要塞回 body
- 写入前先看 `--describe`
- 字段多时优先模板文件，不要直接硬拼 inline JSON
- `KeyValue` 走当前 CLI 的数组结构，别把旧文档的 `Separator/Quote/Keys` 结构搬回来
- 需要更细字段解释时，再看 reference；不要先退到 `api call`

## References

- [references/index-get.md](references/index-get.md) - 读取索引和 `TopicId` 外层 flag 的约束
- [references/index-write.md](references/index-write.md) - create/modify 的模板、`KeyValue` 结构和 `SqlFlag`
- [references/index-delete.md](references/index-delete.md) - 删除时为什么要走 api fallback
- [references/index-config.md](references/index-config.md) - `DescribeIndexConfig` / `CreateIndexConfig` / `ModifyIndexConfig` / `DeleteIndexConfig`
- [references/index-rebuild.md](references/index-rebuild.md) - 重建任务三连
- [references/index-logid.md](references/index-logid.md) - `SearchLogId` / `DescribeLogIdDistribution`
- [references/index-api-only.md](references/index-api-only.md) - 上面三类 API-only 动作的总索引
