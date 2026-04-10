# First Response Playbook

这个 reference 的目标不是帮模型探索，而是让它第一次就走到更窄、更稳的入口。

## 用户要“先看看有哪些项目”

先用：

```bash
volclog project list --all --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
```

不要先用 `capabilities`。

## 用户要“拿某个项目下的主题”

先用：

```bash
volclog topic list --project-id <ProjectId> --all --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"
```

不要先升级成 `api topic DescribeTopics`。

## 用户要“创建主题”

先用：

```bash
volclog topic create --describe
volclog topic create --print-request-template=full
```

填完模板后再执行，不要直接猜请求体。

## 用户要“查看或修改索引”

先用：

```bash
volclog index get --topic-id <TopicId>
volclog index create --print-request-template=full
```

再配合 `--topic-id` 执行。

## 用户要“查日志”

先用：

```bash
volclog log search --describe
```

如果用户明确说“导出很多日志”或“结果很大”，再切到：

```bash
volclog --output-mode file log export --describe
```

## 用户要“消费日志 / 按游标拉日志 / 拉原始日志包”

先用：

```bash
volclog api shard DescribeShards --describe
volclog api log DescribeCursor --describe
volclog api log ConsumeOriginalLogs --describe
```

固定顺序：

1. 先拿 topic 下的 shard 列表
2. 对每一个 shard 单独获取 cursor
3. 再对每一个 shard 单独消费

关键提醒：

- topic 级消费不是一条 `Consume*` 就结束，agent 需要遍历全部 shard
- 用户明确说“原始日志 / 原始包 / raw package”时，优先 `ConsumeOriginalLogs`
- 用户明确说“保留原始写入 IO / 不要服务端组装 / 更省服务端性能”时，也优先 `ConsumeOriginalLogs`
- 如果用户要的是解析后的日志，再考虑 `ConsumeLogs`
- 如果用户要保留原始包边界，不要先切 `jsonl`
- 这是消费链路，不要改走 `log search` / `log export`
- 如果用户同时提到 checkpoint / heartbeat / consumer group，再补看 `consumer-group`

## 用户要“做分析导出”

先用：

```bash
volclog --output-mode file log export-analysis --describe
```

不要把分析语句先塞进 `log export`。

## Escalate Only When

- skill 没给出明确入口
- 现有 shortcut 无法表达需求
- 用户明确指定底层接口或 OpenAPI action

## If The Group Is Clear But No Shortcut Exists

如果用户说的资源名本身已经能锁定 group：

- `shipper`：先用 `volclog-shipper`
- `collector`：先用 `volclog-collector`
- `alarm`：先用 `volclog-alarm`
- `host-group`：先用 `volclog-host-group`
- `trace`：先用 `volclog-trace`

如果是当前还没有专属 domain skill 的 API-first group，例如 `consumer-group`，再直接：

```bash
volclog capabilities --group <group> --view text
volclog api <group> <Action> --describe
```

如果用户说的是当前已有 shortcut 的 group，但 shortcut 没命中，也优先留在原 group 里继续找，不要换组。

注意：

- `consumer-group` 更偏管理面（group / checkpoint / heartbeat）
- 真正消费日志的数据面常常是 `shard + log` 联动，不要把“消费日志”直接改写成 `search/export`

## List / Detail 默认策略

- 复数 `Describe...s` 或 shortcut `list` 场景：优先考虑 `--all`
- 单对象详情场景：优先考虑 `--output-mode file` / `--output-file`
- 同一个 group 下不同 action 的参数往往不同；不要把 `create` 的参数记忆迁移到 `delete/get`
