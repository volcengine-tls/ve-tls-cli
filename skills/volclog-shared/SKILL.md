---
name: volclog-shared
description: Use when operating volclog global workflows such as first-time setup, configure or doctor, choosing between shortcut and api flows, handling output/filter rules, or routing Chinese and English TLS intents into stable volclog groups.
---

# volclog Shared

## Overview

这个 skill 只负责全局前置规则，不负责某个具体资源域。

它解决四件事：

- 首次使用 `volclog` 时先做什么
- 用户意图应该落到哪个公开 group
- 何时优先 shortcut，何时升级到 `api`
- 输出、过滤、配置诊断这些跨 group 规则怎么处理

## 使用顺序

1. 先判断是不是配置/环境问题
2. 不是环境问题时，先命中公开 domain skill
3. 公开 shortcut 不覆盖，再升级到 `volclog-api-explorer`
4. 只有 method/path 已经明确时，才退到 `api call`

## 首次使用先做什么

如果用户是第一次使用 CLI，或当前命令表现像配置问题，先读这些 reference：

- 配置 profile、`cred_ref`、环境变量、凭证诊断：看 [references/config-and-doctor.md](references/config-and-doctor.md)
- 常见错误的第一步判断：看 [references/error-quick-reference.md](references/error-quick-reference.md)

这类场景的固定入口是：

- `volclog doctor`
- `volclog configure list`

## Domain Skill 入口

对外版优先使用这些公开 skill，不要一上来就跑 `capabilities`：

- `project`
- `topic`
- `index`
- `log`
- `host-group`
- `collector`
- `metric-topic`

意图不明确时，再看 [references/intent-routing.md](references/intent-routing.md)。

## 决策顺序

固定升级路径：

1. `volclog <group> <shortcut> --describe`
2. 读该 shortcut skill 对应 reference
3. shortcut 不覆盖时，进入 `volclog-api-explorer`
4. `volclog capabilities --group <group> --view text`
5. `volclog api <group> <Action> --describe`
6. 如果是 body 写入，再 `--print-request-template=required|full`
7. 最后才执行

## 全局规则

- `ProjectId`、`TopicId`、`RuleId`、`HostGroupId` 这类稳定 ID 优先于名字
- 列举型命令优先考虑 `--all`
- 单对象详情或大结果优先 `--output-mode file`
- `--jmes-filter` 作用在原始结果根，不作用于 envelope
- 不要把一个命令里记住的参数机械迁移到同组其他命令
- 写入型命令先 `--describe`；字段多时优先模板

## 何时读哪个 Reference

- 判断用户意图应进哪个 group：看 [references/intent-routing.md](references/intent-routing.md)
- 第一次响应就要给稳妥命令：看 [references/first-response-playbook.md](references/first-response-playbook.md)
- 处理 `--output-mode file`、`--jmes-filter`、shell quoting：看 [references/output-and-filter.md](references/output-and-filter.md)
- 提取常用 ID/名称字段：看 [references/filter-cookbook.md](references/filter-cookbook.md)
- 排查配置、凭证、endpoint、region：看 [references/config-and-doctor.md](references/config-and-doctor.md)
- 先做错误归因：看 [references/error-quick-reference.md](references/error-quick-reference.md)

## 不要这样做

- 不要把 `api call` 当默认入口
- 不要把 `capabilities` 当第一步
- 不要在没拿稳定 ID 时继续做下游写入
- 不要在大结果场景里反复把完整对象打到 stdout
- 不要自己重写一套 CLI 已经给出的下一步命令

## English Handling

- 用户用英文时，先归一化成中文业务意图，再映射到稳定的 `group`
- 回复语言跟随用户，命令路径保持 `volclog` 原始 group 名
