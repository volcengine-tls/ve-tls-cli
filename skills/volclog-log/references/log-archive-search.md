# Log Archive Search

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


这个 reference 只处理归档检索相关 API-only 动作。

## 覆盖动作

- `ArchiveSearch`
- `CreateArchiveSearchTask`
- `DescribeArchiveSearchTask`
- `DescribeArchiveSearchTasks`
- `DeleteArchiveSearchTask`

## 先用什么时候

- 用户说“归档里搜”
- 用户说“归档任务 / 归档检索 / 归档下载”

## 关键约束

- 先区分“归档搜索”和“归档任务管理”
- 创建后再查任务状态，不要以为同步返回就是完成
- 删除前先确认任务 ID

## 常见误用

- 不要把归档检索当成普通 `log export`
- 不要把任务管理和结果检索混成一条链路
- 不要在目标是归档结果时先回到普通 search

## 何时切到 api-explorer

- 用户要更底层的归档任务参数
- 用户明确点名官方文档以外的归档接口细节
