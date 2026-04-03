---
name: volclog-{{domain}}
description: "Use when {{when_to_use}}."
---

# volclog {{domain_title}}

## Overview

{{overview}}

## Design Goal

- 这个 skill 的目标是先缩小能力面，而不是鼓励模型先去探索 CLI
- 高概率命中场景要直接给出默认命令配方、常用过滤配方和输出模式
- 只有命中边界条件时，才允许升级到 explorer / capabilities

## Intent Routing

| 用户意图（中文/英文） | 归一化意图 | 第一条命令 |
|---|---|---|
| {{keyword_1}} | {{intent_1}} | `{{entry_command_1}}` |
| {{keyword_2}} | {{intent_2}} | `{{entry_command_2}}` |

## Default Recipes

- {{recipe_1}}
- {{recipe_2}}
- {{recipe_3}}

## Shortcut First

1. 先跑：`{{describe_command}}`
2. 如果是写入型命令，再跑：`{{template_command}}`
3. 如果 shortcut 不覆盖，再升级到：`volclog capabilities --group {{group}} --view text`
4. 然后：`volclog api {{group}} <action> --describe`

## References

- 需要把高频任务固化成“第一次就用对”的 workflow、过滤配方和输出策略时，把细节拆到 `references/*.md`
- `SKILL.md` 仍要保留默认配方和升级边界，不要只留下空泛导航

## Core Rules

- {{rule_1}}
- {{rule_2}}
- {{rule_3}}

## English Handling

- 当用户使用英文时，先把意图翻译成等价中文业务语义，再映射到稳定的 `group/action`
- 回复语言跟随用户，路由规则不跟随用户语言变化

## Common Mistakes

- {{mistake_1}}
- {{mistake_2}}
