# Group Handoff

这个 reference 用于解决一个高频问题：快捷命令没命中后，下一步应该在**哪个 group**继续找。

核心规则：

- **先留在当前 group**，不要因为 shortcut 不够就跳到别的 group
- **已知 group 时，不要先跑 `capabilities --view groups`**；直接看该 group
- 只有用户意图本身不清楚时，才先看全局 groups

## Shortcut-backed Groups

这些 group 已有快捷命令。快捷命令没命中时，下一步是：

### `project`

```bash
volclog capabilities --group project --view text
volclog api project <Action> --describe
```

常见 action 形态：

- `DescribeProject`
- `DescribeProjects`
- `CreateProject`
- `ModifyProject`
- `DeleteProject`

### `topic`

```bash
volclog capabilities --group topic --view text
volclog api topic <Action> --describe
```

常见 action 形态：

- `DescribeTopic`
- `DescribeTopics`
- `CreateTopic`
- `ModifyTopic`
- `DeleteTopic`

### `index`

```bash
volclog capabilities --group index --view text
volclog api index <Action> --describe
```

常见 action 形态：

- `DescribeIndex`
- `CreateIndex`
- `ModifyIndex`

### `log`

```bash
volclog capabilities --group log --view text
volclog api log <Action> --describe
```

常见 action 形态：

- `SearchLogs`
- 其他日志检索/分析相关动作

### `metric-topic`

```bash
volclog capabilities --group metric-topic --view text
volclog api metric-topic <Action> --describe
```

如果 shortcut 没覆盖某个资源管理或查询动作，仍优先留在 `metric-topic` 里继续找。

### `assistant`

当前 shortcut 较少。如果需求仍明显属于 assistant：

```bash
volclog capabilities --group assistant --view text
volclog api assistant <Action> --describe
```

如果生成能力里没有 `assistant` 组，再结合现有 shortcut/文档确认是否其实是别的已知组；不要凭空猜 action。

## API-first Groups Without Shortcuts

这些 group 当前没有对应 shortcut，用户一旦明确提到这些资源名，就可以直接从该 group 开始：

### `shipper`

先读 `volclog-shipper`。如果它仍不覆盖，再：

```bash
volclog capabilities --group shipper --view text
volclog api shipper <Action> --describe
```

适用：投递、投递配置、投递状态

### `collector`

先读 `volclog-collector`。如果它仍不覆盖，再：

```bash
volclog capabilities --group collector --view text
volclog api collector <Action> --describe
```

适用：采集配置、采集任务

### `alarm`

先读 `volclog-alarm`。如果它仍不覆盖，再：

```bash
volclog capabilities --group alarm --view text
volclog api alarm <Action> --describe
```

适用：告警规则、通知策略

### `host-group`

先读 `volclog-host-group`。如果它仍不覆盖，再：

```bash
volclog capabilities --group host-group --view text
volclog api host-group <Action> --describe
```

适用：机器组、机器纳管、规则绑定、自动更新

### `consumer-group`

```bash
volclog capabilities --group consumer-group --view text
volclog api consumer-group <Action> --describe
```

适用：消费组、checkpoint、heartbeat、消费状态

注意：

- `consumer-group` 更偏管理面，不是实际拉取日志的数据面
- 用户要“消费日志 / 拉原始日志 / 按 cursor 读取”时，通常还要回到：

```bash
volclog api shard DescribeShards --describe
volclog api log DescribeCursor --describe
volclog api log ConsumeOriginalLogs --describe
```

### `trace`

先读 `volclog-trace`。如果它仍不覆盖，再：

```bash
volclog capabilities --group trace --view text
volclog api trace <Action> --describe
```

适用：Trace 存储、Trace 检索、Span/Trace 查询

### 其他当前已生成但未做 shortcut 的 group

如果用户直接提到这些组名，也可以直接按组进入：

- `account`
- `dashboard`
- `etl`
- `import`
- `kafka-proxy`
- `log-back-flow`
- `olap`
- `processor`
- `schedule-sql-task`
- `shard`
- `tag`
- `template-market`

统一做法：

```bash
volclog capabilities --group <group> --view text
volclog api <group> <Action> --describe
```

## Escalation Pattern Inside A Group

当你已经锁定 group 后，统一按下面顺序：

1. `volclog capabilities --group <group> --view text`
2. 选最接近用户意图的 action 名
3. `volclog api <group> <Action> --describe`
4. 需要 body 时再 `--print-request-template=full`
5. 执行前再 `--dry-run`

## Do Not Do

- 不要因为 shortcut 没命中，就从 `project` 跳去 `topic` 或 `log`
- 不要因为不知道 action 名，就退化成 `api call`
- 不要在已知 group 的情况下，先看全局 groups 再兜一圈
- 不要忽略已经存在的 group domain skill
