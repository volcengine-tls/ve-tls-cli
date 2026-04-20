---
name: volclog-metric-topic
description: Use when creating, listing, reading, updating, deleting, or querying metric topics with volclog, including Chinese intents such as 指标主题 or PromQL 查询 and English intents such as metric topic, prometheus, promql, or metric search.
---

# volclog Metric Topic

## Overview

这个 skill 负责 `metric-topic` 相关场景，既包含资源管理，也包含查询。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先判断是在做资源管理还是指标查询
2. 资源管理留在 `list/get/create/modify/delete`
3. 查询优先 `metric-topic search`
4. 只有未封装 Prom API 才升级 explorer

## Agent 禁止行为

- 不要把 PromQL 查询误路由到 `log`
- 不要把资源管理和查询语义混成一个命令
- 不要跳过 `--describe`

## Shortcut First

- 列表: `volclog metric-topic list --describe`
- 创建: `volclog metric-topic create --describe`
- 查询: `volclog metric-topic search --describe`

## Default Recipes

- 列某项目下的指标主题:
  `volclog metric-topic list --project-id <ProjectId> --all`
- 查单个指标主题:
  `volclog metric-topic get --topic-id <TopicId>`
- 查单个指标主题详情并直接落文件:
  `volclog --output-mode file --output-file ./metric-topic-detail.json metric-topic get --topic-id <TopicId>`
- 做指标查询:
  `volclog metric-topic search --describe`
- 创建前看约束:
  `volclog metric-topic create --describe`
- 修改或删除前先确认状态:
  `volclog metric-topic modify --describe`
  `volclog metric-topic delete --describe`

## 场景路由

- 用户说“列指标主题 / 找 metric topic”：
  先用 `volclog metric-topic list --describe`
- 用户说“PromQL / Prometheus / metric query / 查询指标”：
  先用 `volclog metric-topic search --describe`
- 用户说“创建 / 修改 / 删除指标主题”：
  先用 `volclog metric-topic create --describe`、`modify --describe`、`delete --describe`

## References

- 需要判断资源管理和查询如何分流: 看 [references/metric-topic-routing.md](references/metric-topic-routing.md)
- 需要固定查询/资源管理配方: 看 [references/query-playbook.md](references/query-playbook.md)

## Core Rules

- 如果用户在说 PromQL / Prometheus 查询，优先 `metric-topic search`
- 如果是在说资源增删改查，优先 `list/get/create/modify/delete`
- 需要更底层 Prom API 或未封装能力时，再升级到 `volclog-api-explorer`
- 普通 PromQL 查询不要先错走 `log`
- 复数列举默认优先考虑 `--all`，不要只看第一页就下结论
- 单资源详情默认可以配合 `--output-mode file`

## 最小闭环

```bash
volclog metric-topic list --project-id <ProjectId> --all
volclog metric-topic create --describe
volclog metric-topic create --print-request-template=full
volclog metric-topic get --topic-id <TopicId>
volclog metric-topic modify --describe
volclog metric-topic delete --describe
```

## 必踩坑

- `Ttl` 不要随便填天数；优先使用已知可配档位：`15`、`30`、`90`、`180`、`365`
- 创建后短时间内立刻 `modify/delete`，服务端可能返回 `409 TaskIsRunning`
- 遇到 `TaskIsRunning` 时，不要继续盲重试；先查一次当前对象状态，再稍后重试：

```bash
volclog metric-topic get --topic-id <TopicId>
volclog metric-topic list --project-id <ProjectId>
```

## Metric Topic 心智模型

- `metric-topic` 是独立资源域，不是 `log` 的别名
- 查询和资源管理是两条不同路径，先判断再选命令

## Common Mistakes

- 不要把 PromQL 查询误路由到普通 `log search`
- 不要混淆 metric topic 资源管理和查询语义
- 不要跳过 `--describe`
- 不要把 `Ttl` 当成任意整数试错
- 创建成功后不要立刻接 `modify/delete`，先给异步任务留收敛时间
- 不要把 `create` 需要的 `--project-id` 误迁移到 `delete`

## 未命中时下一步

- shortcut 不够时：
  `volclog capabilities --group metric-topic --view text`
- 锁定 action 后：
  `volclog api metric-topic <Action> --describe`
- 不要把 PromQL 需求先误切到 `log`
