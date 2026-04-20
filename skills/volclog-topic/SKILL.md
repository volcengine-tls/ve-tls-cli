---
name: volclog-topic
description: Use when creating, listing, reading, updating, or deleting TLS log topics with volclog, including Chinese intents such as 创建主题 or 修改主题 and English intents such as create topic, list topics, update topic, or delete topic.
---

# volclog Topic

## Overview

这个 skill 负责主题管理场景，优先走 `topic` shortcut，并在字段变多时切换到模板驱动方式。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 先确认 `ProjectId`
2. 先列主题并提取 `TopicId/TopicName`
3. 写入前先 `create/modify --describe`
4. 字段多时直接转模板，不要继续堆 flags

## Agent 禁止行为

- 不要没拿 `ProjectId` 就开始列主题
- 不要用 `TopicName` 代替稳定的 `TopicId`
- 不要在字段多时继续手写长命令

## Default Recipes

- 列项目下主题：
  `volclog topic list --project-id <ProjectId> --all --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"`
- 看单个主题详情并直接落文件：
  `volclog --output-mode file --output-file ./topic-detail.json topic get --topic-id <TopicId>`
- 创建前先看约束：
  `volclog topic create --describe`
- 写入字段较多时先拿模板：
  `volclog topic create --print-request-template=full`

## 场景路由

- 用户说“某个项目下有哪些主题 / list topics”：
  先用 `volclog topic list --describe`；如果目标是“列全”，继续补 `--all`
- 用户说“拿 TopicId / 找主题 / 主题清单”：
  先用 `volclog topic list --project-id <ProjectId>`
- 用户说“创建主题 / 修改 TTL / 改 shard / 开自动分裂”：
  先用 `volclog topic create --describe` 或 `volclog topic modify --describe`
- 用户说“删主题”：
  先确认 `TopicId`，再用 `volclog topic delete --describe`

## References

- 需要完整的创建/修改写入流程和常见字段提醒: 看 [references/topic-write-workflow.md](references/topic-write-workflow.md)
- 需要更少探索的固定 list/create 配方: 看 [references/topic-playbook.md](references/topic-playbook.md)
- 需要快速定位高频坑、参数边界和常见误用: 看 [references/error-quick-reference.md](references/error-quick-reference.md)

## Shortcut First

- 列表：`volclog topic list --describe`
- 创建：`volclog topic create --describe`
- 修改：`volclog topic modify --describe`

## Write Workflow

1. 先看约束：`volclog topic create --describe`
2. 再生成模板：`volclog topic create --print-request-template=full`
3. 写入 `req.json`
4. 再执行：`volclog topic create --request file://req.json`

## Common Mistakes

- 不要同时把 `TopicName` 和 `TopicId` 用在 list 场景里
- `AutoSplit=true` 时别忘了 `MaxSplitShard`
- 字段很多时不要硬拼 flags，直接回到模板流程
- 普通列主题和拿 `TopicId` 时，不要先跑 `capabilities`
- 不要把 `topic create` 里记住的 `--project-id` 机械迁移到 `topic delete`

## Topic 心智模型

- `TopicId` 是 index、log 等后续命令的稳定入口
- topic 创建最容易错在字段变多后仍坚持 flags；这时应立即切到模板

## 参数边界

- 先拿 `ProjectId` 再列主题
- 先拿 `TopicId` 再做 index/log 下游操作
- 当字段开始变多时，立即从 flags 切到模板或结构化输入
- `list/create` 常常依赖 `--project-id`
- `get/modify/delete` 常常依赖 `--topic-id`
- 同组命令参数要求可能不同，先看 `--describe`，不要靠上一个命令的记忆迁移

## 未命中时下一步

- shortcut 不够时：
  `volclog capabilities --group topic --view text`
- 锁定 action 后：
  `volclog api topic <Action> --describe`
- 不要把主题管理误切到 `project`、`index` 或 `log`
