---
name: volclog-api-explorer
description: Use when volclog shortcuts do not cover the task, when a user asks for a specific OpenAPI action, when the needed interface is unknown and must be discovered, or when an agent needs to fall back from shortcut flows to capabilities and api describe/template/dry-run execution.
---

# volclog API Explorer

## Overview

当 shortcut 不够用时，用这个 skill 回退到 `capabilities` 和 `api`。目标不是猜接口，而是用 CLI 自带的自解释能力把未知能力探索出来。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)，并确认对应 domain skill 没有覆盖该需求。

进入这个 skill 后，仍然优先复用 CLI 自己给的提示，不要让模型自由发挥。`capabilities`、`api --describe`、shortcut `--describe` 已经能直接给出下一步命令和 fallback 命令。

## Design Goal

- 这是 fallback skill，不是默认入口。
- 只有 domain skill 明确不覆盖，或用户明确点名某个 OpenAPI action 时，才进入这里。
- 能用 `project/topic/index/log/metric-topic/assistant` shortcut 解决的事，不要先来这里重新探索一遍。
- 进入这里之后，也不要重新发明流程；优先照抄 CLI 原生提示字段里的命令。

## 执行前必做

- 先确认对应 domain skill 的默认命令确实不够
- 先确认用户是否明确指定了某个 action
- 如果只是常见列举、创建、检索，不要进入这里
- 如果 shortcut `--describe` 已返回 `fallback_api_describe`，优先直接用它

## Required Workflow

1. 先看组：`volclog capabilities --view groups`
2. 再缩小范围：`volclog capabilities --group <group> --view text`
3. 再看约束：`volclog api <group> <action> --describe`
4. 直接读 `api --describe` 返回的 `guidance`
5. 如果有 body：优先执行 `guidance.template`
6. 执行前校验：优先执行 `guidance.dry_run`
7. 再正式执行 `guidance.execute`

如果 `api --describe` 已经返回：

- `shortcut_first`
- `fallback_discovery`
- `template`
- `dry_run`
- `execute`

就直接使用这些字段，不要再人工拼接等价命令。

## References

- 需要完整探索流程和常见分岔: 看 [references/explore-flow.md](references/explore-flow.md)
- 需要判断 `--all`、分页、Describe 复数接口怎么走: 看 [references/pagination-and-all.md](references/pagination-and-all.md)
- 需要知道“快捷命令没命中后，这个 group 下一步该怎么走”: 看 [references/group-handoff.md](references/group-handoff.md)

## When To Upgrade From Shortcut

- shortcut 没有对应命令
- shortcut 的字段不够表达需求
- 用户明确指定某个 OpenAPI action
- 需要更稳定的机器可读约束
- 需要复数 `Describe...s` 接口的 `--all`

如果 shortcut `--describe` 已经给了 `fallback_discovery` 和 `fallback_api_describe`，这不叫“重新探索”，而是按 CLI 原生 handoff 继续走。

## Do Not Use For

- 列项目、列主题、拿 `ProjectId/TopicId`
- 普通 topic/index 创建或修改
- 普通日志检索、原始日志导出、分析结果导出
- 常见 metric topic 查询
- 常见 assistant answer 查询

这些场景优先回到对应 domain skill 的默认配方。

## Decision Rules

- 如果用户意图已经落在某个已知 group，就优先留在这个 group 里继续探索，不要随意换组
- 先看 `capabilities` 输出里的 `agent entry` / `agent_next_step`
- 优先 `api <group> <action>`，不要默认走 `api call`
- `api --describe` 返回 `shortcut_first` 时，说明该 action 附近已有高频 shortcut；如果用户需求仍是常见路径，先回到这些 shortcut
- `api --describe` 返回 `fallback_discovery` 时，说明下一步 group discovery 已固定，不要再跑全局 groups
- 只有 method/path 已完全明确时，才用 `api call`
- 如果 action 是复数 `Describe...s`，优先尝试 `--all`
- 如果结果很大，优先 `--output-mode file`

## CLI 原生提示怎么用

- `capabilities --view groups/text`
  用来拿 `agent entry`
- `capabilities --group <group> --action <action> --view full`
  用来拿 `agent_entrypoint`、`agent_next_step`、`related_shortcuts`
- `api --describe`
  用来拿 `guidance.template`、`guidance.dry_run`、`guidance.execute`、`guidance.shortcut_first`
- shortcut `--describe`
  用来拿 `guidance.fallback_discovery`、`guidance.fallback_api_describe`

低能力模型的执行原则是：**字段里给了命令，就照着用；不要再翻译成另一套步骤。**

## Common Mistakes

- 明明 shortcut 已覆盖，却仍从 `capabilities` 开始探索
- 不要跳过 `--describe`
- 不要直接手写大 body，先打印模板
- 不要把 `--all` 用在 `api call`
- 不要把 `--jmes-filter` 写成 envelope 路径
- 不要忽略 `guidance.shortcut_first`、`guidance.fallback_discovery`、`guidance.execute`
