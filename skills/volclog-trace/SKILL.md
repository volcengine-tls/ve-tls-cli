---
name: volclog-trace
description: Use when operating TLS trace storage and queries with volclog, including Chinese intents such as Trace 实例、Span 查询 or 链路追踪 and English intents such as trace, span, trace instance, search traces, or search spans.
---

# volclog Trace

## Overview

这个 skill 负责 `trace` 组。它适合 Trace 实例管理、Trace/Span 检索，以及链路详情查询。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先判断是在做实例管理，还是 Trace/Span 查询
2. 先看 `volclog capabilities --group trace --view text`
3. 再选 action，执行 `volclog api trace <Action> --describe`
4. 写入型动作先模板、再 `--dry-run`

## Agent 禁止行为

- 不要把 Trace 查询误路由到普通 `log`
- 不要没确认 `TraceInstanceId` 就开始组织查询
- 不要把“实例管理”和“Trace/Span 搜索”混成一路

## Default Recipes

- 看 Trace 组能力：
  `volclog capabilities --group trace --view text`
- 列 Trace 实例：
  `volclog api trace DescribeTraceInstances --describe`
- 查单个 Trace 实例：
  `volclog api trace DescribeTraceInstance --describe`
- 查单个 Trace 详情：
  `volclog api trace DescribeTrace --describe`
- 查 Span / Trace：
  `volclog api trace SearchSpans --describe`
  `volclog api trace SearchTraces --describe`

## 场景路由

- 用户说“trace 实例 / trace storage / 实例管理”：
  先用 `volclog api trace DescribeTraceInstances --describe`
- 用户说“查单条 trace 详情”：
  先用 `volclog api trace DescribeTrace --describe`
- 用户说“search traces / search spans / span 查询”：
  先用 `volclog api trace SearchTraces --describe` 或 `SearchSpans --describe`

## Core Rules

- `DescribeTraceInstances` 更偏实例管理
- `DescribeTrace / SearchTraces / SearchSpans` 更偏查询
- 查询通常依赖 `TraceInstanceId`，先确认实例范围再查

## 最小闭环

```bash
volclog api trace DescribeTraceInstances --describe
volclog api trace CreateTraceInstance --describe
volclog api trace CreateTraceInstance --print-request-template=full
volclog --dry-run api trace CreateTraceInstance --request file://req.json
volclog api trace DescribeTraceInstance --describe
volclog api trace ModifyTraceInstance --describe
volclog api trace SearchTraces --describe
volclog api trace SearchSpans --describe
```

风险点：

- 先拿到 `TraceInstanceId` 再做查询或修改
- 实例管理和查询不要混到一个 body 里

## Trace 心智模型

- Trace 实例是存储边界，`TraceInstanceId` 很关键
- `DescribeTrace` 查单条 Trace 详情
- `SearchTraces / SearchSpans` 适合条件检索，不等同于实例管理

## References

- 常见 action 分流：看 [references/action-playbook.md](references/action-playbook.md)

## Common Mistakes

- 把 Trace 查询当成普通日志查询
- 没拿到 `TraceInstanceId` 就直接查
- 详情查询和搜索查询不分

## 未命中时下一步

- 先留在 `trace`：
  `volclog capabilities --group trace --view text`
- 再锁定 action：
  `volclog api trace <Action> --describe`
- 不要把 trace 查询误切到普通 `log`
