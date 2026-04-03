---
name: volclog-shipper
description: Use when operating TLS shippers with volclog, including Chinese intents such as 投递、投递配置 or 重试投递任务 and English intents such as shipper, delivery, ship logs, or retry shipper task.
---

# volclog Shipper

## Overview

这个 skill 负责 `shipper` 组。它适合投递配置的创建、修改、删除、列举，以及投递任务重试。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先确认需求是“投递配置管理”还是“投递任务处理”
2. 先看 `volclog capabilities --group shipper --view text`
3. 再选最接近的 action，执行 `volclog api shipper <Action> --describe`
4. 写入型动作先打印模板，再 `--dry-run`

## Agent 禁止行为

- 不要把投递需求误路由到 `collector`
- 不要在不知道 `ShipperType` 时直接猜 body
- 不要跳过 `--describe` 就手写大请求体

## Default Recipes

- 看投递配置能力：
  `volclog capabilities --group shipper --view text`
- 创建投递配置：
  `volclog api shipper CreateShipper --describe`
- 列举投递配置：
  `volclog api shipper DescribeShippers --describe`
- 修改或删除投递配置：
  `volclog api shipper ModifyShipper --describe`
  `volclog api shipper DeleteShipper --describe`
- 重试投递任务：
  `volclog api shipper RetryShipperTask --describe`

## 场景路由

- 用户说“投递配置 / delivery config / shipper config”：
  先用 `volclog api shipper DescribeShippers --describe`
- 用户说“创建投递 / 修改投递类型 / 改投递目标”：
  先用 `volclog api shipper CreateShipper --describe` 或 `ModifyShipper --describe`
- 用户说“重试任务 / retry delivery task”：
  先用 `volclog api shipper RetryShipperTask --describe`

## Core Rules

- `shipper` 关注的是投递配置和投递任务，不是采集源配置
- 写入型动作优先模板驱动，不要凭字段名记忆硬写
- 如果结果较大或需要复盘，配合 `--trace-dir` 或 `--output-mode file`

## 最小闭环

```bash
volclog api shipper DescribeShippers --describe
volclog api shipper CreateShipper --describe
volclog api shipper CreateShipper --print-request-template=full
volclog --dry-run api shipper CreateShipper --request file://req.json
volclog api shipper ModifyShipper --describe
volclog api shipper DeleteShipper --describe
volclog api shipper RetryShipperTask --describe
```

风险点：

- `ShipperType` 和目标配置强相关，先模板再填真实目标
- 重试任务和修改配置是两条路径，不要混用

## Shipper 心智模型

- `CreateShipper / ModifyShipper / DeleteShipper` 管配置本身
- `DescribeShippers` 看配置列表
- `RetryShipperTask` 更偏任务级操作，不是配置修改

## References

- 常见 action 分流：看 [references/action-playbook.md](references/action-playbook.md)

## Common Mistakes

- 把投递配置问题误当成采集配置问题
- 没确认 `ShipperType` 就开始组织 body
- 任务重试和配置修改混成同一路径

## 未命中时下一步

- 先留在 `shipper`：
  `volclog capabilities --group shipper --view text`
- 再锁定 action：
  `volclog api shipper <Action> --describe`
- 不要跳到 `collector`
