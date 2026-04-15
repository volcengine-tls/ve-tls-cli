---
name: volclog-shared
description: Use when operating volclog global workflows such as configure or doctor, choosing between shortcut and api flows, handling JMESPath/envelope/output rules, or translating Chinese and English user intent into stable volclog groups.
---

# volclog Shared

## Overview

用这个 skill 承接 `volclog` 的全局规则。它不负责某个单独业务域，而是负责先把用户意图路由到正确的 `group`，再决定走 shortcut 还是 `api`。

这个 skill 的首要职责不是自己发明流程，而是**优先复用 CLI 已经提供的原生提示**。当 `volclog` 的帮助、`capabilities`、`api --describe`、shortcut `--describe` 已经给出下一步命令时，优先照抄这些命令，不要重新总结一套。

## Agent 快速执行顺序

1. 先判断用户需求属于哪一类：配置诊断、资源管理、日志查询、指标查询，还是底层 API 探索
2. 先命中对应 domain skill 的默认配方，不要先跑 `capabilities`
3. 如果 CLI 已经返回 `agent entry`、`agent_next_step`、`shortcut_first`、`fallback_discovery` 这类原生提示，优先直接执行这些提示
4. 如果是写入型命令，先 `--describe`，需要 body 时再 `--print-request-template=full`
5. 只有 domain skill 明确不覆盖时，才进入 `volclog-api-explorer`

## Agent 禁止行为

- 不要把 `api call` 当成默认入口
- 不要把 `capabilities` 当成第一步
- 不要在还没拿稳定 ID 时继续做下游写入
- 不要把 `--jmes-filter` 写成 envelope 路径
- 不要在大结果场景里反复把完整对象打到 stdout

## Core Rules

- 先做意图归一化，再选命令。
- 用户说中文或英文都一样，先翻译成稳定的中文业务意图，再映射到固定的 `group/action`。
- 默认优先高频 shortcut，不要一上来就跑 `capabilities`。
- 但一旦进入 CLI 探索流程，优先相信 CLI 原生提示字段，不要再自己猜下一步。
- 只有 shortcut 不覆盖时，才升级到 `capabilities -> api --describe -> template -> dry-run -> execute`。
- `--jmes-filter` 作用于原始结果，不作用于 envelope。
- 大结果优先 `--output-mode file`。
- `--dry-run` 当前只对 `api` 生效，不要假设 shortcut 也支持。

## CLI 原生提示优先级

如果已经拿到了 CLI 的输出，按这个顺序使用：

1. `shortcut --describe` / `api --describe` 里的 `guidance`
2. `capabilities` 里的 `agent entry`、`agent_entrypoint`、`agent_next_step`、`related_shortcuts`
3. group help / root help 里的示例命令
4. 最后才回到 skill 的静态配方

这意味着：

- `api --describe` 里有 `shortcut_first` 时，先尝试这些 shortcut
- shortcut `--describe` 里有 `fallback_api_describe` 时，直接用它，不要自己再拼 action
- `fallback_discovery` 已给出时，直接照抄 `volclog capabilities --group <group> --view text`
- `execute`、`template`、`dry_run` 已给出时，优先照抄，不要改写成别的等价命令

## 全局心智模型

1. `ProjectId/TopicId` 比名字稳定
2. 过滤默认作用在原始结果根，而不是 envelope
3. 大结果优先裁字段，再决定是否落盘
4. 配置/鉴权异常先看 `doctor`，再怀疑业务命令

## Default Recipes

高频需求先用这些固定入口，不要先探索：

- 列项目：`volclog project list`
- 列主题：`volclog topic list --project-id <ProjectId>`
- 建主题：`volclog topic create --describe`
- 看索引：`volclog index get --topic-id <TopicId>`
- 查日志：`volclog log search --describe`
- 消费原始日志：`volclog api shard DescribeShards --describe`
- 导出原始日志：`volclog --output-mode file log export --describe`
- 导出分析结果：`volclog --output-mode file log export-analysis --describe`
- 批量写日志：`volclog log ingest --describe`

## Intent Routing

优先读这个 skill，再按需读 reference：

- 意图不明确、用户中英文混说、需要判断应该进哪个 group：看 [references/intent-routing.md](references/intent-routing.md)
- 需要处理 envelope、`--jmes-filter`、`--output-mode file`、shell quoting：看 [references/output-and-filter.md](references/output-and-filter.md)
- 需要排查凭证、`cred_ref`、profile、endpoint/region：看 [references/config-and-doctor.md](references/config-and-doctor.md)
- 需要“第一次就用对”的固定命令配方和升级边界：看 [references/first-response-playbook.md](references/first-response-playbook.md)
- 需要常用字段提取配方：看 [references/filter-cookbook.md](references/filter-cookbook.md)
- 需要常见错误定位方法：看 [references/error-quick-reference.md](references/error-quick-reference.md)

## English Handling

- 当用户使用英文时，先把语义翻译成等价中文意图，再进入同一套路由。
- 回复语言跟随用户，但命令路由始终使用稳定的 `volclog` group 名称。

## Shortcut First

优先顺序：

1. `volclog <group> <shortcut> --describe`
2. 直接读它返回的 `guidance`
3. 若是写入型 shortcut，再跑 `guidance.template`
4. 若 shortcut 不覆盖，再跑 `guidance.fallback_discovery`
5. 然后跑 `guidance.fallback_api_describe`
6. 再生成模板、`--dry-run`、执行

如果 shortcut `--describe` 已经给了 `fallback_api_describe`，不要自己再把 group/action 从文本里重新提炼一遍。

## Do Not Explore First

以下情况不要先跑 `capabilities`：

- 用户只是要列资源、拿 ID、创建 topic、查看索引、检索日志
- skill 已经给了默认命令配方
- 需求明显落在 `project/topic/index/log/host-group/collector/metric-topic` 高频 shortcut 里

只有这些情况才升级：

- shortcut 不覆盖
- 用户明确指定 OpenAPI action
- 需要未封装分页或更精确 body

## Capabilities 使用规则

只有在 domain skill 和 shortcut 都没给出稳定入口时，才进入 `capabilities`。

进入后按固定方式使用：

- `volclog capabilities --view groups`
  先看 group 概览和 `agent entry`
- `volclog capabilities --group <group> --view text`
  先看组内 action 和首条建议命令
- `volclog capabilities --group <group> --action <action> --view full`
  只在需要更稳定机器可读约束时才看

如果 `capabilities` 已经返回：

- `agent_entrypoint: shortcut-first`
- `agent_next_step: volclog ... --describe`
- `related_shortcuts: [...]`

就直接使用这些字段，不要继续全局探索。

## Common Mistakes

- 不要把 shortcut 当成“仅供人类临时用”的入口；现在它们也是 agent-first 入口。
- 不要写 `data.Total` 这类 envelope 路径；过滤时直接写 `Total`。
- 不要把 `api call` 当默认入口；只有 method/path 已经确定时才用。
- 不要忽略 `doctor`；配置、凭证、`cred_ref`、region/endpoint 异常先看它。
- 不要在 CLI 已给出 `guidance` 或 `agent entry` 后，仍然自己改写下一步命令。
