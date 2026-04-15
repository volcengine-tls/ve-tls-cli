# Log WebTracking And Kafka

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


这个 reference 只处理 WebTracking 和 Kafka 相关 API-only 动作。

## 覆盖动作

- `WebTrack`
- `WebTrackGif`
- `WebTrackUaGif`
- `WebTracks`
- `DescribeKafkaOffset`
- `ConsumeKafkaLogs`
- `ConsumeOriginalKafkaLogs`

## 先用什么时候

- 用户说“前端埋点 / WebTracking / 公网匿名写入”
- 用户说“Kafka offset / Kafka 原始消费 / Kafka 拉日志”

## 关键约束

- `WebTracks` 不是默认安全的写入入口，先确认主题已开启 WebTracking
- `WebTrackGif` / `WebTrackUaGif` 只在对应埋点链路中用
- Kafka 消费先确认 offset 语义，再决定 `ConsumeKafkaLogs` 还是 `ConsumeOriginalKafkaLogs`
- Kafka 消费不要和 shard cursor 消费混用

## 常见误用

- 不要把 WebTracking 当成普通写入
- 不要默认认为匿名写入没有风险
- 不要把 Kafka 消费改写成 `log search`

## 何时切到 api-explorer

- 用户要更底层的埋点或 Kafka 调试字段
- 用户明确点名官方文档之外的参数
