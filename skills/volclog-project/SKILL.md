---
name: volclog-project
description: Use when creating, listing, reading, updating, or deleting TLS projects with volclog, including Chinese intents such as 创建项目 or 查看项目 and English intents such as create project, list projects, update project, or delete project.
---

# volclog Project

## Overview

这个 skill 负责项目管理场景，优先走 `project` shortcut，并把“列举、查看、创建、修改、删除”统一成稳定流程。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先列项目并提取 `ProjectId/ProjectName`
2. 如果要写入，再看 `create/modify --describe`
3. 删除前先确认目标 ID
4. 只有 shortcut 不覆盖时才升级到 explorer

## Agent 禁止行为

- 不要直接用项目名当长期稳定标识
- 不要在不知道字段约束时硬拼 body
- 不要把普通项目场景升级成 `api call`

## Shortcut First

- 列表: `volclog project list --describe`
- 查看: `volclog project get --describe`
- 创建: `volclog project create --describe`
- 修改: `volclog project modify --describe`

## 场景路由

- 用户说“看看有哪些项目 / list projects / 项目清单”：
  先用 `volclog project list --describe`；如果目标是“把项目都看全”，继续用 `volclog project list --all`
- 用户说“拿项目 ID / 找项目 / 模糊搜项目”：
  先用 `volclog project list --fuzzy-search-key <keyword>`
- 用户说“看某个项目详情”：
  先用 `volclog project get --describe`
- 用户说“创建项目 / 修改项目”：
  先用 `volclog project create --describe` 或 `volclog project modify --describe`
- 用户说“删除项目”：
  先确认 `ProjectId`，再用 `volclog project delete --describe`

## Default Recipes

- 先拿项目 ID:
  `volclog project list --all --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"`
- 只想拿第一个项目 ID:
  `volclog project list --jmes-filter "Projects[0].ProjectId"`
- 模糊找项目:
  `volclog project list --fuzzy-search-key <keyword>`
- 看单个项目详情并直接落文件：
  `volclog --output-mode file --output-file ./project-detail.json project get --project-id <ProjectId>`
- 创建前先看约束:
  `volclog project create --describe`

## References

- 需要完整项目写入与读取流转: 看 [references/project-workflow.md](references/project-workflow.md)
- 需要更少试错的固定读取/创建配方: 看 [references/project-playbook.md](references/project-playbook.md)

## Core Rules

- 先用 `list`/`get` 了解资源，再执行创建或修改
- 写入前先看 `--describe`，需要 body 时再生成模板
- shortcut 不覆盖时再切到 `volclog-api-explorer`
- 普通项目列举和拿 ID 时，不要先跑 `capabilities`
- 如果是复数列举场景，优先补上 `--all`，避免分页漏资源让后续判断失真
- 如果是详情查看，优先考虑 `--output-mode file` / `--output-file`，避免长 JSON 淹没上下文

## 未命中时下一步

- shortcut 不够时：
  `volclog capabilities --group project --view text`
- 锁定 action 后：
  `volclog api project <Action> --describe`
- 不要跳到 `topic` 或 `log` 再兜回来

## Project 心智模型

- `ProjectId` 是后续 topic、metric-topic 等命令更稳定的输入
- 列表场景默认先把结果裁成 `ProjectId/ProjectName`
- 普通项目管理优先留在 shortcut 层

## 模糊搜索与分页

- 已知部分名称但不确定全名时，优先 `--fuzzy-search-key`
- 需要精确匹配名称时，再用 `--project-name`
- 需要全量结果时，优先 `volclog project list --all`
- 如果用户自己控制翻页，再用 `--page-number` + `--page-size`
- `--all` 不要和 `--page-number` 混用

## 参数一致性提醒

- 同一个 group 下不同 action 的必填参数不继承
- `list` 偏查询条件，例如 `--project-name`、`--fuzzy-search-key`
- `get/modify/delete` 更偏稳定 ID，例如 `--project-id`
- 不要把一个命令记住的 flags 直接迁移到另一个命令；先看 `--describe`

常见分页配方：

```bash
volclog project list --page-size 20
volclog project list --fuzzy-search-key prod --all
volclog project list --project-name demo --jmes-filter "Projects[].ProjectId"
```

## Common Mistakes

- 不要把项目级写入直接退化成裸 `api call`
- 不要在不知道字段约束时直接硬拼 body
- 删除前先确认资源标识，避免名称和 ID 混淆
