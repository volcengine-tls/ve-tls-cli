# 5. Practical Guide

[← Previous: Usage](4-Usage.md) | [中文](5-Practical-Guide_zh.md) | [Next: Advanced →](6-Advanced.md)

This guide walks through three end-to-end scenarios using the default `volclog` command surface (`tool`, `workflow`, `raw`). For installation, authentication, configuration, and command architecture, see [Getting Started](1-Getting-Started.md), [Authentication](2-Authentication.md), [Configuration](3-Configuration.md), and [Usage](4-Usage.md).

`volclog-human` shortcuts are an optional interactive path and are invoked explicitly as `volclog-human`; they are not used in the automation-oriented flows below. See [Human Shortcuts](7-Human-Shortcuts.md) for interactive terminal usage.

## 1. Connect a new service to TLS and verify logs are searchable

### Goal

Create a project and topic, send a sample log, and confirm the log is searchable.

### Prerequisites

- A configured profile with permission to create projects and topics, write logs (`PutLogs`), and search logs (`SearchLogs`).
- The profile's region and endpoint are already set (see [Configuration](3-Configuration.md)).

### Placeholders

| Placeholder | Meaning |
| --- | --- |
| `<profile>` | The configured profile name |
| `<project-name>` | Name for the new project |
| `<region>` | TLS region for the project |
| `<project-id>` | Returned by `project.create` |
| `<topic-name>` | Name for the new topic |
| `<shard-count>` | Integer chosen according to current service requirements |
| `<ttl-days>` | Integer chosen according to current retention requirements and service constraints |
| `<topic-id>` | Returned by `topic.create` |
| `<marker>` | Unique string suffix for the sample log |
| `<from-ms>` / `<to-ms>` | Unix millisecond range covering ingestion time |

Confirm `<shard-count>` and `<ttl-days>` constraints with `tool describe topic.create`; do not assume defaults or valid ranges.

### Discover before write

Inspect the contracts for the tools and workflow you will call so the request body matches the current schema:

```bash
volclog --profile '<profile>' tool describe project.create
volclog --profile '<profile>' tool describe topic.create
volclog --profile '<profile>' workflow describe log.ingest
volclog --profile '<profile>' tool describe log.search
```

### Create the project and topic

Use `--dry-run` first to validate the request shape locally before sending it. A dry-run is a local validation gate; it does not check remote existence, permissions, or TLS service behavior.

`project.create` requires `ProjectName` and `Region` in the body:

```bash
volclog --profile '<profile>' tool exec project.create \
  --input '{"ProjectName":"<project-name>","Region":"<region>","Description":"example project"}' \
  --dry-run

volclog --profile '<profile>' tool exec project.create \
  --input '{"ProjectName":"<project-name>","Region":"<region>","Description":"example project"}'
```

Note the returned `ProjectId`. Then create the topic. `topic.create` requires `ProjectId`, `TopicName`, `ShardCount`, and `Ttl` in the body:

```bash
volclog --profile '<profile>' tool exec topic.create \
  --input '{"ProjectId":"<project-id>","TopicName":"<topic-name>","ShardCount":<shard-count>,"Ttl":<ttl-days>}' \
  --dry-run

volclog --profile '<profile>' tool exec topic.create \
  --input '{"ProjectId":"<project-id>","TopicName":"<topic-name>","ShardCount":<shard-count>,"Ttl":<ttl-days>}'
```

Note the returned `TopicId`.

### Send a sample log and search

Use the `log.ingest` workflow to send a sample log from a local file rather than hand-authoring the `LogGroups` request body. Create a short local file with a unique marker placeholder:

```bash
printf 'tls-pipeline-verify-<marker>\n' > sample.log
```

Run a workflow dry-run first. Workflow dry-run is local validation only; it does not check remote state. Then run the same workflow without dry-run:

