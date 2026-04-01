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

func usageAPI() string {
	return u(`Usage:
  tlsctl api <group>
  tlsctl api <group> <action> [flags]
  tlsctl api call --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...|->] [--request-format <json|jsonl>]

Key Flags (generated action):
  --request <json|file://...|->
  --request-format <json|jsonl>
  --print-request-template[=required|full]
  --describe
  --query k=v
  --header k=v

Examples:
  tlsctl api log
  tlsctl api log SearchLogs -h
  tlsctl api call -h
  tlsctl api log SearchLogs --describe
  tlsctl api log SearchLogs --print-request-template=full
  tlsctl --dry-run api log SearchLogs --request file://./req.json
  tlsctl api log SearchLogs --request file://./req.json

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure

Agent:
  - Discover: tlsctl capabilities --group <group> --action <action>
  - Constraints: tlsctl api <group> <action> --describe
  - Validate first: tlsctl --dry-run api <group> <action> --request file://req.json
`)
}

func usageCapabilities() string {
	return u(`Usage:
  tlsctl capabilities [--group <group>] [--action <action>] [--view <compact|full|text>] [--hints-file <path>]

Description:
  Output API capability contract.
  Includes metadata: contract_version/param_doc_source/supports_dry_run/output_mode_hint.
  Includes declarative hints: hints_mode/risk_level/idempotency (advisory only).
  view=compact (default) hides verbose params/request_params_doc for token saving.
  view=full returns complete parameter constraints and official doc intros.
  view=text returns human-friendly command list text.
  Hints file resolution when --hints-file is omitted: VOLCLOG_HINTS_FILE > project .volclog/cli.config.json hints_file.

Examples:
  tlsctl capabilities
  tlsctl capabilities --group log
  tlsctl capabilities --group log --action SearchLogs
  tlsctl capabilities --group log --action SearchLogs --view full
  tlsctl capabilities --view text
  tlsctl capabilities --action create
  tlsctl capabilities --hints-file ./docs/agentic-stage1/capability-hints-overrides.example.json
`)
}

func usageProject() string {
	return u(`Usage:
  tlsctl project <command> [args]

Commands:
  list     List projects
  get      Get a project by id
  create   Create a project
  modify   Modify a project
  delete   Delete a project

Examples:
  tlsctl project list --page-size 20 --project-name test
  tlsctl project get --project-id <pid>
  tlsctl project create --project-name demo --description test
  tlsctl project modify --project-id <pid> --description updated --favourite
  tlsctl project delete --project-id <pid>
  tlsctl --output-mode file project list
  tlsctl --trace-dir ./.tlsctl/traces project list

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure

Agent:
  - Typical flow: tlsctl doctor -> tlsctl --output-mode file project list
`)
}

func usageTopic() string {
	return u(`Usage:
  tlsctl topic <command> [args]

Commands:
  list     List topics
  get      Get a topic by id
  create   Create a topic
  modify   Modify a topic
  delete   Delete a topic

Notes:
  - TopicName and TopicId cannot be provided together for list.
  - Complex request bodies can be passed via --request file://...
  - --request also accepts "-" to read JSON from stdin.

Examples:
  tlsctl topic list --project-id <pid> --page-size 20
  tlsctl topic get --topic-id <tid>
  tlsctl topic create --project-id <pid> --topic-name demo-topic --ttl 30 --shard-count 2 --auto-split --max-split-shard 10
  tlsctl topic modify --topic-id <tid> --description updated --ttl 60
  tlsctl topic delete --topic-id <tid>
  tlsctl topic create --request file://./create_topic.json
  cat ./create_topic.json | tlsctl topic create --request -
  tlsctl --output-mode file topic list --project-id <pid>
  tlsctl --trace-dir ./.tlsctl/traces topic list --project-id <pid>

Exit Code:
  0 success
  1 usage / invalid args
  2 request/runtime failure
  3 output/decode failure

Agent:
  - Use --request - to pipe JSON from other tools (jq, templates)
`)
}

func usageMetricTopic() string {
	return u(`Usage:
  tlsctl metric-topic <command> [args]

Commands:
  list     List metric topics
  get      Get a metric topic by id
  create   Create a metric topic
  modify   Modify a metric topic
  delete   Delete a metric topic
  search   Query metric data via /SearchLogs (SQL/PromQL/PromQL+SQL in Query)
  prom     Prometheus HTTP API compatible calls

Notes:
  - TopicName and TopicId cannot be provided together for list.
  - Complex request bodies can be passed via --request file://...
  - Prom endpoints support GET and POST (application/x-www-form-urlencoded).
  - --request also accepts "-" to read JSON from stdin.

Examples:
  tlsctl metric-topic list --project-id <pid> --page-size 20
  tlsctl metric-topic get --topic-id <metric_tid>
  tlsctl metric-topic create --project-id <pid> --topic-name demo-metric --ttl 30 --shard-count 2
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
  - Prefer --output-mode file for query results
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

Commands:
  get      Get index by topic id
  create   Create index with JSON body
  modify   Modify index with JSON body

Examples:
  tlsctl index get --topic-id <tid>
  tlsctl index create --topic-id <tid> --body file://./index.json
  tlsctl index modify --topic-id <tid> --body file://./index.json
  cat ./index.json | tlsctl index create --topic-id <tid> --body -
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

Commands:
  search   Search logs via /SearchLogs
  export   Auto-page export logs (stdout JSON array; use --output jsonl for streaming)
  export-analysis  Export SQL/analysis rows as JSONL (one row object per line by default)

Notes:
  - SearchLogs requires X-Tls-Apiversion=0.3.0 (handled by client).
  - Full request body can be passed via --request file://...
  - --request also accepts "-" to read JSON from stdin.
  - Prefer --output jsonl log export for streaming.
  - For pure search (no analysis), you can paginate via Context/Limit/Sort/Offset.
  - For analysis (query contains "|"), Context/Sort/Limit/Offset in body are not effective; use SQL limit/offset in Query. Analysis does not support Context pagination.

Examples:
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

func usageAssistant() string {
	return u(`Usage:
  tlsctl assistant <command> [args]

Commands:
  describe-session-answer   Ask AI Assistant for a topic

Notes:
  - If --instance-id is not provided, TLS_AI_ASSISTANT_INSTANCE_ID will be used.
  - If instance id is missing, --account-id (or LOG_SERVICE_ACCOUNT_ID) is required to find/create one.

Examples:
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

func usageCompletion() string {
	return u(`Usage:
  tlsctl completion <bash|zsh|fish|powershell>

Examples:
  tlsctl completion zsh
`)
}
