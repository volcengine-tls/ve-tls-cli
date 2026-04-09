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
  tlsctl skill install --dir /path/to/agent/skills --name volclog-shared --name volclog-log
  tlsctl skill install --dir /path/to/agent/skills --force

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure
`)
}

func usageAPI() string {
	return u(`Usage:
  tlsctl api <group>
  tlsctl api <group> <action> [flags]
  tlsctl api call --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...|->] [--request-format <json|jsonl>]

概览:
  用于执行 OpenAPI；优先使用 api <group> <action>。
  只有在已明确 method/path 时才使用 api call。
  全局参数必须写在 api 前；大输出优先使用 --output-mode file。
  对 api 可放宽：输出类全局参数也可写在 action 后。
  --jmes-filter 作用于原始 API 结果，而不是 CLI envelope。

推荐流程:
  1. 先发现 action：        tlsctl capabilities --view groups
  2. 查看调用约束：         tlsctl api <group> <action> --describe
  3. 有 body 时先生成模板： tlsctl api <group> <action> --print-request-template=full
  4. 执行前先 dry-run：     tlsctl --dry-run api <group> <action> --request file://req.json
  5. 若是复数 Describe 列举接口，可直接加 --all 自动翻页

调用方式:
  - query/path 参数通过 flags 传入
  - body 通过 --request 传入
  - --request 支持 inline JSON、file://...、-、裸文件路径

关键参数:
  --request <json|file://...|->
  --print-request-template[=required|full]
  --describe
  --request-format <json|jsonl>
  --query k=v
  --header k=v

过滤与引号:
  --jmes-filter 筛选原始 API 返回；例如取 Total 写 Total，不写 data.Total
  zsh/bash:      --jmes-filter "keys(@)"
  fish/PowerShell: --jmes-filter 'keys(@)'

Examples:
  tlsctl api project
  tlsctl api log SearchLogs --describe
  tlsctl api project DescribeProjects --all
  tlsctl api log SearchLogs --print-request-template=full
  tlsctl --dry-run api log SearchLogs --request file://./req.json
  tlsctl api call -h
`)
}

func usageAPICall() string {
	return u(`Usage:
  tlsctl api call --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...|->] [--request-format <json|jsonl>]

概览:
  底层直调入口；仅在已明确 method/path 时使用。
  如果你还不确定接口，先回到 capabilities / api <group> <action>。
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
  tlsctl api call --method GET --path /DescribeProjects
  tlsctl api call --method POST --path /CreateProject --body file://./req.json
  tlsctl api call --method GET --path /DescribeProjects --jmes-filter "Total"
`)
}

func usageCapabilities() string {
	return u(`Usage:
  tlsctl capabilities [--group <group>] [--action <action>] [--view <json|compact|full|text|groups>] [--hints-file <path>]

概览:
  用于发现 group 与 action；不执行请求。
  先选 group，再选 action，最后转到 api --describe。

推荐流程:
  1. 粗分类: tlsctl capabilities --view groups
  2. 看全量 group + action: tlsctl capabilities --view text
  3. 看组内动作: tlsctl capabilities --group <group> --view text
  4. 查动作细节: tlsctl capabilities --group <group> --action <action> --view full
  5. 执行前确认: tlsctl api <group> <action> --describe

视图说明:
  - groups: group 一行概览，并给出 agent entry
  - text: group + action + 描述，并给出每个 action 的下一条可执行命令
  - json: compact 的机器可读 JSON（compact 别名）
  - compact: 单 action 简明语义
  - full: 完整约束

Examples:
  tlsctl capabilities --view groups
  tlsctl capabilities --view text
  tlsctl capabilities --group log --view text
  tlsctl capabilities --action CreateProject
  tlsctl capabilities --group log --action SearchLogs
  tlsctl capabilities --group log --action SearchLogs --view full
`)
}

func usageProject() string {
	return u(`Usage:
  tlsctl project <command> [args]

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl project create --describe
  - Generate JSON template when needed: tlsctl project create --print-request-template=full
  - Fall back to capabilities/api when the shortcut does not cover the need.

Commands:
  list     List projects
  get      Get a project by id
  create   Create a project
  modify   Modify a project
  delete   Delete a project

