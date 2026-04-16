package cli

import "strings"

func u(s string) string {
	s = strings.ReplaceAll(s, ".tlsctl", ".volclog")
	s = strings.ReplaceAll(s, "TLSCTL_", "VOLCLOG_")
	s = strings.ReplaceAll(s, "tlsctl", "volclog")
	return s
}

func usageConfigure() string {
	return u(`Usage:
  tlsctl configure <command> [args]

Commands:
  set     Set a profile
  project Manage project defaults (show/set)
  profile Alias commands: add/use/show/list/delete
  cred    Manage shared credentials (delete)
  use     Set default profile
  show    Show a profile
  list    List profiles
  delete  Delete a profile (or batch delete by prefix)

Examples:
  tlsctl configure set --profile default --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile tenant-a-sg --ak <ak> --sk <sk> --endpoint https://tls-ap-singapore-1.volces.com
  tlsctl configure set --profile abc-bj --cred-ref ma-abc-root --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile abc-sg --cred-ref ma-abc-root --endpoint https://tls-ap-singapore-1.volces.com
  tlsctl configure profile add tenant-a --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure profile use tenant-a
  tlsctl configure use default
  tlsctl configure show --profile default
  tlsctl configure project show
  tlsctl configure project set --output json --output-mode file --output-dir ./out
  tlsctl --profile tenant-a-sg project list
  tlsctl configure list
  tlsctl configure delete tenant-a-sg
  tlsctl configure delete --prefix tenant-a --yes
  tlsctl configure cred delete ma-abc-root

Exit Code:
  0 success
  1 usage / invalid args

Agent:
  - Use tlsctl doctor to verify config before running requests
  - Prefer env or --secrets-file in CI to avoid writing secrets to disk
`)
}

func usageSkill() string {
	return u(`Usage:
  tlsctl skill <command> [args]

Commands:
  list      List bundled skills available in this volclog build
  install   Install bundled skills into a user-provided agent skills directory

Notes:
  - --dir is required for install and should point to the target agent's skills directory
  - If --name is omitted, install copies all bundled skills
  - Use --force to overwrite an existing installed skill directory
  - This command installs from the CLI's bundled skills; it does not require the source repo checkout

Examples:
  tlsctl skill list
  tlsctl skill install --dir /path/to/agent/skills
  tlsctl skill install --dir /path/to/agent/skills --name volclog-core
  tlsctl skill install --dir /path/to/agent/skills --force

Exit Code:
  0 success
  1 usage / invalid args
 2 runtime failure
`)
}

func usageAPI() string {
	return u(`Usage:
  tlsctl api <legacy surface removed>

Notes:
  - This legacy surface is no longer routed from the main CLI entry.
  - Use tlsctl tool ... / tlsctl raw ... instead.
`)
}

func usageAPICall() string {
	return u(`Usage:
  tlsctl api call <legacy surface removed>

Notes:
  - This legacy surface is no longer routed from the main CLI entry.
  - Use tlsctl raw --method <METHOD> --path <PATH> instead.
`)
}

func usageRaw() string {
	return u(`Usage:
  tlsctl raw --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...|->] [--request-format <json|jsonl>]

概览:
  原始 transport 调用入口；需要显式提供 method/path。
  --jmes-filter 作用于原始 API 结果，而不是 CLI envelope。

关键参数:
  --method <GET|POST|PUT|DELETE>
  --path <path>
  --query k=v
  --header k=v
  --body <json|file://...|->
  --request-format <json|jsonl>

调用方式:
  - path 必须是以 / 开头的 OpenAPI 路径
  - body 支持 inline JSON、file://...、-、裸文件路径

Examples:
  tlsctl raw --method GET --path /DescribeProjects
  tlsctl raw --method POST --path /CreateProject --body file://./req.json
  tlsctl raw --method GET --path /DescribeProjects --jmes-filter "Total"
`)
}

func usageTool() string {
	return u(`Usage:
  tlsctl tool <command> [args]

概览:
  用统一 tool 契约面做发现、筛选与契约查看；执行能力请使用 tool exec。
  仅公开官网文档已发布接口；未公开接口不属于对外 CLI 契约面。

Commands:
  list      List groups or actions
  describe  Show a tool contract and execution hint
  exec      Execute a tool contract with JSON context/input

Use:
  volclog tool list
  volclog tool list <group> [--verb <verb>] [--format <text|json>]
  volclog tool describe <group.action>
  volclog tool exec <group.action> --context file://ctx.json [--input file://req.json]

说明:
  list 默认返回 group 摘要；指定 <group> 后返回 action 列表。

Exit Code:
  0 success
  1 usage / invalid args

Agent:
  - describe 结果是机器可读契约，包含输入/上下文/执行约束
  - shortcut 仍是人工入口，不属于 tool 默认流程
`)
}

func usageWorkflow() string {
	return u(`Usage:
  tlsctl workflow <command> [args]

概览:
  CLI workflow 契约面，暴露少量高价值高层编排。
  这些能力不是官网公开 OpenAPI tool；tool 仍只暴露官网公开 API。

Commands:
  list      List workflow groups or workflow ids
  describe  Show a workflow contract and execution hint
  exec      Execute a workflow with JSON context/input
`)
}

