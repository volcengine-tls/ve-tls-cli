# Log Consume

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


这个 reference 只处理“按 shard / cursor 消费日志”。

## 先用什么时候

- 消费日志
- 按 cursor 拉日志
- 从头遍历 topic 下的所有 shard
- 想保留原始包边界

## 关键约束

- 先 `DescribeShards`
- 再每个 shard 单独拿 `DescribeCursor`
- 再执行 `ConsumeLogs` 或 `ConsumeOriginalLogs`
- topic 级消费要遍历全部 shard，不要只消费第一个
- `ConsumeOriginalLogs` 更接近原始 IO 包
- `ConsumeLogs` 是更偏解析后的日志视图
- `DescribeCursor` 里的起点和时间语义要先确认；按时间消费时，不要把 cursor 当成时间戳本身

## 关键词触发可选参数

- 说“按 shard 逐个消费”“从头消费”“游标消费”时，先去拿 `DescribeShards` 和 `DescribeCursor`
- 说“原始包”“不要服务端组装”“更像写入时的 IO”时，补 `ConsumeOriginalLogs`
- 说“解析后的日志”“我想看服务端处理后的结果”时，补 `ConsumeLogs`
- 说“想知道某个 cursor 对应时间”时，先去 `DescribeCursorTime`

## 推荐流程

```bash
volclog api shard DescribeShards --describe
volclog api log DescribeCursor --describe
volclog api log ConsumeOriginalLogs --describe
```

如果用户说“我要完整消费这个 topic”，默认要补一句：遍历全部 shard，而不是只拿第一个 shard 试一条。

## 常见误用

- 不要把消费日志改写成 `log search`
- 不要把消费日志改写成 `log export`
- 不要只消费一个 shard 就当完成

## 何时切到 api-explorer

- 需要 heartbeat / checkpoint / consumer-group 管理
- 需要原始消费 API 的更底层字段说明
