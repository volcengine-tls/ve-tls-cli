# Contract Override Provenance

This directory contains reviewed inputs to the checked-in operation catalog. Override files are not a live mirror of a service repository: they must remain reviewable, reproducible inputs whose generated catalog and lock are committed together.

## Supplemental operation policy

`supplemental_operations.json` adds an operation when its ID is absent from the source catalog and replaces the complete operation when the ID already exists. The merge is strict JSON and validates every supplemental entry as a complete contract before updating the catalog.

Keep the file stable for review:

- preserve existing entries and their order unless their contract changes;
- append a new capability batch by group, then sort IDs within that group;
- do not mix formatting or unrelated existing-operation rewrites into a capability batch;
- regenerate `internal/contract/generated_catalog.json` and `contracts/operation-catalog-v2-lock.json` after every semantic change.

## App, LogApp, and Trace alignment baseline

The App/LogApp/Trace capability batch is aligned to the log-service server repository at:

- branch: `epic_v6.6.2`
- commit: `af4f584761927a9bc2ca9ca1931b5f727f0de1dc`
- scope: 24 public operations in the `app`, `log-app`, and `trace` groups

The local reference worktree also contained annotation-only changes in `api/handler/log_app.go`, `api/rest/trace/search_spans.go`, and `api/rest/trace/search_traces.go`. This baseline intentionally pins the clean commit above. Required fields and limit constraints were derived from request binding and `Validate` behavior already present at that commit, not from those uncommitted Swagger annotations.

### Evidence precedence

Use server runtime code in this order:

1. request `Method()` and `Path()` for the wire contract;
2. request fields, `ParseQuery`, binding tags, and `Validate` for accepted input;
3. handler and service call chains for time units and other transformed semantics;
4. generated Swagger only as supporting documentation.

When Swagger conflicts with runtime code, record the runtime behavior in `docs.usage_constraints` and do not silently preserve the Swagger error.

### Operation-to-symbol map

Paths below are relative to the log-service repository at the pinned commit.

| Operation ID | Method and path | Server request symbol |
| --- | --- | --- |
| `app.create` | `POST /CreateApp` | `api/rest/template_market/create_app.go::CreateAppRequest` |
| `app.delete` | `DELETE /DeleteApp` | `api/rest/template_market/delete_app.go::DeleteAppRequest` |
| `app.describe` | `GET /DescribeApp` | `api/rest/template_market/describe_apps.go::DescribeAppRequest` |
| `app.describe-apps` | `GET /DescribeApps` | `api/rest/template_market/describe_apps.go::DescribeAppsRequest` |
| `app.describe-template` | `GET /DescribeTemplate` | `api/rest/template_market/describe_template.go::DescribeTemplateRequest` |
| `app.describe-templates` | `GET /DescribeTemplates` | `api/rest/template_market/describe_template.go::DescribeTemplatesRequest` |
| `app.modify` | `POST /ModifyApp` | `api/rest/template_market/modify_app.go::ModifyAppRequest` |
| `log-app.create` | `POST /CreateLogApp` | `api/rest/log_app/create.go::CreateReq` |
| `log-app.delete` | `DELETE /DeleteLogApp` | `api/rest/log_app/delete.go::DeleteReq` |
| `log-app.describe` | `GET /DescribeLogApp` | `api/rest/log_app/describe.go::DescribeReq` |
| `log-app.describe-dashboard` | `GET /DescribeLogAppDashboard` | `api/rest/log_app/describe_dashboard.go::DescribeDashboardReq` |
| `log-app.describe-dashboard-templates` | `GET /DescribeLogAppDashboardTemplates` | `api/rest/log_app/describe_dashboards.go::DescribeTemplatesReq` |
| `log-app.describe-log-apps` | `GET /DescribeLogApps` | `api/rest/log_app/describes.go::DescribeLogAppsReq` |
| `log-app.describe-market` | `GET /DescribeLogAppMarket` | `api/rest/log_app/describe_market.go::DescribeLogAppMarketReq` |
| `log-app.describe-session` | `POST /DescribeLogAppSession` | `api/rest/log_app/describe_session.go::DescribeSessionReq` |
| `log-app.describe-sessions` | `POST /DescribeLogAppSessions` | `api/rest/log_app/describe_sessions.go::DescribeSessionsReq` |
| `log-app.describe-trace` | `POST /DescribeLogAppTrace` | `api/rest/log_app/describe_trace.go::DescribeTraceReq` |
| `log-app.modify` | `PUT /ModifyLogApp` | `api/rest/log_app/modify.go::ModifyReq` |
| `log-app.search-spans` | `POST /SearchLogAppSpans` | `api/rest/log_app/search_spans.go::SearchSpansReq` |
| `log-app.search-traces` | `POST /SearchLogAppTraces` | `api/rest/log_app/search_traces.go::SearchTracesReq` |
| `trace.delete-scores` | `DELETE /DeleteTraceScores` | `api/rest/trace_score/trace_score.go::DeleteTraceScoresReq` |
| `trace.describe-scores` | `GET /DescribeTraceScores` | `api/rest/trace_score/trace_score.go::DescribeTraceScoresReq` |
| `trace.modify-scores` | `POST /ModifyTraceScores` | `api/rest/trace_score/trace_score.go::ModifyTraceScoresReq` |
| `trace.search-spans` | `POST /SearchSpans` | `api/rest/trace/search_spans.go::SearchSpansReq` |

