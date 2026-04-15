---
name: volclog-shipper
description: Use when a user explicitly asks for shipper, delivery config, or retry task operations and the public CLI should fall back to API-level exploration.
---

# volclog Shipper

## Overview

`shipper` 是 public v1 的 API-only group，不保留 shortcut-style 手册。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## 路由

- 用户说“投递配置 / delivery config / shipper config”：
  先看 `volclog capabilities --group shipper --view text`
  再用 `volclog api shipper <Action> --describe`
- 用户说“重试投递任务 / retry delivery task”：
  同样先锁定 `shipper` group，再用 `api shipper <Action> --describe`
- 需要 method/path 或更底层调用时，再进入 `volclog-api-explorer`

## 关键词

- 投递配置
- delivery config
- shipper config
- 重试任务
- retry delivery task

## API 入口

- `volclog capabilities --group shipper --view text`
- `volclog api shipper <Action> --describe`
- body 复杂时，再看 `--print-request-template=required|full`
- 最后再 `--dry-run`

## 不要这样做

- 不要把 `shipper` 当 shortcut 手册来用
- 不要把投递配置和采集配置混成一个 group
- 不要在不知道 `ShipperType` 时直接组织 body
