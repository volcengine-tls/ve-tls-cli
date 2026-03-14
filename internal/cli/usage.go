package cli

func usageConfigure() string {
	return `Usage:
  tlsctl configure <command> [args]

Commands:
  set     Set a profile
  use     Set default profile
  show    Show a profile
  list    List profiles
  delete  Delete a profile (or batch delete by prefix)

Examples:
  tlsctl configure set --profile default --ak <ak> --sk <sk> --region cn-beijing
  tlsctl configure set --profile tenant-a-sg --ak <ak> --sk <sk> --region ap-singapore-1
  tlsctl configure use default
  tlsctl configure show --profile default
  tlsctl --profile tenant-a-sg project list
  tlsctl configure list
  tlsctl configure delete tenant-a-sg
  tlsctl configure delete --prefix tenant-a --yes
`
}

func usageAPI() string {
	return `Usage:
  tlsctl api call --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...>]

Examples:
  tlsctl api call --method GET --path /DescribeProject --query ProjectId=<pid>
  tlsctl api call --method POST --path /SearchLogs --body file://./search.json
`
}

func usageProject() string {
	return `Usage:
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
`
}

func usageTopic() string {
	return `Usage:
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

Examples:
  tlsctl topic list --project-id <pid> --page-size 20
  tlsctl topic get --topic-id <tid>
  tlsctl topic create --project-id <pid> --topic-name demo-topic --ttl 30 --shard-count 2 --auto-split --max-split-shard 10
  tlsctl topic modify --topic-id <tid> --description updated --ttl 60
  tlsctl topic delete --topic-id <tid>
  tlsctl topic create --request file://./create_topic.json
`
}

func usageMetricTopic() string {
	return `Usage:
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

Examples:
  tlsctl metric-topic list --project-id <pid> --page-size 20
  tlsctl metric-topic get --topic-id <metric_tid>
  tlsctl metric-topic create --project-id <pid> --topic-name demo-metric --ttl 30 --shard-count 2
  tlsctl metric-topic modify --topic-id <metric_tid> --description updated --ttl 60
  tlsctl metric-topic delete --topic-id <metric_tid>
  tlsctl metric-topic search --topic-id <metric_tid> --query 'avg(rate(http_requests_total[5m]))' --from 1710374400000 --to 1710378000000
  tlsctl metric-topic prom --help
`
}

func usageMetricTopicProm() string {
	return `Usage:
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
`
}

func usageIndex() string {
	return `Usage:
  tlsctl index <command> [args]

Commands:
  get      Get index by topic id
  create   Create index with JSON body
  modify   Modify index with JSON body

Examples:
  tlsctl index get --topic-id <tid>
  tlsctl index create --topic-id <tid> --body file://./index.json
  tlsctl index modify --topic-id <tid> --body file://./index.json
`
}

func usageLog() string {
	return `Usage:
  tlsctl log <command> [args]

Commands:
  search   Search logs via /SearchLogs
  export   Auto-page export logs (stdout JSON array; use --output jsonl for streaming)

Notes:
  - SearchLogs requires X-Tls-Apiversion=0.3.0 (handled by client).
  - Full request body can be passed via --request file://...

Examples:
  tlsctl log search --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --limit 100 --sort desc
  tlsctl log search --request file://./search_logs.json
  tlsctl --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 10
`
}

func usageAI() string {
	return `Usage:
  tlsctl ai <command> [args]

Commands:
  list-packs  List builtin packs
  bootstrap   Create or reuse topic and ensure index
  export      Export logs for a pack

Examples:
  tlsctl ai list-packs
  tlsctl ai bootstrap --pack llm-trace-v1 --project-id <pid>
  tlsctl --output jsonl ai export --pack llm-trace-v1 --project-id <pid> --from 1710374400000 --to 1710378000000
`
}