### Runtime semantics checked beyond Swagger

- `DescribeApps` accepts one `AppType` query value, uses the exact list-filter spelling `AppID`, and parses `Tags` from a JSON-encoded string array.
- `DescribeLogApps` exposes external `PageNumber`/`PageSize` pagination. `IamProjectName` is intentionally absent because the pinned `ParseQuery` does not read it.
- `SearchSpans` and `SearchLogAppSpans` pass outer `StartTime`/`EndTime` to `SearchLogs` as Unix milliseconds. Nested start-time and duration filters use microseconds.
- `DescribeLogAppSession` and `DescribeLogAppSessions` accept microsecond timestamps and divide them by 1000 before querying logs.
- `DescribeTraceScores` reads `SpanIds` with `QueryArray`; the CLI therefore sends array values as repeated query parameters.

## Shard merge alignment baseline

`shard.merge` is aligned to the same clean `epic_v6.6.2` commit `af4f584761927a9bc2ca9ca1931b5f727f0de1dc` using these runtime symbols:

- `api/rest/shard/merge_shard.go::ManualMergeShardReq`: `POST /ManualMergeShard`, required JSON body fields `TopicId` and `ShardId`, UUID validation for `TopicId`, and `ShardId >= 0`;
- `api/handler/shard.go::ManualMergeShard`: merges the selected readwrite shard with its next contiguous readwrite shard, rejects a final-range or non-mergeable shard, and serializes split/merge operations per Topic;
- `api/rest/shard/merge_shard.go::ManualMergeShardResp`: returns the resulting `Shards` list.

This route exists in the server and the legacy agentic snapshot but is absent from the generated public source catalog, so it is added as a supplemental operation with high-risk retry semantics. After an ambiguous result, use `shard.describe` to reconcile the shard list instead of retrying automatically.

## Regeneration and validation

After editing supplemental operations, run:

```bash
go test ./internal/openapigen -run 'Test(LoadSupplemental|MergeSupplemental|SupplementalMerge|CommittedSupplemental)'

go run ./internal/openapigen \
  --merge-supplemental-operations-only \
  --out-operation-catalog internal/contract/generated_catalog.json \
  --out-operation-catalog-lock contracts/operation-catalog-v2-lock.json \
  --lock-root . \
  --supplemental-operation-overrides contracts/overrides/supplemental_operations.json
```

These checks prove strict decoding, internal contract validity, deterministic merge behavior, and catalog/lock integrity. They do not fetch the log-service repository or prove ongoing parity with a later server revision; updating the pinned baseline requires a new source audit.
