---
name: volclog-host-group
description: Use when operating TLS host groups with volclog, including Chinese intents such as 机器组、主机纳管、绑定采集规则、异常主机清理、agent 运维策略 or 自动更新 and English intents such as host group, machine group, bind collectors, abnormal hosts, ops policy, or host auto update.
---

# volclog Host Group

## Overview

这个 skill 只负责 `host-group` 组的入口路由，不重复展开完整参数表。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## 使用顺序

1. 先用 `host-group` shortcut
2. shortcut 不够时，先读对应 `reference`
3. `reference` 仍不够时，再交给 `api-explorer`
4. 只有 method/path 已经明确时，才退到 `api call`

## Shortcut First

- 基础机器组管理优先走 `host-group list/get/create/modify/delete`
- `list/get` 先看列表/详情 reference
- `create/modify/delete` 先看写入 reference
- 主机成员、绑定、自动更新先看关系 reference

## References

- 列表/详情：看 [references/host-group-list-get.md](references/host-group-list-get.md)
- 创建/修改/删除：看 [references/host-group-write.md](references/host-group-write.md)
- 主机成员/绑定/自动更新：看 [references/host-group-relations.md](references/host-group-relations.md)

## 何时切到 `api-explorer`

- shortcut/reference 没覆盖的字段，先读对应 reference，再决定要不要切 `api-explorer`
- 用户明确要官网文档里没有展开的 action，或要更底层的 method/path 时，再去 `api-explorer`
- 不要一上来就跑 `capabilities`
