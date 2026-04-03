---
name: volclog-suite
description: Use when operating Volcengine TLS through volclog across configure, doctor, project, topic, metric-topic, index, log, assistant, or api explorer workflows.
---

# volclog Master Skill

## Overview

这是总入口 skill。先把用户意图路由到正确的 domain，再决定走 shortcut 还是 `api`。

## Design Goal

- 先把高频场景压到预制 shortcut 和 workflow
- 不要让模型在明明已有稳定入口时，仍去 `capabilities` 重新探索
- 只有 domain skill 明确说“不覆盖”时，才切到 `volclog-api-explorer`

## Global Routing

1. 先读取 `volclog-shared`
2. 根据用户意图命中对应 domain：
   - `project`
   - `topic`
   - `metric-topic`
   - `index`
   - `log`
   - `assistant`
3. 如果 domain shortcut 不覆盖，再切到 `volclog-api-explorer`

## References

- 全局路由、输出、过滤、doctor/configure 规则: 交给 `volclog-shared`
- 具体业务域细节: 交给各自 domain skill 的 `references/*.md`
- 未知接口探索: 交给 `volclog-api-explorer`

## Discovery Commands

```bash
volclog capabilities --view groups
volclog capabilities --group <group> --view text
volclog api <group> <action> --describe
```

## Shortcut Discovery

```bash
volclog project create --describe
volclog topic create --describe
volclog index create --describe
volclog log search --describe
```