func usageWorkflowList() string {
	return u(`Usage:
  tlsctl workflow list
  tlsctl workflow list [<group>] [--format <text|json>]

说明:
  - workflow 面只暴露 CLI workflow，不混入 public tool
  - 当前首批 workflow: log.ingest / log.export / log.export-analysis
  - tool 仍只暴露官网公开 API

Next:
  volclog workflow describe <group.command>
  volclog workflow exec <group.command> --input file://req.json

Filters:
  --format <text|json>
`)
}

func usageWorkflowDescribe() string {
	return u(`Usage:
  tlsctl workflow describe <group.command>

说明:
  - describe 返回 CLI workflow contract，而不是 public OpenAPI contract
  - 需要原子 API 契约时，回到 volclog tool describe <group.action>
  - workflow exec 使用 JSON input/context，不要求 agent 学 flags
`)
}

func usageWorkflowExec() string {
	return u(`Usage:
  tlsctl workflow exec <group.command> [--context file://ctx.json|-|'<inline-json>'] --input file://req.json|-|'<inline-json>'

Notes:
  - --context 可省略；省略时默认使用空对象 {}
  - --input 支持 file://...、-、inline JSON object
  - execution 默认从 ctx.json 的 execution 字段读取
  - execution.projection / execution.artifact / execution.dry_run 语义与 tool exec 一致
  - 大结果 workflow 建议配合 --output-mode file 或 execution.artifact
`)
}

func usageToolList() string {
	return u(`Usage:
  tlsctl tool list
  tlsctl tool list [<group>] [--verb <verb>] [--format <text|json>]

说明:
  - 默认返回 group 摘要
  - 指定 <group> 后返回该 group 下可执行的 action identity
  - 仅列出官网文档已发布接口
  - 只做发现与筛选，不执行请求

支持的发现方式:
  - 按 group 看有哪些 action: tlsctl tool list <group>
  - 按 verb 缩小范围: tlsctl tool list <group> --verb <verb>

常见 verb:
  create / get / list / describe / modify / delete / search

Next:
  tlsctl tool describe <group.action>
  tlsctl tool exec <group.action> [--context file://ctx.json] [--input file://req.json|-]

Filters:
  --verb <verb>
  --format <text|json>
`)
}

func usageToolDescribe() string {
	return u(`Usage:
  tlsctl tool describe <group.action> [--view <compact|full>]

说明:
  - 默认返回 compact 视图，优先保留最小可执行契约
  - 指定 --view full 或显式 --output json 时返回完整机器契约
  - 只展示官网文档已发布接口的契约
  - 只做契约查看，不执行请求
  - 通常与 volclog tool list <group> 配合使用
`)
}

func usageToolExec() string {
	return u(`Usage:
  tlsctl tool exec <group.action> [--context file://ctx.json|-|'<inline-json>'] [--input file://req.json|-|'<inline-json>'] [--page-all]

Notes:
  - 先根据 tool describe 准备 context/input 文件
  - --context 可省略；省略时默认使用空对象 {}
  - --context 和 --input 都支持 file://...、-、inline JSON object
  - 当 tool describe 的 input_schema 为空时，可省略 --input
  - tool exec 既支持显式嵌套的 {query,path,header,body}，也支持扁平 JSON；当字段能唯一映射到某个 section 时会自动归位
  - --page-all 是 execution.page.all 的 CLI 入口（等价于 execution.page.all=true）
  - 未显式指定 --output-mode stdout/file 且 stdout 结果过大时，tool exec 会自动把全量结果写入 artifact，并仅返回摘要预览
  - execution 默认从 ctx.json 的 execution 字段读取
  - execution.projection 支持 "expr"、["expr"]、{"jmes":"expr"}
  - execution.artifact 支持 true、"/tmp/out.json"、{"path":"/tmp/out.json"}
  - execution.page.all 只在 tool describe 返回 execution.supports_all=true 时可用；它提高完整性，可能增加 payload 大小
  - context.trace 支持 true、"/tmp/traces"、{"dir":"/tmp/traces","redact":"strict"}
`)
}

func usageProject() string {
	return u(`Usage:
  tlsctl project <command> [args]

Human Shortcut:
  - 面向人工交互的高频命令；已明确要做 project 操作时可直接使用。
  - 当前 shortcut 仍支持 --describe；字段复杂时先看 --print-request-template=full。
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
  - 当前 shortcut 仍支持 --describe；字段复杂时先看 --print-request-template=full。
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
  - 当前 shortcut 仍支持 --describe；字段复杂时先看 --print-request-template=full。
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
  - 当前 shortcut 仍支持 --describe；字段复杂时先看 --print-request-template=full。
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
  - 当前 shortcut 仍支持 --describe；复杂写入体先看 --print-request-template=full。
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
  - 当前 shortcut 仍支持 --describe；字段复杂时先看 --print-request-template=full。
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
  - 当前 shortcut 仍支持 --describe；字段复杂时先看 --print-request-template=full。
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

func usageDoctor() string {
	return u(`Usage:
  tlsctl doctor [--online]

Flags:
  --online   Run minimal online checks (optional)

Examples:
  tlsctl doctor
  tlsctl doctor --online

Output:
  - time.local_unix_ms, time.server_unix_ms, time.skew_seconds, time.skew_risk

Exit Code:
  0 success
  2 missing required config/credentials

Agent:
  - Use doctor output to decide whether to proceed or reconfigure
`)
}
