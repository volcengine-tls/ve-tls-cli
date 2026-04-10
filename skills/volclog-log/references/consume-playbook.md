# Consume Playbook

这个 reference 用于日志消费场景的首命令选择和固定执行顺序。

## 场景路由

| 用户场景 | 第一条命令 | 后续动作 |
|---|---|---|
| 消费解析后的日志 | `volclog api shard DescribeShards --describe` | 对每个 shard 执行 `DescribeCursor`，再执行 `ConsumeLogs` |
| 消费原始日志包 / 保留写入时 IO | `volclog api shard DescribeShards --describe` | 对每个 shard 执行 `DescribeCursor`，再执行 `ConsumeOriginalLogs` |
| 消费 Kafka 日志 | `volclog api shard DescribeShards --describe` | 对每个 shard 执行 `DescribeCursor`，再执行 `ConsumeKafkaLogs` / `ConsumeOriginalKafkaLogs` |
| 管理消费组 / checkpoint / heartbeat | `volclog capabilities --group consumer-group --view text` | 锁定 action 后进入 `api consumer-group <Action> --describe` |

## 原始日志消费固定顺序

用户说“消费原始日志 / 拉原始包 / raw package / 按 cursor 拉 topic 数据”时，按这个顺序执行：

```bash
volclog api shard DescribeShards --describe
volclog api log DescribeCursor --describe
volclog api log ConsumeOriginalLogs --describe
```

执行规则：

1. 先获取 topic 下的 shard 列表
2. 对每一个 shard 单独获取 cursor
3. 对每一个 shard 单独执行消费
4. 如果目标是整个 topic，必须遍历全部 shard

## 解析后日志消费

如果用户要的是已经解码后的日志内容，而不是原始包：

```bash
volclog api shard DescribeShards --describe
volclog api log DescribeCursor --describe
volclog api log ConsumeLogs --describe
```

理解差异时按这个心智模型：

- `ConsumeOriginalLogs`：尽量保留写入时的原始 IO 包，更适合做回放、核对原始写入内容、减少服务端额外组装
- `ConsumeLogs`：更偏解析后的日志视图，服务端可能会把多个原始 IO 组合后再返回

## 关键规则

- 日志消费不是 `search/export` 的变体，不要改走检索或导出路径
- topic 级消费不是一次 API 调用，agent 必须自己遍历 shard
- 不要只取第一个 shard
- `DescribeCursor` 是逐 shard 的，不要拿一个 cursor 复用到所有 shard
- 用户明确说“原始日志 / 原始包 / raw package”时，默认优先 `ConsumeOriginalLogs`
- 用户明确说“按写入时 IO 读取 / 不要服务端组装 / 更省服务端性能”时，也默认优先 `ConsumeOriginalLogs`
- 如果用户要保留原始包边界，不要先切 `--output jsonl`
- 只有在需要 checkpoint、heartbeat、consumer group 生命周期管理时，才进入 `consumer-group`

## 未命中时下一步

如果当前能力还不够表达需求，继续：

```bash
volclog capabilities --group shard --view text
volclog capabilities --group log --view text
volclog api <group> <Action> --describe
```
