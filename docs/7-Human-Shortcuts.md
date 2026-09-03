# 7. Human Shortcuts

[← Previous: Advanced](6-Advanced.md) | [中文](7-Human-Shortcuts_zh.md) | [Next: README →](../README.md)

`volclog-human` is for terminal users who already know the target resource and operation. It shares authentication and configuration with `volclog` and adds resource-oriented shortcuts such as `project`, `topic`, `metric-topic`, `index`, `log`, `host-group`, and `collector`.

## 1. Choose the Right Surface

| Scenario | Recommended surface |
| --- | --- |
| You already know that you want to list projects, create a topic, search logs, or inspect collector rules | A `volclog-human` shortcut |
| You need a stable contract for one public API operation | `volclog-human tool` |
| You need a higher-level CLI flow such as ingest or export | `volclog-human workflow` |
| A write operation must be checked with `--dry-run` first | `tool exec` / `workflow exec` |
| You already know the HTTP method and path | `volclog-human raw` |

All of these surfaces are included in `volclog-human`. Agents, CI jobs, and scripts should prefer `tool`, `workflow`, or `raw` instead of depending on shortcut options.

## 2. Start with Help

See [Getting Started](1-Getting-Started.md#3-installation) for installation. After installation, verify:

```bash
volclog-human --version
volclog-human --help
volclog-human --profile default doctor
```

`volclog-human version` emits the same machine-readable build and catalog metadata as `volclog version`; `volclog-human --version` remains the text-compatible version output. `upgrade` and Skill lifecycle commands are shared CLI groups rather than resource shortcuts; see [Usage](4-Usage.md) and [Advanced](6-Advanced.md).

Do not memorize every option. Discover the installed version in this order:

```bash
volclog-human topic --help
volclog-human topic create --help
volclog-human topic create --describe
volclog-human index create --print-request-template=required > index-request.json
```

Complex shortcut requests use `--request`; `tool exec` and `workflow exec` use `--input`. Prefer placing global identity options before the command group, for example `volclog-human --profile default project list`.

## 3. Three Common Scenarios

### 3.1 Inspect Resources

```bash
volclog-human --profile default --output table project list
volclog-human --profile default --output table topic list \
  --project-id 'YOUR_PROJECT_ID' --all
volclog-human --profile default host-group list --all
volclog-human --profile default collector list \
  --project-id 'YOUR_PROJECT_ID' --all
volclog-human --profile default host-group get \
  --host-group-id 'YOUR_HOST_GROUP_ID'
volclog-human --profile default collector get --rule-id 'YOUR_RULE_ID'
```

`--all` iterates every supported page. Do not combine it with `--page-number` or `--cursor`. `table` is limited to common list/get operations, `index get`, and `log search`; not every shortcut supports it.

For a collection failure, use these commands to locate the host group and rule. To inspect hosts or bindings next, run `volclog-human tool describe host-group.describe-hosts` and `volclog-human tool describe collector.apply-rule-to-host-groups`.

### 3.2 Create a Topic and Index

The following shortcuts perform real writes. When you need a preview, switch to the public contract first; inspect `index.create` in the same way for an index:

```bash
volclog-human tool describe topic.create
volclog-human tool describe index.create
volclog-human --profile default --dry-run tool exec topic.create \
  --input '{"ProjectId":"YOUR_PROJECT_ID","TopicName":"YOUR_TOPIC_NAME","Ttl":30,"ShardCount":1}'
```

After confirming the request, use the shortcuts:

```bash
volclog-human --profile default topic create \
  --project-id 'YOUR_PROJECT_ID' \
  --topic-name 'YOUR_TOPIC_NAME' \
  --ttl 30 \
  --shard-count 1

volclog-human index create --print-request-template=required > index-request.json
volclog-human --profile default index create \
  --topic-id 'YOUR_TOPIC_ID' \
  --request file://index-request.json
```

Shortcuts do not support `--dry-run`. Complete the operation through `tool` or `workflow` when a dry run is required; do not add `--dry-run` to a shortcut.

### 3.3 Search and Export Logs

Start with a small result, then export the same query and time range:

```bash
volclog-human --profile default --output table log search \
  --topic-id 'YOUR_TOPIC_ID' \
  --query "error" \
  --from 'START_TIME_MS' \
  --to 'END_TIME_MS' \
  --limit 20

volclog-human --profile default \
  --output jsonl --output-mode file --output-dir ./out \
  log export \
  --topic-id 'YOUR_TOPIC_ID' \
  --query "error" \
  --from 'START_TIME_MS' \
  --to 'END_TIME_MS' \
  --max-pages 10
```

Use `log export` for plain-search results and `log export-analysis` for SQL or analysis rows. See [Usage](4-Usage.md#7-output-and-delivery) and [Advanced](6-Advanced.md) for complete output, pagination, and incomplete-result semantics.

---

[← Previous: Advanced](6-Advanced.md) | [中文](7-Human-Shortcuts_zh.md) | [Next: README →](../README.md)
