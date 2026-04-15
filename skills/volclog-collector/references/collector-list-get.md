# Collector List / Get

## 适用场景
本页用于当前 collector API-only 动作族；先判断是规则读写、绑定关系、导入任务还是解析辅助，再看下方专题。

## 必填输入
先确认本页“覆盖动作”里的核心主键，通常是 `RuleId`、`TopicId`、`HostGroupIds`，或导入/解析所需的样例输入。

## 可选参数触发词
见本页后续的关键词触发、推荐命令或常见误用；如果用户只是普通规则管理，优先回到 `collector list/get/create/modify/delete`。

## 字段联动/限制
以本页的参数约束和字段联动为准；绑定、导入、解析辅助和规则本体是不同链路，不要混。

## 常见误用
不要把绑定关系当成规则本体，不要把导入任务当成规则创建，不要在没有主键时直接批量操作。

## 下一步命令
先执行本页或对应专题页给出的推荐命令；如果要更底层字段，再去 `volclog-api-explorer`。


## 适用范围

用于列采集规则、查单条规则、先拿 `RuleId` 再做后续操作。

## 优先用法

```bash
volclog collector list --describe
volclog collector get --describe
```

## 参数约束

- `list` 先考虑 `--all`，避免漏页。
- `get` 先确认 `--rule-id`。
- 大结果或深层详情优先 `--output-mode file`。
- 当前公开读路径是 `DescribeRulesV2` / `DescribeRuleV2`。

## 关键词触发

| 用户说法 | 优先参数 | 用途 |
|---|---|---|
| `列全量` / `全部规则` / `怕漏页` | `--all` | 把采集规则尽量列全 |
| `按规则名查` / `模糊查规则` | `--rule-name` | 走名称过滤 |
| `按规则 ID 查` / `精确看某条规则` | `--rule-id` | 走稳定主键 |
| `按项目查` / `按 topic 查` | `--project-id` / `--project-name` / `--topic-id` / `--topic-name` | 按资源上下文筛规则 |
| `只看某个 IAM 项目` | `--iam-project-name` | 限定 IAM 命名空间 |
| `结果很多` / `只保留字段` | `--output-mode file` / `--jmes-filter` | 降低 stdout 噪音 |

## 字段联动

- `RuleId` 是后续 `modify/delete/binding` 的稳定定位键。
- `RuleName` 和 `TopicId/TopicName` 适合用于检索，不适合当稳定主键。
- `ProjectId` / `ProjectName` / `IamProjectName` 常作为列表过滤条件一起使用。

## 常见误用

- 没拿 `RuleId` 就直接改或删。
- 只看默认一页，导致漏掉规则。
- 把规则列表当成绑定详情。

## 何时切模板 / api

- 只要用户要的是“列 / 查”，先用 shortcut。
- 如果用户明确要 shortcut 没有暴露的筛选条件，再看 `api --describe`。
- 如果需要更底层的 method/path，才交给 `api-explorer`。
