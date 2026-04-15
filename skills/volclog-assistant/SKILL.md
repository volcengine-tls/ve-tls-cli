---
name: volclog-assistant
description: Use when a user explicitly asks about volclog assistant or session-answer commands and you must determine whether the current build exposes assistant-related abilities without proactively surfacing hidden assistant entrypoints.
---

# volclog Assistant

## Overview

这个 skill 只处理一种特殊情况：用户明确点名 `assistant` / `session answer` 能力时，帮 agent 先判断当前环境要不要继续走隐藏的 assistant 命令。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先确认用户是不是明确点名了 `assistant` / `session answer`
2. 如果只是泛泛说“帮我总结日志 / 帮我排障”，不要主动切到 `assistant`
3. 只有用户明确要求 assistant 命令，或当前环境已明确暴露相关能力时，才继续

## Agent 禁止行为

- 不要把普通日志分析、排障建议主动误路由到 `assistant`
- 不要在对外版里主动暴露隐藏的 assistant 入口
- 不要跳过 `--describe` 猜参数

## Shortcut First

- 仅当用户明确点名 assistant 命令时：`volclog assistant describe-session-answer --describe`

## Default Recipes

- 先确认当前环境是否暴露 assistant：
  `volclog capabilities --view groups`
- 如果用户明确点名 assistant 命令，再看：
  `volclog assistant describe-session-answer --describe`

## 场景路由

- 用户说“AI 助手回答 / session answer / answer detail / 智能问答结果”：
  先确认这是不是显式指定的 assistant CLI 需求；如果不是，不要主动切到 assistant
- 用户说“实例管理 / 创建实例 / 配置助手实例”：
  先确认当前环境是否真的暴露 assistant group，再决定是否升级

## References

- 需要理解当前 shortcut 覆盖范围和何时升级: 看 [references/assistant-routing.md](references/assistant-routing.md)

## Core Rules

- 默认不要主动推荐 assistant；只在用户明确点名 assistant 命令时继续
- 继续之前，先确认当前环境是否暴露 assistant 相关能力
- 如果用户说的是 AI 助手结果定位、会话回答详情，而且当前环境确实支持，再看 `--describe`
- 实例管理或底层接口再升级；否则不要凭空猜 action

## 最小闭环

```bash
volclog capabilities --view groups
volclog assistant describe-session-answer --describe
volclog capabilities --group assistant --view text
volclog api assistant <Action> --describe
```

风险点：

- 常见查询只需要 `topic-id + question`，不要先跳实例管理
- 问题较长时优先 `file://` 输入，避免 shell quoting 干扰

## Assistant 心智模型

- 当前对外版不会主动公开 `assistant` 入口；它更像保留能力
- 只有用户显式点名时，才继续看 `assistant` 命令或底层接口

## Common Mistakes

- 不要默认所有 assistant 需求都能用一个 shortcut 解决
- 不要在没确认当前环境支持时就给出 assistant 命令
- 不要跳过 `--describe` 就猜参数

## 未命中时下一步

- 先确认 group 是否存在：
  `volclog capabilities --view groups`
- 确认存在后再：
  `volclog capabilities --group assistant --view text`
- 锁定 action 后：
  `volclog api assistant <Action> --describe`
- 不要直接退化成 `api call`