```bash
volclog --profile '<profile>' workflow exec log.ingest \
  --input '{"TopicId":"<topic-id>","Input":"file://sample.log","InputFormat":"lines"}' \
  --context '{"execution":{"dry_run":true}}'

volclog --profile '<profile>' workflow exec log.ingest \
  --input '{"TopicId":"<topic-id>","Input":"file://sample.log","InputFormat":"lines"}'
```

Wait a few seconds for indexing, then search using the unique marker placeholder. Replace `<from-ms>` and `<to-ms>` with millisecond timestamps covering the send time:

```bash
volclog --profile '<profile>' tool exec log.search \
  --input '{"TopicId":"<topic-id>","Query":"tls-pipeline-verify-<marker>","StartTime":<from-ms>,"EndTime":<to-ms>,"Limit":20}'
```

### Success signals

- `project.create` and `topic.create` return `status: success` and a non-empty `ProjectId` / `TopicId`.
- `log.ingest` returns `status: success`.
- `log.search` returns `status: success` and the `data` contains the sample log entry.
- If `ResultStatus` is `incomplete`, the service returned only a partial scan; narrow the time range and rerun before trusting the result.

### Recovery checkpoints

- If `project.create` fails with a permission error, verify the profile's identity with `volclog --profile '<profile>' doctor --online` (this checks profile resolution and minimal live connectivity; permissions, quota, and resource state remain server-side).
- If `log.search` returns no results, confirm the `TopicId`, time range, and that enough time passed for indexing; use `tool describe log.search` to recheck the query syntax.
- If a dry-run fails, inspect the structured error first. A dry-run sends no business API request, but local prerequisites can still fail:
  - Contract shape or missing-field errors: rerun `tool describe` / `workflow describe` and align the fields.
  - Profile, credential, endpoint, or region errors: inspect [Configuration](3-Configuration.md) and run `doctor`.
  - Local input or file errors: check the path exists and is readable, and that the input format matches the contract.

## 2. Move from an alert to exported evidence for analysis

### Goal

Take a query surfaced by an alert, verify it with a bounded search, then export the matching logs to a file for offline analysis.

### Prerequisites

- A configured profile. Replace `<profile>`, `<topic-id>`, `<query>`, `<from-ms>`, `<to-ms>`, and `<max-pages>` with your values.
- A writable output directory for the exported file.

### Preview with a bounded search first

Run `tool describe log.search` to confirm the query syntax, then run a bounded `log.search` and inspect the stdout envelope's `status`, `requestId`, and service `ResultStatus`:

```bash
volclog --profile '<profile>' tool describe log.search

volclog --profile '<profile>' tool exec log.search \
  --input '{"TopicId":"<topic-id>","Query":"<query>","StartTime":<from-ms>,"EndTime":<to-ms>,"Limit":20}'
```

If `ResultStatus` is `incomplete`, narrow the time range or query before exporting; the export workflow does not expose `ResultStatus` in its output.

### Discover the export contract

The only current workflow IDs are `log.export`, `log.export-analysis`, and `log.ingest`. Use `workflow describe` before creating request JSON so the fields match the current contract:

```bash
volclog --profile '<profile>' workflow describe log.export
```

`log.export` auto-paginates pure-search results. It defaults to at most 100 pages when `MaxPages` is omitted, and can stop at the selected/default page limit without exposing a "more pages remained" signal. The exported data artifact contains rows only and does not preserve `ResultStatus` or the final `ListOver` state. Command success and file existence therefore prove only that those rows were exported, not that the evidence set is complete. For evidence-grade export, inspect a bounded preview first, do not proceed as complete when preview `ResultStatus=incomplete`, narrow or split the time window or query, and explicitly choose `MaxPages` based on expected volume. You own the `MaxPages` choice. For analysis (SQL) queries, use `log.export-analysis` instead; it does not auto-paginate analysis rows, so SQL `limit`/`offset` semantics apply.

### Export to a file

Force JSONL export to a writable directory with an explicit `MaxPages`. `log.export` streams exported rows directly into the data artifact (JSONL rows for `--output jsonl`); the artifact is not an envelope. stdout contains only the fixed file notice with the data-artifact path:

