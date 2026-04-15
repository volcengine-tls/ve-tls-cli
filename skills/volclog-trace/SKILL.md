---
name: volclog-trace
description: Use when a user explicitly asks for trace instance, trace search, or span search operations and the public CLI should fall back to API-level exploration.
---

# volclog Trace

## Overview

`trace` 是 public v1 的 API-only group，不保留 shortcut-style 手册。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## 路由

- 用户说“trace 实例 / trace storage / 实例管理”：
  先看 `volclog capabilities --group trace --view text`
  再用 `volclog api trace <Action> --describe`
- 用户说“search traces / search spans / span 查询”：
  同样先锁定 `trace` group，再用 `api trace <Action> --describe`
- 需要 method/path 或更底层调用时，再进入 `volclog-api-explorer`

## 关键词

- trace 实例
- span 查询
- search traces
- search spans
- 链路追踪
- trace storage

## API 入口

- `volclog capabilities --group trace --view text`
- `volclog api trace <Action> --describe`
- body 复杂时，再看 `--print-request-template=required|full`
- 最后再 `--dry-run`

## 不要这样做

- 不要把 `trace` 当 shortcut 手册来用
- 不要把 trace 查询误路由到普通 `log`
- 不要没拿到 `TraceInstanceId` 就直接查
