---
name: volclog-collector
description: Use when operating TLS collector rules with volclog, including Chinese intents such as 采集配置、采集规则、绑定机器组 or 导入采集配置 and English intents such as collector, collection rule, bind host group, or config import.
---

# volclog Collector

## Overview

这个 skill 负责 `collector` 组。基础采集规则管理优先走 `collector` shortcut；绑定机器组、导入配置、解析辅助再升级到 `api`。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 基础采集规则管理先用 `collector list/get/create/modify/delete`
2. 如果要绑定机器组、导入配置或做解析辅助，再进入 `capabilities -> api --describe`
3. 写入动作先模板、再 `--dry-run`

## Agent 禁止行为

- 不要把采集配置误路由到 `shipper`
- 不要没确认 `TopicId/RuleId/HostGroupIds` 就直接执行绑定或修改
- 不要跳过解析辅助动作，直接猜路径/时间解析

## Default Recipes

- 看采集组能力：
  `volclog capabilities --group collector --view text`
- 创建采集规则：
  `volclog collector create --describe`
- 查看采集规则：
  `volclog collector list --describe`
- 绑定机器组：
  `volclog api collector ApplyRuleToHostGroups --describe`
- 导入采集配置：
  `volclog api collector CreateConfigImportTask --describe`

## 场景路由

- 用户说“采集规则 / collection rule / 采集配置”：
  先用 `volclog collector list --describe`、`collector get --describe` 或 `collector create --describe`
- 用户说“绑定机器组 / bind host group”：
  先用 `volclog api collector ApplyRuleToHostGroups --describe`
- 用户说“导入配置 / import collector config”：
  先用 `volclog api collector CreateConfigImportTask --describe`
- 用户说“路径解析 / 时间解析 / split with quote”：
  先用解析辅助 action 的 `--describe`，不要先猜规则

## Core Rules

- `collector list/get/create/modify/delete` 是基础采集规则管理主路径
- `collector` 也承接机器组绑定、导入配置、解析辅助
- 当需求涉及路径解析、时间解析、拆分规则时，先找解析型 action，不要自己猜正则
- 写入前先看 request template，避免把 `Pause`、`InputType`、`RuleType` 等关键字段猜错
- `ParseTime` 对 `TimeFormat` 很敏感；不要默认按 Go time layout 传参

## 关键字段说明

- `RuleId`：
  已有采集规则的稳定标识；修改、删除、绑定前先确认它
- `TopicId`：
  规则最终写入的日志主题；不要在还没确认 `TopicId` 时先绑机器组
- `HostGroupIds`：
  规则和机器组关联时的核心字段；绑定/解绑前先确认机器组范围
- `RuleType`：
  决定规则类型；先看 `--describe` 和模板，不要凭经验猜枚举值
- `InputType`：
  决定采集输入源形态；路径采集、容器采集等场景差别很大
- `Pause`：
  影响规则是否立即生效；如果用户只是先创建再调试，要明确是否需要暂停

这些字段是低能力模型最容易猜错的部分。遇到复杂规则时，优先：

```bash
volclog api collector <Action> --describe
volclog api collector <Action> --print-request-template=full
```

## Collector 心智模型

- `CreateRule / ModifyRule / DeleteRule` 管采集规则
- `ApplyRuleToHostGroups` 管规则与机器组关系
- `ParsePath / ParseTime / SplitWithQuote` 是辅助校验动作，适合先验证规则思路

## Parse Helpers 风险提示

- `ParsePath`：先验证路径样例和正则，再回填正式规则
- `ParseTime`：`TimeFormat` 可能是固定格式或有限枚举；不要默认传 Go layout，例如 `2006-01-02 15:04:05`
- `SplitWithQuote`：带分隔符和引号的日志先用它确认拆分效果，再写正式采集规则
- 如果 `ParseTime` 返回 `InvalidArgument`，优先回到 `--describe` / 模板，不要继续试错时间格式

```bash
volclog api collector ParseTime --describe
volclog api collector ParseTime --print-request-template=full
```

## References

- 常见 action 分流：看 [references/action-playbook.md](references/action-playbook.md)

## Common Mistakes

- 采集规则还没看清，就直接绑定机器组
- 路径/时间解析问题直接手调正则，不用解析辅助 action
- 把导入配置任务和规则创建混成一路
- 把 `TimeFormat` 按 Go time layout 直接传给 `ParseTime`

## 未命中时下一步

- 先留在 `collector`：
  `volclog capabilities --group collector --view text`
- 再锁定 action：
  `volclog api collector <Action> --describe`
- 不要把采集规则问题误切到 `shipper` 或 `host-group`