```bash
volclog --profile '<profile>' --output jsonl --output-mode file --output-dir ./evidence \
  workflow exec log.export \
  --input '{"TopicId":"<topic-id>","Query":"<query>","StartTime":<from-ms>,"EndTime":<to-ms>,"MaxPages":<max-pages>}'
```

### Success signals

- stdout prints `结果已写入文件。\n文件: <path>\n` (the fixed file notice).
- The file at `<path>` contains the exported log rows (JSONL), not an envelope.
- The export artifact does not contain `status`, `summary.deliveryMode`, `requestId`, or `ResultStatus`.
- Success proves only that the exported rows were written; it does not prove the evidence set is complete.

### Recovery checkpoints

- If the command errors with `result too large for stdout`, the output was not forced to file; rerun with `--output-mode file --output-dir '<writable-dir>'`.
- If `workflow exec` reports a missing required field, rerun `workflow describe log.export` and align the request body.
- If transport request IDs for the multi-page export are needed, enable a restricted trace directory (`--trace-dir '<dir>'`) and inspect the trace after the run. Trace files are sensitive: structured fields keep header/query keys and body hashes, but the transport `error_message` field may contain URL and query values. Never put credentials in query parameters.

## 3. Diagnose a host-group/collector pipeline whose logs are not arriving

### Goal

Determine whether a host group and its collector rule are configured correctly and whether hosts are reporting.

### Prerequisites

- A configured profile. Replace `<profile>`, `<project-id>`, `<host-group-name>`, and `<rule-name>` with your values.

### Discover the contracts

```bash
volclog --profile '<profile>' tool describe host-group.describe-host-groups-v2
volclog --profile '<profile>' tool describe host-group.describe-hosts
volclog --profile '<profile>' tool describe collector.describe-rules
```

### List and filter host groups

`host-group.describe-host-groups-v2` has `supports_all=false` and does not accept `ProjectId`. Filter by `HostGroupName` and request an explicit first page. Do not use `--page-all` on this action:

```bash
volclog --profile '<profile>' tool exec host-group.describe-host-groups-v2 \
  --input '{"HostGroupName":"<host-group-name>","PageNumber":1,"PageSize":20}'
```

`host-group.describe-host-groups-v2` does not accept `ProjectId`, so name filtering can return same-name groups from different projects. Before copying `<host-group-id>`, inspect each returned item's project and name metadata and confirm it belongs to the intended project. If ambiguous, refine the available filters or inspect candidates; do not guess an ID. Inspect the returned data and the current `tool describe` contract for the exact field names.

### Inspect hosts

`host-group.describe-hosts` requires `HostGroupId` and has `supports_all=true`. Use `--page-all` to fetch all pages:

```bash
volclog --profile '<profile>' tool exec host-group.describe-hosts \
  --input '{"HostGroupId":"<host-group-id>"}' --page-all
```

### Inspect collector rules

`collector.describe-rules` accepts `ProjectId` and `RuleName` and has `supports_all=true`:

```bash
volclog --profile '<profile>' tool exec collector.describe-rules \
  --input '{"ProjectId":"<project-id>","RuleName":"<rule-name>"}' --page-all
```

### Success signals

- `host-group.describe-host-groups-v2` returns the matching host group; inspect the response for the host group identifier.
- `host-group.describe-hosts` returns the hosts in the group; inspect the returned data and the `tool describe` contract for the exact status field names.
- `collector.describe-rules` returns the rules matching the project and rule name.

### Recovery checkpoints

- If the host group is not found, verify the `HostGroupName` filter and that the group was created in the expected project.
- If hosts are not reporting, check the collector rule's path/topic binding and that the collector agent is running on the host; use `doctor --online` to confirm the profile can reach the endpoint (this checks connectivity, not permissions or resource state).
- If `--page-all` is rejected, the tool contract does not support it for that action; inspect `tool describe` for `supports_all` and paginate manually if needed.

---

[← Previous: Usage](4-Usage.md) | [中文](5-Practical-Guide_zh.md) | [Next: Advanced →](6-Advanced.md)
