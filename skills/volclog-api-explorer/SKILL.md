---
name: volclog-api-explorer
description: Use when public volclog shortcuts do not cover the task, when a user asks for a specific published OpenAPI action, when a group is known but the shortcut is insufficient, or when an agent must fall back from shortcut flows to capabilities and api describe/template execution.
---

# volclog API Explorer

## Overview

这个 skill 是 fallback，不是默认入口。

它只处理两类事情：

- 公开 shortcut 不覆盖，但公开 API 已发布
- 用户明确指定某个 `group/action`

如果公开 shortcut 已经够用，不要进入这里重新探索。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)，并确认对应 domain skill 不能直接完成目标。

## 适用边界

优先进入这里的情况：

- shortcut 没有对应命令
- shortcut 的 `--flag` 不够表达输入
- 用户明确说出某个 OpenAPI action
- 这个 group 本身就是 API-only，不打算维护 shortcut-style 手册
- 需要官方文档接口的原始 `api <group> <action>` 调用

不应进入这里的情况：

- 普通列资源、拿 ID、常见创建/修改/删除
- 普通日志检索、导出、写入
- 已有公开 shortcut 覆盖的高频操作

## 固定流程

1. 如果 group 已知，先看：
   `volclog capabilities --group <group> --view text`
2. 锁定 action 后看：
   `volclog api <group> <Action> --describe`
3. 如果该 action 有 body，再打印模板：
   `volclog api <group> <Action> --print-request-template=required`
4. body 较复杂时再切：
   `--print-request-template=full`
5. 最后执行

## API 与 Shortcut 的分工

- shortcut 负责高频路径，优先展示 `--flag`
- `api <group> <action>` 负责公开接口全集，body 按 JSON 处理
- `api call` 只保留为最后兜底，不是常规探索入口

## 什么时候用 `api call`

只有在以下条件同时满足时才用：

- method/path 已经明确
- `api <group> <action>` 不能满足当前调用
- 用户明确接受走更原始的调用方式

否则优先：

`capabilities -> api <group> <Action> --describe`

## 何时读哪个 Reference

- 需要完整探索顺序：看 [references/explore-flow.md](references/explore-flow.md)
- 需要判断 `--all`、分页、复数 Describe 接口：看 [references/pagination-and-all.md](references/pagination-and-all.md)
- shortcut 没命中后，想知道应该留在哪个 group：看 [references/group-handoff.md](references/group-handoff.md)

## 规则

- 已知 group 时，不要先跑 `capabilities --view groups`
- 已知 action 时，不要先跑全局探索
- 对 body 字段不要猜，先看 `--describe` 再看模板
- 复数 `Describe...s` 接口优先考虑 `--all`
- 结果很大时优先 `--output-mode file`
- 这个 skill 只承接公开发布接口；不把未公开接口作为默认目标

## Common Mistakes

- 明明 shortcut 已覆盖，却仍从 explorer 开始
- 跳过 `api --describe` 直接拼 body
- 不区分 `required` 模板和 `full` 模板
- 已知 group 后仍先跑全局 groups
- 明明可以用 `api <group> <action>`，却直接退到 `api call`
