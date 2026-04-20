---
name: volclog-alarm
description: Use when operating TLS alarms with volclog, including Chinese intents such as 告警、通知组、Webhook 集成 or 告警内容模板 and English intents such as alarm, alert policy, webhook integration, or alarm template.
---

# volclog Alarm

## Overview

这个 skill 负责 `alarm` 组。它适合告警策略、告警通知模板、通知组与 Webhook 集成等操作。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先判断是告警策略、通知模板，还是 Webhook 集成
2. 先看 `volclog capabilities --group alarm --view text`
3. 再选 action，执行 `volclog api alarm <Action> --describe`
4. 写入型动作先模板、再 `--dry-run`

## Agent 禁止行为

- 不要没分清“告警策略”和“通知/Webhook 集成”就直接写 body
- 不要跳过 `QueryRequest`、`Condition`、`TriggerConditions` 约束
- 不要把飞书/钉钉/Webhook 集成和告警策略本体混成一个对象

## Default Recipes

- 看告警组能力：
  `volclog capabilities --group alarm --view text`
- 列告警策略：
  `volclog api alarm DescribeAlarms --describe`
- 创建告警策略：
  `volclog api alarm CreateAlarm --describe`
- 修改告警策略：
  `volclog api alarm ModifyAlarm --describe`
- 删除或停用告警策略：
  `volclog api alarm DeleteAlarm --describe`
  `volclog api alarm DisableAlarm --describe`
- 创建 Webhook 集成：
  `volclog api alarm CreateAlarmWebhookIntegration --describe`

## 场景路由

- 用户说“告警策略 / alert policy / 告警规则”：
  先用 `volclog api alarm CreateAlarm --describe` 或 `ModifyAlarm --describe`
- 用户说“通知模板 / 告警内容模板”：
  先锁定模板相关 action，再看 `--describe`
- 用户说“Webhook 集成 / 飞书 / 钉钉 / 通知渠道”：
  先用 `volclog api alarm CreateAlarmWebhookIntegration --describe`

## Core Rules

- `alarm` 组的高风险点在于 body 结构复杂，不适合凭记忆硬写
- 先确认是在写“策略本体”、还是“通知渠道/模板”
- 当 `Condition`、`Severity`、`TriggerConditions` 之间有互斥或替代关系时，以 `--describe` / 模板为准

## 最小闭环

```bash
volclog api alarm DescribeAlarms --describe
volclog api alarm CreateAlarm --describe
volclog api alarm CreateAlarm --print-request-template=full
volclog --dry-run api alarm CreateAlarm --request file://req.json
volclog api alarm ModifyAlarm --describe
volclog api alarm DisableAlarm --describe
volclog api alarm DeleteAlarm --describe
```

风险点：

- 告警策略 body 往往是异步联动多个子结构，先模板后执行
- 通知组 / Webhook / 内容模板与策略本体分开处理，不要试图一次塞进单个对象

## Alarm 心智模型

- `CreateAlarm / ModifyAlarm / DeleteAlarm` 管策略本体
- 模板、通知组、Webhook 集成是告警周边能力，不要和策略本体混为一体
- 告警策略往往依赖查询语句和触发条件，先把结构看清楚再写

## References

- 常见 action 分流：看 [references/action-playbook.md](references/action-playbook.md)

## Common Mistakes

- 没看清触发条件结构就开始写
- 把模板、通知组、Webhook 和策略本体混成一份请求
- 不做 `--dry-run` 就直接发复杂告警 body

## 未命中时下一步

- 先留在 `alarm`：
  `volclog capabilities --group alarm --view text`
- 再锁定 action：
  `volclog api alarm <Action> --describe`
- 不要把告警周边能力误切到别的 group
