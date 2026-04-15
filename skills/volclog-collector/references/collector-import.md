# Collector Import

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

用于 `CreateConfigImportTask` / `DescribeConfigImportTasks`。

## 什么时候看这个 reference

- 用户说“导入采集配置”
- 用户说“看导入进度 / 导入任务状态”
- 用户明确要把外部配置搬进 `collector`

## 推荐命令

```bash
volclog api collector CreateConfigImportTask --describe
volclog api collector DescribeConfigImportTasks --describe
```

## 参数约束

- 导入配置仍属于 `collector` 组，不要切错 group
- 通常要先准备导入源，再看任务结果
- 导入任务和创建/修改规则不是一类动作

## 常见误用

- 把导入当成普通规则 create/modify
- 创建完导入任务就不再跟进状态
- 误把别的 group 的配置导入能力带进来

## 何时切换

- 如果用户只是要建/改/删规则，回到 shortcut
- 如果需要更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