场景速选:
  - 列项目/拿 ProjectId: volclog project list --describe
  - 模糊找项目: volclog project list --fuzzy-search-key <keyword>
  - 看单项目详情: volclog project get --describe
  - 创建或修改项目: volclog project create --describe / volclog project modify --describe

Examples:
  tlsctl project list --page-size 20 --project-name test
  tlsctl project list --all
  tlsctl project get --project-id <pid>
  tlsctl project create --describe
  tlsctl project create --print-request-template=full
  tlsctl project create --project-name demo --description test
  tlsctl project modify --project-id <pid> --description updated --favourite
  tlsctl project delete --project-id <pid>
  tlsctl --output table project list
  tlsctl --output-mode file project list
  tlsctl --trace-dir ./.tlsctl/traces project list

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure

Agent:
  - Typical flow: tlsctl project create --describe -> tlsctl project create --print-request-template=full -> tlsctl project create --request file://req.json
`)
}

func usageTopic() string {
	return u(`Usage:
  tlsctl topic <command> [args]

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl topic create --describe
  - Generate JSON template when needed: tlsctl topic create --print-request-template=full
  - Fall back to capabilities/api when the shortcut does not cover the need.

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

Examples:
  tlsctl topic list --project-id <pid> --page-size 20
  tlsctl topic list --project-id <pid> --all
  tlsctl topic get --topic-id <tid>
  tlsctl topic create --describe
  tlsctl topic create --print-request-template=full
  tlsctl topic create --project-id <pid> --topic-name demo-topic --ttl 30 --shard-count 2 --auto-split --max-split-shard 10
  tlsctl topic modify --topic-id <tid> --description updated --ttl 60
  tlsctl topic delete --topic-id <tid>
  tlsctl topic create --request file://./create_topic.json
  cat ./create_topic.json | tlsctl topic create --request -
  tlsctl --output table topic list --project-id <pid>
  tlsctl --output-mode file topic list --project-id <pid>
  tlsctl --trace-dir ./.tlsctl/traces topic list --project-id <pid>

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure

Agent:
  - Use --describe plus --print-request-template before composing large request bodies
`)
}

func usageMetricTopic() string {
	return u(`Usage:
  tlsctl metric-topic <command> [args]

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl metric-topic create --describe
  - Generate JSON template when needed: tlsctl metric-topic create --print-request-template=full
  - Fall back to capabilities/api when the shortcut does not cover the need.

Commands:
  list     List metric topics
  get      Get a metric topic by id
  create   Create a metric topic
  modify   Modify a metric topic
  delete   Delete a metric topic
  search   Query metric data via /SearchLogs (SQL/PromQL/PromQL+SQL in Query)
  prom     Prometheus HTTP API compatible calls

场景速选:
  - 列指标主题: volclog metric-topic list --describe
  - PromQL/指标查询: volclog metric-topic search --describe
  - 创建或修改指标主题: volclog metric-topic create --describe / volclog metric-topic modify --describe

Notes:
  - TopicName and TopicId cannot be provided together for list.
  - --all 会自动翻完分页；不要与 --page-number 混用。
  - Complex request bodies can be passed via --request file://...
  - Prom endpoints support GET and POST (application/x-www-form-urlencoded).
  - --request also accepts "-" to read JSON from stdin.

Examples:
  tlsctl metric-topic list --project-id <pid> --page-size 20
  tlsctl metric-topic list --project-id <pid> --all
  tlsctl metric-topic get --topic-id <metric_tid>
  tlsctl metric-topic create --describe
  tlsctl metric-topic create --print-request-template=full
  tlsctl metric-topic create --project-id <pid> --topic-name demo-metric --ttl 30 --shard-count 2
  tlsctl --output table metric-topic list --project-id <pid>
  tlsctl metric-topic modify --topic-id <metric_tid> --description updated --ttl 60
  tlsctl metric-topic delete --topic-id <metric_tid>
  tlsctl metric-topic search --topic-id <metric_tid> --query 'avg(rate(http_requests_total[5m]))' --from 1710374400000 --to 1710378000000
  tlsctl metric-topic prom --help
  tlsctl --output-mode file metric-topic search --topic-id <metric_tid> --query 'up' --from 1710374400000 --to 1710378000000
  tlsctl --trace-dir ./.tlsctl/traces metric-topic search --topic-id <metric_tid> --query 'up' --from 1710374400000 --to 1710378000000

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure

