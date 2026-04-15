# Collector Relations and API-Only Index

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


这页只做总索引，不重复展开细节。`collector` 组里没有放进公开 shortcut、但又经常会被点名的 API-only 内容，拆到下面三页：

- 导入任务：[`collector-import.md`](collector-import.md)
- 解析辅助：[`collector-parse-helpers.md`](collector-parse-helpers.md)
- 绑定/运维动作：[`collector-bindings-or-ops.md`](collector-bindings-or-ops.md)

## 先用什么时候

- 用户说“导入采集配置 / 看导入进度”，先看导入页
- 用户说“路径解析 / 时间解析 / 分隔符拆分 / 生成正则”，先看解析辅助页
- 用户说“绑定机器组 / 看绑定关系 / 角色绑定 / 老版规则视图”，先看绑定/运维页

## 总体规则

- 这三类都不要混进 `collector list/get/create/modify/delete` 的 shortcut 第一跳
- 如果用户其实只是想做普通规则管理，回到 shortcut
- 如果需要更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
