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

The local reference worktree also contained annotation-only changes in `api/handler/log_app.go`, `api/rest/trace/search_spans.go`, and `api/rest/trace/search_traces.go`. This baseline intentionally pins the clean commit above. Request constraints must be checked against parsing, explicit `Validate` behavior, and handler defaults already present at that commit, not inferred solely from Swagger annotations or `binding:"required"` tags.

### Evidence precedence

Use server runtime code in this order:

1. request `Method()` and `Path()` for the wire contract;
2. the actual JSON/query parser and validator configuration, followed by explicit `Validate` checks, for accepted input;
3. handler, service, and persistence call chains for defaults, time units, and other transformed semantics;
4. generated Swagger only as supporting documentation.

When Swagger conflicts with runtime code, record the runtime behavior in `docs.usage_constraints` and do not silently preserve the Swagger error.

In particular, the current strict JSON parser does not invoke Gin's binding
validator. `ValidatorMW` uses the validator's default `validate` tag and then
calls `Validate`; `binding:"required"` alone therefore does not make a field
required at runtime. Non-pointer booleans and custom enums can have valid zero
values even when the corresponding annotation claims the field is required.

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

## ConsumeLogs correction and documentation baseline

`log.consume` keeps `POST /ConsumeLogs` with the `consumelogs` codec. Its query
requires `TopicId` and `ShardId`; its body requires only `Cursor`. `Offset` belongs
to `PullKafkaLogsReq`, not `PullLogsReq`, and is intentionally absent from this
operation's schema.

The old local `API 参考/日志管理/ConsumeLogs.md` incorrectly listed a required Kafka
`Offset`. The documentation merge added that row to the generated body schema,
which made the CLI reject otherwise valid cursor requests. The complete
supplemental replacement prevents this row from returning in either source or
merge-only generation.

The `API 参考1` download inspected on 2026-09-09 corrects the body table to
`Cursor`, `EndCursor`, `Compression`, `LogCount`, `LogGroupCount`, `ProcessorId`,
and `Size`, matching the local Swagger `log.PullLogsReq`. It is supporting evidence,
not an authoritative replacement: its request section still says GET, whereas
Swagger marks GET deprecated and POST canonical; its JSON response example also
contradicts its own protobuf response description. Keep the existing method and
codec when using the updated parameter documentation.

### Remaining documentation import findings

The local comparison covers 141 old and 142 new Markdown files, with 141 shared
filenames, eight changed request tables, and the newly documented
`ManualMergeShard`. Other added request fields are `CreateDownloadTask.MustComplete`,
`CreateIndex`/`ModifyIndex.EnablePhraseIndex`, and `ProcessorType` in four processor
APIs. These additions already exist in the local Swagger snapshot.

- Switching only `--api-doc-root` to `API 参考1` fails source generation with
  duplicate route `shard.ManualMergeShard`: the source ID becomes
  `shard.manual-merge-shard` while the supplemental entry is `shard.merge`.
  The source-only comparison also changes `shard.create` to
  `shard.manual-shard-split`. Resolve route identity before migrating inputs.
- `log.track` still has a separate schema defect: `WebTracks.md` describes
  `Logs` as an object array, but the imported schema uses string items and
  incorrectly promotes the explanatory `Key`/`Value` table to body properties.
  The parser also does not recognize its `必选` marker. This finding is not
  corrected by the `log.consume` override or the new documentation download;
  the reviewed `log.track` replacement below corrects the published contract
  without changing the general Markdown parser.
- Some new pages, including `DescribeCursorTime.md`, use `## URI 请求参数`
  rather than the parser's `## 请求参数` plus location subheadings. Their request
  tables are not imported by the current parser; Swagger remains the only
  schema input for those pages.
- A top-level comparison of the old source-derived catalog against Swagger
  flags nine operations in total. Besides the two log schema defects above,
  import/project/tag/topic discrepancies include documented fields missing from
  Swagger or empty Swagger definitions; they are not proven service errors.
  The `log.put.LogGroups` requirement is an explicit protocol override. Do not
  remove these fields solely because Swagger lacks them.