Agent:
  - Prefer --describe plus --print-request-template before composing large request bodies
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

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl index create --describe
  - Generate JSON template first: tlsctl index create --print-request-template=full
  - Fall back to capabilities/api when the shortcut does not cover the need.

Commands:
  get      Get index by topic id
  create   Create index with JSON body
  modify   Modify index with JSON body

场景速选:
  - 看当前索引: volclog index get --topic-id <tid>
  - 创建或修改索引: volclog index create --describe / volclog index modify --describe
  - 不确定 body 怎么写: volclog index create --print-request-template=full

Examples:
  tlsctl index get --topic-id <tid>
  tlsctl index create --describe
  tlsctl index create --print-request-template=full
  tlsctl index create --topic-id <tid> --request file://./index.json
  tlsctl index modify --topic-id <tid> --request file://./index.json
  cat ./index.json | tlsctl index create --topic-id <tid> --request -
  tlsctl --trace-dir ./.tlsctl/traces index get --topic-id <tid>

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

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl log search --describe
  - Generate JSON template when needed: tlsctl log search --print-request-template=full
  - Fall back to capabilities/api when the shortcut does not cover the need.

Commands:
  search   Search logs via /SearchLogs
  histogram Query histogram buckets for a log query
  context  Fetch surrounding logs for one hit
  put      Write logs via PutLogs special IO
  export   Auto-page export logs (stdout JSON array; use --output jsonl for streaming)
  export-analysis  Export SQL/analysis rows as JSONL (one row object per line by default)

场景速选:
  - 普通日志检索: volclog log search --describe
  - 看命中日志上下文: volclog log context --describe
  - 看查询直方图: volclog log histogram --describe
  - 写日志/WebTracking: volclog log put --describe
  - 大量原始日志导出: volclog --output-mode file log export --describe
  - SQL/聚合/分析导出: volclog --output-mode file log export-analysis --describe

Notes:
  - SearchLogs requires X-Tls-Apiversion=0.3.0 (handled by client).
  - Full request body can be passed via --request file://...
  - --request also accepts "-" to read JSON from stdin.
  - log search 默认返回 100 条样本；log export 默认每批拉取 500 条，可用 --limit 覆盖。
  - Prefer --output jsonl log export for streaming.
  - For pure search (no analysis), you can paginate via Context/Limit/Sort/Offset.
  - For analysis (query contains "|"), Context/Sort/Limit/Offset in body are not effective; use SQL limit/offset in Query. Analysis does not support Context pagination.
  - Analysis/export-analysis column availability depends on current index config and usually applies incrementally; old logs may still show null for newly indexed fields.

Examples:
  tlsctl log search --describe
  tlsctl log search --print-request-template=full
  tlsctl log histogram --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --interval 60
  tlsctl log context --topic-id <tid> --context-flow <flow> --package-offset 66 --source 127.0.0.1 --prev-logs 20 --next-logs 20
  tlsctl log put --describe
  tlsctl log put --print-request-template=full
  tlsctl log put --topic-id <tid> --request file://./put_logs.json
  tlsctl log search --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --limit 100 --sort desc
  tlsctl log search --request file://./search_logs.json
  tlsctl --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 10
  tlsctl log export-analysis --topic-id <tid> --query "*|select count(*) as cnt group by __time__ limit 100" --from 1710374400000 --to 1710378000000
  tlsctl --output-mode file log search --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000
  tlsctl --trace-dir ./.tlsctl/traces log search --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure

Agent:
  - Prefer --output-mode file for large search results
  - Use --trace-dir to generate trace artifacts for debugging
  - For export, prefer JSONL streaming: tlsctl --output jsonl log export ...
`)
}

func usageHostGroup() string {
	return u(`Usage:
  tlsctl host-group <command> [args]

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl host-group create --describe
  - Generate JSON template when needed: tlsctl host-group create --print-request-template=full
  - Fall back to capabilities/api when the shortcut does not cover the need.

