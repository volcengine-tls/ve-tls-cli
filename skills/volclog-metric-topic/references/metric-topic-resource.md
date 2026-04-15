# Metric Topic Resource Management

## 适用场景

- 处理 `DescribeMetricTopics`
- 处理 `DescribeMetricTopic`
- 处理 `CreateMetricTopic`、`ModifyMetricTopic`、`DeleteMetricTopic`

## 必填输入

- `list` 已知项目范围时优先提供 `ProjectId`
- `get/modify/delete` 以 `TopicId` 为准
- `create` 先确认 `ProjectId`、`TopicName`、`Ttl`、`ShardCount`

## 可选参数触发词

- 说“某个项目下”时，补 `--project-id`
- 说“全量”“别漏”时，补 `--all`
- 说“只看一个指标主题”时，补 `--topic-id`
- 说“分层存储”“自动分裂”“热/冷/归档”时，补 `--enable-hot-ttl`、`--hot-ttl`、`--cold-ttl`、`--archive-ttl`、`--auto-split`
- 说“字段多”“先给模板”时，补 `--print-request-template=full` 或 `--request`

## 字段联动/限制

- `create` 需要 `ProjectId`，但 `get/modify/delete` 不需要；不要横向迁移参数记忆
- `Ttl` 优先使用已知档位：15 天、30 天、90 天、180 天、1 年
- `EnableHotTtl=true` 时，`Ttl` 要和 `HotTtl + ColdTtl + ArchiveTtl` 对齐
- `AutoSplit=true` 时，`MaxSplitShard` 不能空，且要大于 `ShardCount`
- 刚创建完立刻 modify/delete 可能遇到 `409 TaskIsRunning`

## 常见误用

- 把 `ProjectId` 误塞进 `delete`
- 对刚创建的资源立刻改/删，不给异步任务收敛时间
- 把资源管理命令当 PromQL 查询用
- 把 `Ttl` 当秒数或任意整数去试

## 下一步命令

```bash
volclog metric-topic list --project-id <ProjectId> --all
volclog metric-topic get --topic-id <TopicId>
volclog metric-topic create --describe
volclog metric-topic modify --describe
volclog metric-topic delete --describe
```