## Reviewed request-contract alignment (2026-09-09)

The following replacements correct the existing API surface only. They do not
add AI assistant, Copilot, or any other previously unexposed operations. The
server baseline is the same `epic_v6.6.2` revision above; enum and persistence
defaults were additionally checked against its pinned wayout dependency commit
`29599d56a1a4`.

| Existing operation | Reviewed correction | Primary server evidence |
| --- | --- | --- |
| `log.track` | Required nonempty `Logs` array of string-valued objects; no top-level `Key`/`Value` fields | `api/service/service.go::TrackLogsBody`, `paramJSONCheck`, and `trackingLogsFormat` |
| `processor.exec-processor` | Omitted `ProcessorType`/`ProcessorDSLType` use `ingester`/`dsl`; neither is unconditionally required | `api/rest/processor/processor.go::ExecRequest.Validate` and wayout enum definitions |
| `index.create`, `index.modify` | `CaseSensitive` defaults to false; Chinese indexing permits an omitted or empty delimiter, and does not prohibit a nonempty valid delimiter | `api/rest/util.go::CheckFulltextIndexParaValidate` |
| `project.create`, `topic.create`, `tag.add`, `tag.tag-resources` | `Tags` contains objects with `Key`/`Value`, not strings; preserve each operation's existing top-level required policy | Request structs and wayout `tls/dao/api/util.go::TagList` |
| `topic.create` | `EncryptConf` is an object; omitted `ShardCount` is defaulted by the handler | `api/rest/topic/create.go`, `api/handler/topic.go`, wayout `tls/dao/api/topic.go::EncryptConf` |
| `tag.list` | Object-valued `TagFilters`; explicit `MaxResults` in the range 10–100, without a claimed server default of 20 | `api/rest/tag/describe_tags.go::DescribeResourceTagsReq.Validate` |
| `processor.describe-processor-functions` | Language enum is `dsl`/`spl`, not `dsl`/`sql` | `api/rest/processor/processor.go::DescribeProcessorFunctionsRequest.ParseQuery` |
| `processor.create-processor` | Omitted type uses `ingester`, empty description is accepted, `MaxQps` defaults to 0; keep the verified persistence default of 5000 for `TimeoutMs` | `CreateProcessorRequest.Validate`, request conversion, and wayout processor model |
| `log.create-download-task` | Search/Analyze require explicit positive `Limit` and `Sort`; LogContext requires `ContextFlow` and `Source`; do not claim a default `Sort` | `api/rest/index/create_download_task.go::CreateDownloadTaskReq.Validate` |

JSON Schema describes the conditional requirements and types. The existing CLI
validator still checks required-field presence recursively, not array item
types, enums, ranges, or `if`/`then` conditions. Defaults in the contract describe
server behavior; the CLI does not inject these values. Dry-run is therefore a
request-planning check, not proof that all service validation will pass.

### Download-task identity migration

The published IDs are `log.create-download-task` and
`log.cancel-download-task`. Their API action, method, path, codec, and risk
classification are unchanged. `log.create` and `log.cancel` are removed user
identities: calls using either old name fail with a replacement-name hint,
including when its verb would otherwise be unique within the log group.

The generator performs the two explicit, wire-checked identity migrations at
the shared supplemental merge stage, used by both source generation and
merge-only regeneration. It does not create duplicate operations or relax the
generation lock's unchanged-input checks. The creation replacement uses the
new ID; the checked-in bootstrap entry is migrated before replacement matching.
Other operation IDs remain unchanged.

`CreateDownloadTask` creates an asynchronous task and returns `TaskId`; it does
not download a file to the local machine. Continue with
`log.describe-download-tasks` to inspect task status and
`log.describe-download-url` to retrieve the download URL.

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
