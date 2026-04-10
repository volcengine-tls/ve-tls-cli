# Intent Routing

这个 reference 只解决一件事: 用户表达应该先映射到哪个 `group`。

## Routing Table

| 用户意图关键词 | 归一化意图 | 默认 group | 第一条命令 | 升级条件 |
|---|---|---|---|---|
| 项目, project, projects, log project | 项目管理 | `project` | `volclog project list --describe` | shortcut 不覆盖的项目接口, 或需要精确 body |
| 主题, topic, topics, log topic | 主题管理 | `topic` | `volclog topic list --describe` | 需要更多主题管理接口 |
| 指标主题, metric topic, prometheus, promql, metrics query, query_range, series, labels | 指标主题 / 指标查询 | `metric-topic` | `volclog metric-topic search --describe` | 需要更底层 Prom API 或未封装接口 |
| 索引, tokenizer, field parsing, index | 索引配置 | `index` | `volclog index get --describe` | 需要复杂 body, 或 shortcut 校验不足 |
| 日志检索, search logs, export logs, analysis query, 查询日志 | 日志查询 | `log` | `volclog log search --describe` | 需要未知分析接口, 或想回退到通用 `api` |
| 消费日志, consume logs, pull logs, cursor, shard 日志, 原始日志消费, 拉取原始日志, 从游标消费 | 日志消费 | `log` | `volclog api shard DescribeShards --describe` | 需要 heartbeat/checkpoint/消费组管理时，再进入 `consumer-group`；真正拉日志继续走 `DescribeCursor` + `Consume*` |
| 写日志, put logs, ingest logs, web tracks, web tracking | 日志写入 | `log` | `volclog api log PutLogs --describe` | 需要前端埋点写入或更特殊的写入接口 |
| assistant, session answer, AI 助手, 智能问答 | 助手问答 | `assistant` | `volclog assistant describe-session-answer --describe` | 需要实例管理或底层接口 |
| 配置, profile, credential, auth check, doctor | 配置 / 诊断 | `configure` / `doctor` | `volclog doctor` | 需要进一步查看 profile、`cred_ref`、环境变量解析 |
| shipper, 投递, delivery, kafka, tos, object storage | `shipper` | `volclog-shipper` | domain skill 不覆盖后，再进入 `api shipper <Action> --describe` |
| collector, 采集器, 采集配置, 采集规则, bind host group, 机器组绑定, parse path, parse time | `collector` | `volclog-collector` | domain skill 不覆盖后，再进入 `api collector <Action> --describe` |
| alarm, 告警, 通知组, notify group, webhook, 飞书, 钉钉 | `alarm` | `volclog-alarm` | domain skill 不覆盖后，再进入 `api alarm <Action> --describe` |
| host group, machine group, 机器组, ECS 机器组, 主机纳管, auto update | `host-group` | `volclog-host-group` | domain skill 不覆盖后，再进入 `api host-group <Action> --describe` |
| consumer-group, 消费组, checkpoint, heartbeat | `consumer-group` | `volclog capabilities --group consumer-group --view text` | 锁定 action 后进入 `api consumer-group <Action> --describe`；真正拉日志不要停在这个 group |
| trace, span, 链路追踪 | `trace` | `volclog-trace` | domain skill 不覆盖后，再进入 `api trace <Action> --describe` |
| shard, dashboard, etl, processor, import, kafka-proxy, tag, olap, account, template-market | 对应同名 group | `volclog capabilities --group <group> --view text` | 锁定 action 后进入 `api <group> <Action> --describe` |

## English Normalization

当用户用英文表达时，先翻译成等价中文业务意图，再映射到稳定的 group。

Examples:

- `create a log topic` -> 创建日志主题 -> `topic.create`
- `search logs in a topic` -> 检索日志 -> `log.search`
- `consume raw logs from a topic` -> 日志消费 -> `DescribeShards -> DescribeCursor -> ConsumeOriginalLogs`
- `write logs to tls` -> 日志写入 -> `api log PutLogs`
- `update index tokenizer` -> 修改索引分词配置 -> `index.modify`
- `check why auth fails` -> 排查鉴权问题 -> `doctor`
- `bind a rule to a machine group` -> 采集规则绑定机器组 -> `collector`

## Default Escalation Path

命中 group 后，统一执行:

1. 如果该 group 有 shortcut，先跑对应 shortcut 的 `--describe`
2. 如果 shortcut 不覆盖，或该 group 本来就没有 shortcut，直接看:
   `volclog capabilities --group <group> --view text`
3. 锁定 action 后，再跑:
   `volclog api <group> <action> --describe`
4. 如果是写入型动作，再跑 `--print-request-template=full`
5. 最后才 `--dry-run` 和执行

日志消费是例外场景：

- topic 级消费通常不是单个 action 就结束，而是 `DescribeShards -> DescribeCursor -> Consume*`
- 如果用户说“消费原始日志”，默认优先 `ConsumeOriginalLogs`
- 需要消费整条 topic 数据时，agent 必须遍历全部 shard，不要只消费第一个 shard
