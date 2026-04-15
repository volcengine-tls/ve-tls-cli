---
name: volclog-alarm
description: Use when a user explicitly asks for alarm, notify group, webhook, or alarm content template operations and the public CLI should fall back to API-level exploration.
---

# volclog Alarm

## Overview

`alarm` 是 public v1 的 API-only group，不保留 shortcut-style 手册。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## 路由

- 用户说“告警策略 / alert policy / 告警规则”：
  先看 `volclog capabilities --group alarm --view text`
  再用 `volclog api alarm <Action> --describe`
- 用户说“通知组 / 通知模板 / Webhook / 飞书 / 钉钉”：
  同样先锁定 `alarm` group，再用 `api alarm <Action> --describe`
- 需要 method/path 或更底层调用时，再进入 `volclog-api-explorer`

## 关键词

- 告警策略
- 通知组
- 通知模板
- Webhook 集成
- 飞书 / 钉钉 / 企业微信

## API 入口

- `volclog capabilities --group alarm --view text`
- `volclog api alarm <Action> --describe`
- body 复杂时，再看 `--print-request-template=required|full`
- 最后再 `--dry-run`

## 不要这样做

- 不要把 `alarm` 当 shortcut 手册来用
- 不要凭记忆组织 `Condition` / `TriggerConditions` / `Severity`
- 不要把通知组、Webhook、内容模板和策略本体混成一个对象
