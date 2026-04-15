---
name: volclog-collector
description: Use when operating TLS collector rules with volclog, including Chinese intents such as 采集配置、采集规则、绑定机器组、导入采集配置、路径解析、时间解析 or 正则生成 and English intents such as collector, collection rule, bind host group, config import, parse path, parse time, or regex generation.
---

# volclog Collector

## Overview

这个 skill 只负责 `collector` 组的入口路由。`import / parse-helpers / bindings-or-ops` 这些 API-only 内容拆到更小的 reference，shortcut 外的能力再交给 api-explorer。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## 使用顺序

1. 先用 `collector` shortcut
2. shortcut 不够时，先读对应 `reference`
3. `reference` 仍不够时，再交给 `api-explorer`
4. 只有 method/path 已经明确时，才退到 `api call`

## Shortcut First

- 基础采集规则管理优先走 `collector list/get/create/modify/delete`
- `list/get` 先看列表/详情 reference
- `create/modify/delete` 先看写入 reference
- 绑定/解绑/解析辅助先看关系 reference

## References

- 列表/详情：看 [references/collector-list-get.md](references/collector-list-get.md)
- 创建/修改/删除：看 [references/collector-write.md](references/collector-write.md)
- 导入任务：看 [references/collector-import.md](references/collector-import.md)
- 解析辅助：看 [references/collector-parse-helpers.md](references/collector-parse-helpers.md)
- 绑定/运维动作：看 [references/collector-bindings-or-ops.md](references/collector-bindings-or-ops.md)
- 总索引：看 [references/collector-relations.md](references/collector-relations.md)

## 何时切到 `api-explorer`

- shortcut/reference 没覆盖的字段，先读对应 reference，再决定要不要切 `api-explorer`
- 用户明确要官网文档里没有展开的 action，或要更底层的 method/path 时，再去 `api-explorer`
- 不要一上来就跑 `capabilities`
