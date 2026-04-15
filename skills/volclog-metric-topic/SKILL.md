---
name: volclog-metric-topic
description: Use when creating, listing, reading, updating, deleting, or querying metric topics with volclog, including Chinese intents such as 指标主题, PromQL 查询, label values, series, remote write or query_range and English intents such as metric topic, prometheus, promql, query_range, series, labels, or remote write.
---

# volclog Metric Topic

## Overview

这个 skill 负责 metric-topic 的第一跳：先判断是资源管理还是 PromQL/指标查询，再决定读哪篇 reference。更细的字段联动和常见误用不要塞在 SKILL.md 里。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Command Map

- 资源管理: 先看 [references/metric-topic-resource.md](references/metric-topic-resource.md)
- 查询: 先看 [references/metric-topic-query.md](references/metric-topic-query.md)
- 分流与 fallback: 先看 [references/metric-topic-routing.md](references/metric-topic-routing.md)

## Fallback

- shortcut 不够或用户明确点名底层 Prom API 时，切到 [`../volclog-api-explorer/SKILL.md`](../volclog-api-explorer/SKILL.md)
- 如果是普通资源 CRUD，不要先去 explorer

## Core Rules

- 先判断是资源管理还是指标查询
- 资源管理尽量留在 `list/get/create/modify/delete`
- 查询优先 `metric-topic search`
- PromQL / Prometheus / time series 需求不要误路由到 `log`
- 复数列表优先 `--all`
- 详情优先 `--output-mode file`
- 需要更细的字段约束，先读 reference，不要直接猜 body

## Shortcut First

- `volclog metric-topic list --describe`
- `volclog metric-topic get --describe`
- `volclog metric-topic create --describe`
- `volclog metric-topic modify --describe`
- `volclog metric-topic delete --describe`
- `volclog metric-topic search --describe`

## Quick Starts

- `volclog metric-topic list --project-id <ProjectId> --all`
- `volclog metric-topic get --topic-id <TopicId>`
- `volclog --output-mode file --output-file ./metric-topic-detail.json metric-topic get --topic-id <TopicId>`
- `volclog metric-topic search --describe`

## Common Mistakes

- 不要把 PromQL 查询误路由到 `log`
- 不要把资源管理和查询语义混成一个命令
- 不要把 `Ttl` 当成任意整数试错
- 创建后短时间立刻改/删，先查状态再重试
- 不要把 `create` 需要的 `--project-id` 误迁移到 `delete`
- 不要跳过 `--describe`

## References

- [references/metric-topic-routing.md](references/metric-topic-routing.md) - 先判断资源管理还是查询
- [references/metric-topic-resource.md](references/metric-topic-resource.md) - list/get/create/modify/delete 的约束
- [references/metric-topic-query.md](references/metric-topic-query.md) - PromQL / 指标查询的使用方式
