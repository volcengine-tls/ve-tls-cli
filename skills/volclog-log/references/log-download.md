# Log Download

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


这个 reference 只处理下载任务相关 API-only 动作。

## 覆盖动作

- `CreateDownloadTask`
- `DescribeDownloadTasks`
- `DescribeDownloadUrl`
- `CancelDownloadTask`

## 先用什么时候

- 用户说“下载任务 / 任务状态 / 下载链接 / 取消任务”
- 用户要把检索结果异步生成下载包

## 关键约束

- 先 `CreateDownloadTask`
- 再 `DescribeDownloadTasks` 看任务状态
- 真正拿链接时再 `DescribeDownloadUrl`
- 取消任务用 `CancelDownloadTask`

## 常见误用

- 不要把下载任务当成导出结果本身
- 不要只看一次状态就当任务结束
- 不要把下载链路改写成 `log export`

## 何时切到 api-explorer

- 用户要更底层的任务筛选字段
- 用户明确点名官方文档以外的下载参数
