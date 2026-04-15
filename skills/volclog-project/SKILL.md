---
name: volclog-project
description: Use when creating, listing, reading, updating, or deleting TLS projects with volclog, including Chinese intents such as 创建项目 or 查看项目 and English intents such as create project, list projects, update project, or delete project.
---

# volclog Project

## Overview

这个 skill 只负责 project shortcut 的第一跳。字段约束、参数联动、常见误用放到 reference，shortcut 以外的能力再交给 api-explorer。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Shortcut First

- list / get: 优先看 [references/project-list-get.md](references/project-list-get.md)
- create / modify: 优先看 [references/project-write.md](references/project-write.md)
- delete: 优先看 [references/project-delete.md](references/project-delete.md)

## Fallback

- shortcut 不够时，先切到 [`../volclog-api-explorer/SKILL.md`](../volclog-api-explorer/SKILL.md)
- 如果用户只说“查项目”或“改项目”，先留在 project 组，不要先换到别的 group

## Core Rules

- 列表类需求优先 `--all`，避免分页漏资源
- 详情优先 `--output-mode file`，别让长 JSON 占满上下文
- 名字能过滤就用名字，真正稳定的标识还是 `ProjectId`
- 不要把 list 的 flags 直接迁移到 create / modify
- 不要先跳到 `api call`；先看 shortcut，再看 reference，再 fallback

## Quick Starts

- `volclog project list --describe`
- `volclog project list --all`
- `volclog project get --describe`
- `volclog project create --describe`
- `volclog project modify --describe`
- `volclog project delete --describe`

## Common Mistakes

- 不要把项目名当长期稳定 ID
- 不要把 list 里看到的可选参数当成 create / modify 的必填项
- 不要直接硬拼 body；写入前先看 `--describe`
- 不要在删除前猜 `ProjectId`
- 需要更细的参数边界时，再切到 `api-explorer`

## References

- [references/project-list-get.md](references/project-list-get.md) - list/get 的常用约束、`--all`、详情落盘
- [references/project-write.md](references/project-write.md) - create/modify 的字段联动和模板用法
- [references/project-delete.md](references/project-delete.md) - delete 的 ID 确认和安全边界