Commands:
  list     List host groups
  get      Get a host group by id
  bind-rules   Bind a host group to rules
  unbind-rules Unbind a host group from rules
  delete-host  Delete a host from a host group
  create   Create a host group
  modify   Modify a host group
  delete   Delete a host group

场景速选:
  - 列机器组/拿 HostGroupId: volclog host-group list --describe
  - 看单机器组详情: volclog host-group get --describe
  - 绑定/解绑规则: volclog host-group bind-rules --describe / volclog host-group unbind-rules --describe
  - 删除机器组里的主机: volclog host-group delete-host --describe
  - 创建或修改机器组: volclog host-group create --describe / volclog host-group modify --describe

Examples:
  tlsctl host-group list --all
  tlsctl host-group get --host-group-id <hid>
  tlsctl host-group bind-rules --describe
  tlsctl host-group bind-rules --host-group-id <hid> --rule-ids '["rid-1","rid-2"]'
  tlsctl host-group unbind-rules --host-group-id <hid> --rule-ids file://./rule_ids.json
  tlsctl host-group delete-host --host-group-id <hid> --ip 1.1.1.1
  tlsctl host-group create --describe
  tlsctl host-group create --print-request-template=full
  tlsctl host-group create --host-group-name demo --host-group-type Label --host-identifier app-prod
  tlsctl host-group modify --host-group-id <hid> --host-group-name demo-v2 --service-logging
  tlsctl host-group delete --host-group-id <hid>

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

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl collector create --describe
  - Generate JSON template when needed: tlsctl collector create --print-request-template=full
  - Fall back to capabilities/api when the shortcut does not cover the need.

Commands:
  list     List collector rules
  get      Get a collector rule by id
  bind-host-groups   Bind a rule to host groups
  unbind-host-groups Unbind a rule from host groups
  create   Create a collector rule
  modify   Modify a collector rule
  delete   Delete a collector rule

场景速选:
  - 列采集规则/拿 RuleId: volclog collector list --describe
  - 看单规则详情: volclog collector get --describe
  - 绑定/解绑机器组: volclog collector bind-host-groups --describe / volclog collector unbind-host-groups --describe
  - 创建或修改规则: volclog collector create --describe / volclog collector modify --describe

Examples:
  tlsctl collector list --project-id <pid> --all
  tlsctl collector get --rule-id <rid>
  tlsctl collector bind-host-groups --describe
  tlsctl collector bind-host-groups --rule-id <rid> --host-group-ids '["hid-1","hid-2"]'
  tlsctl collector unbind-host-groups --rule-id <rid> --host-group-ids file://./host_group_ids.json
  tlsctl collector create --describe
  tlsctl collector create --print-request-template=full
  tlsctl collector create --topic-id <tid> --rule-name demo-rule --paths file:///var/log/app.log
  tlsctl collector modify --rule-id <rid> --request file://./rule.json
  tlsctl collector delete --rule-id <rid>

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

Agent First:
  - High-frequency shortcut for both agents and humans.
  - Inspect shortcut constraints first: tlsctl assistant describe-session-answer --describe
  - Fall back to capabilities/api when the shortcut does not cover the need.

场景速选:
  - 会话回答/answer detail: volclog assistant describe-session-answer --describe
  - 实例管理或更底层接口: volclog capabilities --group assistant --view text

Commands:
  describe-session-answer   Ask AI Assistant for a topic

Notes:
  - If --instance-id is not provided, TLS_AI_ASSISTANT_INSTANCE_ID will be used.
  - If instance id is missing, --account-id (or LOG_SERVICE_ACCOUNT_ID) is required to find/create one.

Examples:
  tlsctl assistant describe-session-answer --describe
  tlsctl assistant describe-session-answer --topic-id <tid> --question 'What happened?'
  TLS_AI_ASSISTANT_INSTANCE_ID=<id> tlsctl assistant describe-session-answer --topic-id <tid> --question file://./q.txt
  LOG_SERVICE_ACCOUNT_ID=<account> tlsctl assistant describe-session-answer --topic-id <tid> --question '...'

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
