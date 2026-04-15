# Log Saved Search

## 适用场景
本页用于当前动作族的 API-only 场景；先把用户意图收敛到本页覆盖的动作，再决定是否继续看 shortcut 或 api-explorer。

## 必填输入
先确认本页“覆盖动作”里对应动作的主键或核心输入，例如 TopicId、RuleId、TaskId、Query、Cursor、Request body。

## 可选参数触发词
见本页后续的关键词触发、推荐命令或常见误用；如果用户只是泛泛描述，先按主键优先，不要提前补一堆筛选项。

## 字段联动/限制
以本页的参数约束、字段联动、任务状态或消费语义为准；字段多时先看 `--describe`，不要靠记忆拼。

## 常见误用
不要把本页动作混成普通检索、普通写入、普通删除或普通读详情；不要在主键没确认前直接执行高风险操作。

## 下一步命令
先执行本页给出的推荐命令；如果仍不够，再转对应的 `volclog-api-explorer` 或更细的 shortcut reference。


这个 reference 只处理保存检索与收藏相关 API-only 动作。

## 覆盖动作

- `CreateSavedSearch`
- `DescribeSavedSearch`
- `DescribeSavedSearches`
- `ModifySavedSearch`
- `DeleteSavedSearch`
- `AddFavourite`
- `RemoveFavourite`
- `DescribeFavourites`

## 先用什么时候

- 用户说“保存这个查询 / 以后复用 / 常用检索”
- 用户说“收藏 / 取消收藏 / 查看收藏列表”

## 关键约束

- 保存检索和收藏是两条不同链路，不要混
- 先保存查询再谈收藏，避免把临时查询当长期资产
- 修改/删除前先确认目标保存项或收藏项的 ID

## 常见误用

- 不要把保存检索当成普通 search
- 不要把收藏当成查询本体
- 不要混用查询内容和收藏列表的概念

## 何时切到 api-explorer

- 用户要更底层的保存检索字段
- 用户明确点名保存检索或收藏的官方原始参数
