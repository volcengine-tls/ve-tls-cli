---
name: volclog-assistant
description: Use when operating TLS assistant flows with volclog, including Chinese intents such as AI 助手 or 会话回答 and English intents such as assistant, session answer, or answer detail.
---

# volclog Assistant

## Overview

这个 skill 负责 `assistant` 相关场景。当前优先入口是 `describe-session-answer`，如果不够，再升级到 explorer。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先确认是不是在查 session answer / answer detail
2. 先跑 `describe-session-answer --describe`
3. 只有实例管理或底层接口需求才升级 explorer

## Agent 禁止行为

- 不要默认 assistant 需求都要走 explorer
- 不要跳过 `--describe` 猜参数

## Shortcut First

- `volclog assistant describe-session-answer --describe`

## Default Recipes

- 看会话回答详情:
  `volclog assistant describe-session-answer --describe`
- 实际执行最小查询:
  `volclog assistant describe-session-answer --topic-id <TopicId> --question 'What happened?'`
- 先确认输入约束，再组织参数，不要直接猜实例字段

## 场景路由

- 用户说“AI 助手回答 / session answer / answer detail / 智能问答结果”：
  先用 `volclog assistant describe-session-answer --describe`
- 用户说“实例管理 / 创建实例 / 配置助手实例”：
  shortcut 可能不够，准备升级到 `capabilities --group assistant --view text`

## References

- 需要理解当前 shortcut 覆盖范围和何时升级: 看 [references/assistant-routing.md](references/assistant-routing.md)

## Core Rules

- 先看 `--describe`，确认当前 shortcut 需要哪些输入
- shortcut 只覆盖已封装的高频入口，实例管理或底层接口再升级
- 如果用户说的是 AI 助手结果定位、会话回答详情，优先当前 shortcut
- 不要为常见 answer 查询先跑 explorer

## 最小闭环

```bash
volclog assistant describe-session-answer --describe
volclog assistant describe-session-answer --topic-id <TopicId> --question 'What happened?'
volclog capabilities --group assistant --view text
volclog api assistant <Action> --describe
```

风险点：

- 常见查询只需要 `topic-id + question`，不要先跳实例管理
- 问题较长时优先 `file://` 输入，避免 shell quoting 干扰

## Assistant 心智模型

- 当前 shortcut 更偏“回答详情读取”，不是完整 assistant 管理面
- 常见查询先留在 shortcut，未知管理动作再升级

## Common Mistakes

- 不要默认所有 assistant 需求都能用一个 shortcut 解决
- 不要跳过 `--describe` 就猜参数

## 未命中时下一步

- shortcut 不够时：
  `volclog capabilities --group assistant --view text`
- 锁定 action 后：
  `volclog api assistant <Action> --describe`
- 不要直接退化成 `api call`
