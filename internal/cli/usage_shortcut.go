//go:build !agent

package cli

func usageProject() string {
	return u(`Usage:
  tlsctl project <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 project 操作时可直接使用。
  - Agent 默认不要停在 shortcut 元命令；先转到 tlsctl tool list/describe project。
  - 当前 shortcut 仍支持 --describe；只有明确要走人工 shortcut 时再看 --print-request-template=full。
  - 需要公开 API 契约时，转到 tlsctl tool list project / tlsctl tool describe project.create。
  - 字段较多时，用 --print-request-template=full + --request file://req.json 组织完整 JSON。

Commands:
  list     List projects
  get      Get a project by id
  create   Create a project
  modify   Modify a project
  delete   Delete a project

场景速选:
  - 列项目/拿 ProjectId: volclog project list --describe
  - 按项目名过滤: volclog project list --project-name <name>
  - 看单项目详情: volclog project get --describe
  - 创建或修改项目: volclog project create --describe / volclog project modify --describe
  - 字段较多时组织 body: volclog project create --print-request-template=full

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageTopic() string {
	return u(`Usage:
  tlsctl topic <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 topic 操作时可直接使用。
  - Agent 默认不要停在 shortcut 元命令；先转到 tlsctl tool list/describe topic。
  - 当前 shortcut 仍支持 --describe；只有明确要走人工 shortcut 时再看 --print-request-template=full。
  - 需要公开 API 契约时，转到 tlsctl tool list topic / tlsctl tool describe topic.<action>。
  - 字段较多时，用 --print-request-template=full + --request file://req.json 组织完整 JSON。

Commands:
  list     List topics
  get      Get a topic by id
  create   Create a topic
  modify   Modify a topic
  delete   Delete a topic

场景速选:
  - 列主题/拿 TopicId: volclog topic list --describe
  - 创建或修改主题: volclog topic create --describe / volclog topic modify --describe
  - 字段较多时组织 body: volclog topic create --print-request-template=full

Notes:
  - TopicName and TopicId cannot be provided together for list.
  - --all 会自动翻完分页；不要与 --page-number / --cursor 混用。
  - Complex request bodies can be passed via --request file://...
  - --request also accepts "-" to read JSON from stdin.

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageMetricTopic() string {
	return u(`Usage:
  tlsctl metric-topic <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 metric-topic 操作时可直接使用。
  - Agent 不要把这里当主流程；对外 agent tool 不暴露 metric-topic。
  - 当前 shortcut 仍支持 --describe；只有明确要走人工 shortcut 时再看 --print-request-template=full。
  - 对外 agent tool 不暴露 metric-topic；这里保留人工 shortcut。
  - 字段较多时，用 --print-request-template=full + --request file://req.json 组织完整 JSON。

Commands:
  list     List metric topics
  get      Get a metric topic by id
  create   Create a metric topic
  modify   Modify a metric topic
  delete   Delete a metric topic
  search   Query metric data via /SearchLogs (SQL/PromQL/PromQL+SQL in Query)

场景速选:
  - 列指标主题: volclog metric-topic list --describe
  - PromQL/指标查询: volclog metric-topic search --describe
  - 创建或修改指标主题: volclog metric-topic create --describe / volclog metric-topic modify --describe

Notes:
  - TopicName and TopicId cannot be provided together for list.
  - --all 会自动翻完分页；不要与 --page-number 混用。
  - Complex request bodies can be passed via --request file://...
  - --request also accepts "-" to read JSON from stdin.

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageMetricTopicProm() string {
	return u(`Usage:
  tlsctl metric-topic prom <subcommand> [args]

Subcommands:
  query         /topic/{topic_id}/api/v1/query
  query-range   /topic/{topic_id}/api/v1/query_range
  series        /topic/{topic_id}/api/v1/series
  labels        /topic/{topic_id}/api/v1/labels
  label-values  /topic/{topic_id}/api/v1/label/{label_name}/values

Notes:
  - --method GET|POST (default GET)
  - Many args accept file://..., including --query, --time, --start, --end, --match, --label-name
  - --match file://... supports JSON array of strings or newline-delimited strings

Examples:
  tlsctl metric-topic prom query --topic-id <tid> --query 'up' --time '2026-03-14T00:00:00Z'
  tlsctl metric-topic prom query-range --topic-id <tid> --query 'rate(up[5m])' --start 1710374400000 --end 1710378000000 --step 15
  tlsctl metric-topic prom series --topic-id <tid> --start 1710374400000 --end 1710378000000 --match 'up'
  tlsctl metric-topic prom labels --topic-id <tid> --match file://./match.txt
  tlsctl metric-topic prom label-values --topic-id <tid> --label-name job --match 'up'

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageIndex() string {
	return u(`Usage:
  tlsctl index <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 index 操作时可直接使用。
  - Agent 默认不要停在 shortcut 元命令；先转到 tlsctl tool list/describe index。
  - 当前 shortcut 仍支持 --describe；只有明确要走人工 shortcut 时再看 --print-request-template=full。
  - 需要公开 API 契约时，转到 tlsctl tool list index / tlsctl tool describe index.<action>。
  - 字段较多时，用 --print-request-template=full + --request file://req.json 组织完整 JSON。

Commands:
  get      Get index by topic id
  create   Create index with JSON body
  modify   Modify index with JSON body

场景速选:
  - 看当前索引: volclog index get --topic-id <tid>
  - 创建或修改索引: volclog index create --describe / volclog index modify --describe
  - 不确定 body 怎么写: volclog index create --print-request-template=full

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageLog() string {
	return u(`Usage:
  tlsctl log <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 log 操作时可直接使用。
  - Agent 默认先用 tlsctl tool/workflow describe；只有用户明确要 shortcut flags 或模板时再留在这里。
  - 当前 shortcut 仍支持 --describe；复杂写入体只有明确要走人工 shortcut 时再看 --print-request-template=full。
  - 需要公开 API 契约时，转到 tlsctl tool list log / tlsctl tool describe log.<action>。
  - CLI workflow 约束与执行请看 tlsctl workflow describe/exec log.<command>。

Commands:
  search   Search logs via /SearchLogs
  histogram Query histogram buckets for a log query
  context  Fetch surrounding logs for one hit
  put      Write logs via PutLogs special IO
  ingest   Batch-ingest text/JSON logs and let CLI assemble PutLogs requests
  export   Auto-page export logs (stdout JSON array; use --output jsonl for streaming)
  export-analysis  Export SQL/analysis rows as JSONL (one row object per line by default)

场景速选:
  - 普通日志检索: volclog log search --describe
  - 看命中日志上下文: volclog log context --describe
  - 看查询直方图: volclog log histogram --describe
  - 写日志/WebTracking: volclog log put --describe
  - 批量导入文本或 JSON 日志: volclog workflow describe log.ingest
  - 大量原始日志导出: volclog workflow describe log.export
  - SQL/聚合/分析导出: volclog workflow describe log.export-analysis

Notes:
  - SearchLogs requires X-Tls-Apiversion=0.3.0 (handled by client).
  - Full request body can be passed via --request file://...
  - --request also accepts "-" to read JSON from stdin.
  - log search 默认返回 100 条样本；log export 默认每批拉取 500 条，可用 --limit 覆盖。
  - log ingest 默认按 500 条一批发送，默认压缩为 lz4。
  - lines 输入会把每行文本写入 __content__；jsonl/json-array 会保留用户原始字段。
  - log ingest 未指定 --time-field 时，会用本次命令启动时的毫秒时间戳补齐日志时间。
  - Prefer --output jsonl log export for streaming.
  - For pure search (no analysis), you can paginate via Context/Limit/Sort/Offset.
  - For analysis (query contains "|"), Context/Sort/Limit/Offset in body are not effective; use SQL limit/offset in Query. Analysis does not support Context pagination.
  - Analysis/export-analysis column availability depends on current index config and usually applies incrementally; old logs may still show null for newly indexed fields.

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageHostGroup() string {
	return u(`Usage:
  tlsctl host-group <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 host-group 操作时可直接使用。
  - Agent 默认不要停在 shortcut 元命令；先转到 tlsctl tool list/describe host-group。
  - 当前 shortcut 仍支持 --describe；只有明确要走人工 shortcut 时再看 --print-request-template=full。
  - 需要公开 API 契约时，转到 tlsctl tool list host-group / tlsctl tool describe host-group.<action>。
  - 字段较多时，用 --print-request-template=full + --request file://req.json 组织完整 JSON。

Commands:
  list     List host groups
  get      Get a host group by id
  create   Create a host group
  modify   Modify a host group
  delete   Delete a host group

场景速选:
  - 列机器组/拿 HostGroupId: volclog host-group list --describe
  - 看单机器组详情: volclog host-group get --describe
  - 创建或修改机器组: volclog host-group create --describe / volclog host-group modify --describe

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageCollector() string {
	return u(`Usage:
  tlsctl collector <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 collector 操作时可直接使用。
  - Agent 默认不要停在 shortcut 元命令；先转到 tlsctl tool list/describe collector。
  - 当前 shortcut 仍支持 --describe；只有明确要走人工 shortcut 时再看 --print-request-template=full。
  - 需要公开 API 契约时，转到 tlsctl tool list collector / tlsctl tool describe collector.<action>。
  - 字段较多时，用 --print-request-template=full + --request file://req.json 组织完整 JSON。

Commands:
  list     List collector rules
  get      Get a collector rule by id
  create   Create a collector rule
  modify   Modify a collector rule
  delete   Delete a collector rule

场景速选:
  - 列采集规则/拿 RuleId: volclog collector list --describe
  - 看单规则详情: volclog collector get --describe
  - 创建或修改规则: volclog collector create --describe / volclog collector modify --describe

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageAssistant() string {
	return u(`Usage:
  tlsctl assistant <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 assistant 操作时可直接使用。
  - Agent 不要把这里当主流程；对外公开 CLI 不暴露 assistant tool catalog。
  - 当前 shortcut 仍支持 --describe。
  - 对外公开 CLI 不暴露 assistant tool catalog；这里只保留人工 shortcut。

场景速选:
  - 会话回答/answer detail: volclog assistant describe-session-answer --describe
  - 对外公开 CLI 不暴露 assistant tool catalog；不要把 assistant 当成公开 tool 分组

Commands:
  describe-session-answer   Ask AI Assistant for a topic

Notes:
  - If --instance-id is not provided, TLS_AI_ASSISTANT_INSTANCE_ID will be used.
  - If instance id is missing, --account-id (or LOG_SERVICE_ACCOUNT_ID) is required to find/create one.

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure
`)
}

func usageAssistantDescribeSessionAnswer() string {
	return u(`Usage:
  tlsctl assistant describe-session-answer [args]

Flags:
  --topic-id <id>        (required)
  --question <text|file> (required, supports file://... or "-")
  --instance-id <id>     (optional; fallback to env TLS_AI_ASSISTANT_INSTANCE_ID)
  --account-id <id>      (optional; used to find/create instance; fallback to env LOG_SERVICE_ACCOUNT_ID)
  --intent <name>        (optional; default Text2Tls)
  -h, --help

Examples:
  tlsctl assistant describe-session-answer --topic-id <tid> --question 'What happened?'
  tlsctl assistant describe-session-answer --topic-id <tid> --question file://./q.txt --account-id <account>
`)
}
