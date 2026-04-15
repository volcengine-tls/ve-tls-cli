---
name: volclog-topic
description: Use when creating, listing, reading, updating, or deleting TLS log topics with volclog, or when handling topic-specific public API tasks such as topics with index or server timezone, including Chinese intents such as 创建主题, 主题和索引联查, 服务端时区 and English intents such as create topic, list topics, topics with index, or server timezone.
---

# volclog Topic

## Overview

主题管理按 `shortcut -> reference -> api-explorer` 走。先读 shared，再按具体命令读 reference。`topic` 只覆盖高频 CRUD；官网文档之外的字段、低频官方 action 或更原始的接口探索，转 `volclog-api-explorer`。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Shortcut First

- list：看 [references/topic-list.md](references/topic-list.md)
- get：看 [references/topic-get.md](references/topic-get.md)
- create：看 [references/topic-create.md](references/topic-create.md)
- modify：看 [references/topic-modify.md](references/topic-modify.md)
- delete：看 [references/topic-delete.md](references/topic-delete.md)

## API Only

- 主题与索引联查、或用户明确点名 `DescribeTopicsWithIndex`：看 [references/topic-api-only.md](references/topic-api-only.md)

## References

- 列表、过滤、分页和 `--all`：看 [references/topic-list.md](references/topic-list.md)
- 单个主题详情、`--output-mode file` 和 `TopicId` 约束：看 [references/topic-get.md](references/topic-get.md)
- 创建主题的必填字段、TTL 联动和模板切换：看 [references/topic-create.md](references/topic-create.md)
- 修改主题的增量更新、联动约束和模板切换：看 [references/topic-modify.md](references/topic-modify.md)
- 删除主题前的确认流程和 `TopicId` 约束：看 [references/topic-delete.md](references/topic-delete.md)
- 主题和索引联查、以及 shortcut 不覆盖的 topic 官方动作：看 [references/topic-api-only.md](references/topic-api-only.md)

## Fallback

- shortcut 字段不够表达需求，或用户明确要官网接口全集时，转 `volclog-api-explorer`
- 只有在 group/action 已明确且 shortcut 不覆盖时，才看 `api topic <Action> --describe`
