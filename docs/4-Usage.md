# 4. Usage

[← Previous: Configuration](3-Configuration.md) | [中文](4-Usage_zh.md) | [Next: Practical Guide →](5-Practical-Guide.md)

This guide covers how to run commands with `volclog`: the command surface, how to discover tools and workflows, how `tool exec`, `workflow exec`, and `raw` work, how context and selectors interact, and how output, filtering, and tracing behave. For authentication and configuration details, see [Authentication](2-Authentication.md) and [Configuration](3-Configuration.md).

## 1. Command architecture and edition boundary

The default `volclog` command exposes these groups: `configure`, `tool`, `workflow`, `raw`, `doctor`, `skill`, `upgrade`, `version`, `login`, `logout`, and `sso`.

`volclog-human` adds a human shortcut layer (`project`, `topic`, `metric-topic`, `index`, `log`, `host-group`, `collector`) alongside `tool`/`workflow`/`raw`. The shortcuts reuse shared authentication, transport, envelope, and tracing infrastructure, but have their own shortcut argument parsing and request construction. Contract validation, table support, and file/stdout behavior can differ from `tool`/`workflow`/`raw`. Users and automation must follow the documented contract for the selected command surface.

Choose the right surface:

- Use `tool` when you know the API action and want contract validation and a structured envelope.
- Use `workflow` when you want CLI-provided higher-level orchestration over one or more tools.
- Use `raw` only when you already know the exact method and path and need a direct transport call.
- Use `volclog-human` shortcuts only for interactive terminal work; agents and CI should default to `tool`/`workflow`/`raw`.

### Version and explicit upgrade

`version` is the machine-readable build and catalog metadata command. `--version` remains the text-compatible form for scripts that only need the version string:

```bash
volclog version
volclog --version
```

The JSON returned by `volclog version` includes `schema_version`, `version`, `edition`, `commit`, `catalog_digest`, `operation_count`, `public_operation_count`, and `workflow_count`.

Upgrade checks and installs are explicit only; `volclog` never performs a background network check:

```bash
volclog upgrade --check
volclog upgrade --version 1.1.1
volclog upgrade --version 1.1.1 --yes
volclog upgrade --yes
```

`--check` and a version selection without `--yes` do not write files. npm installations delegate to npm. Standalone binaries require release checksum verification and use an atomic replacement.

### Upgrading to 1.1.0

Update saved commands, scripts, and Agent tool references to use the new download-task IDs:

- `log.create` → `log.create-download-task`
- `log.cancel` → `log.cancel-download-task`

The old short names are removed, not kept as aliases. Calls using them fail with a replacement-name hint; the underlying API actions, HTTP methods, and paths are unchanged. Creating a download task remains asynchronous and does not download a local file.

Request contracts have also been corrected: `log.consume` requires `Cursor`, not the Kafka-only `Offset`; `tag.list` requires an explicit `MaxResults` between 10 and 100. Refresh cached contracts with `volclog tool describe <tool-id> --view full`. Defaults describe server behavior and are not injected by the CLI; dry-run does not replace service-side type, range, enum, or conditional validation.

### Skill lifecycle

All Skill lifecycle commands require `--dir`. Repeat `--name` to select multiple bundled Skills; omit it to select all bundled Skills:

```bash
volclog skill list
volclog skill install --dir <agent-skills-dir> [--name <name>] [--force]
volclog skill status --dir <agent-skills-dir> [--name <name>]
volclog skill update --dir <agent-skills-dir> [--name <name>] [--force]
volclog skill uninstall --dir <agent-skills-dir> [--name <name>] [--force]
```

Each installed Skill directory contains `.volclog-skill.json` with the schema version, Skill name, installed CLI version, source digest, and installed-content digest. Digests are stable SHA-256 values over sorted relative POSIX paths and length-framed file contents; the sidecar is excluded, and symlinks or special files are rejected. `status` is offline and reports `not_installed`, `current`, `outdated`, `modified`, `untracked`, or `invalid_manifest`. A version mismatch is reported separately and does not by itself mean that the user modified the Skill.

`current` means the on-disk content matches `installed_digest` and the bundled source matches `source_digest`; `outdated` means the content is unchanged but the bundled source digest changed; `modified` means the on-disk content differs from `installed_digest`. A missing sidecar is `untracked`, while an unreadable or inconsistent sidecar is `invalid_manifest`.

`install` errors when a target already exists unless `--force` is explicit. `update` and `uninstall` skip `modified`, `untracked`, and `invalid_manifest` Skills by default; use `--force` to replace or remove them. Installation and updates stage a complete directory, replace it with same-filesystem renames, and restore the prior directory if replacement fails.

## 2. Discover before execution

Before running a tool or workflow, inspect its contract so you know the required input and the request shape.

### 2.1 `tool list` and `tool describe`

```bash
volclog tool list
volclog tool list project
volclog tool list app
volclog tool list log-app
volclog tool list trace
volclog tool list --verb create --format json
volclog tool describe project.describe-projects
volclog tool describe project.create --view full
```

`tool list` takes the group as a positional argument (not a `--group` flag) and supports `--verb` to filter by verb and `--format text|json` (default `text`). `tool describe <group.action>` shows the tool's identity, input schema, context schema, execution schema, behavior, output policy, and contract digest. The `--view` flag selects `compact` (default) or `full`; explicit JSON output defaults to `full`.

### 2.2 `workflow list` and `workflow describe`

```bash
volclog workflow list
volclog workflow list log
volclog workflow list app
volclog workflow list --format json
volclog workflow describe log.export
volclog workflow describe app.resolve-resources
```

`workflow list` takes the group as a positional argument and supports `--format text|json`. `workflow describe <group.command>` shows the workflow's kind, source, input schema, context schema, execution schema, recommended global flags, and guidance.

## 3. `tool exec`

`tool exec` runs a single tool by its `<group.action>` identity.

```bash
volclog --profile default tool exec project.describe-projects
```

### 3.1 Input and context

`--input` supplies the request body. It accepts `file://<path>`, `-` (stdin), or an inline JSON object (must start with `{`). `--context` supplies runtime and execution controls using the same sources.

Context validation rejects unknown fields, incorrect types, and conflicting aliases before applying the context. Supported legacy forms remain available: `trace` as a boolean, string, or object; `execution.artifact` as a boolean, string, or object; `execution.output`/`output_mode`; `execution.page.all`/`page_all`; and `execution.projection` as a string, object, or array.

```bash
volclog --profile default tool exec project.create \
  --input '{"ProjectName":"my-project","Region":"cn-beijing","Description":"example"}'

volclog --profile default tool exec project.create \
  --input file://req.json \
  --context '{"execution":{"dry_run":true}}'
```

The `file://req.json` file must contain the required `ProjectName` and `Region` fields.

### 3.2 Contract validation and dry-run

Before any network call, `tool exec` normalizes flat input into the correct `query`/`path`/`header`/`body` sections (when the contract defines an input schema) and validates required fields. A missing required field fails before the request is sent.

`--dry-run` runs the same contract validation, then skips the network call and returns a local plan that checks profile, endpoint, region, and body JSON validity:

```bash
volclog --profile default tool exec project.create \
  --input '{"ProjectName":"my-project","Region":"cn-beijing"}' --dry-run
```

`--page-all` is a direct flag for `tool exec` and requests all pages of a paginated result.

Inspect `summary.completeness` even when pages were merged: `summary.pagination.merged=true` means pages were accumulated, not that the result is complete. The service's `data.Total` is preserved; `summary.itemCount` is the number collected. Completeness reports `complete`, `stopReason`, `pageCount`, `itemCount`, `resumable`, and, when available, `next`. Reaching a page budget or a later request failure returns nonzero while retaining collected data. When `next` is present, invoke its `operationId` and `input` as an ordinary `tool exec` request with the same runtime selectors and no `--page-all` or enabled page-all context; `next.pageAll` is `false`.

## 4. `workflow exec`

`workflow exec` runs a CLI-provided higher-level workflow by its `<group.command>` identity. Workflows wrap one or more tools with orchestration logic; the public API tool surface remains available through `tool exec`.

```bash
volclog --profile default workflow exec log.export \
  --input file://export.json
```

### 4.1 Input and context

`--input` and `--context` behave exactly as in `tool exec`: `file://<path>`, `-` (stdin), or inline JSON object. The workflow's required params are validated before execution.

### 4.2 Execution controls

`workflow exec` does not accept `--artifact`, `--projection`, or `--page-all` as direct CLI flags. The controls it consumes from the `--context` JSON's `execution` object are:

- `execution.dry_run` — skip the network call
- `execution.artifact` — force file delivery
- `execution.projection` — project the raw result with a JMESPath expression before the envelope is built

Although the context parser recognizes `execution.page.all` / `execution.page_all`, current workflow execution does not consume them. Pagination semantics are workflow-specific; follow the guidance in `workflow describe` for the workflow you are using. Do not rely on page-all context controls for workflows.

```bash
volclog --profile default workflow exec log.export \
  --input file://export.json \
  --context '{"execution":{"dry_run":true}}'
```

Before running a workflow, use `workflow list` or `workflow describe` to confirm that the workflow ID is available in the current installation.

For SearchLogs, `ListOver` is a completion signal only for pure search. SQL/analysis results are complete only when `ResultStatus=complete`; `incomplete` and `time_out` are reported as incomplete, and `error` as a failure. Do not invent a SQL `OFFSET` to resume an incomplete response. An explicitly written SQL `LIMIT`/`OFFSET` is a separate query and does not guarantee a shared snapshot.

### 4.3 App resource-resolution workflows

The `app`, `log-app`, and `trace` groups expose atomic public API calls. Use `tool list <group>` and `tool describe <group.action> --view full` for their current contracts. Use the workflows below only when the task must expand relationships across `DescribeApp`, `DescribeLogApp`, and `DescribeTraceInstance`:

```bash
volclog workflow exec app.resolve-resources \
  --input '{"AppId":"YOUR_APP_ID"}'

volclog workflow exec app.resolve-topic-ids \
  --input '{"AppId":"YOUR_APP_ID"}'
```

- `app.resolve-resources` returns normalized `App`, `Nodes`, `Edges`, `LogAppIds`, `TraceInstanceIds`, and `TopicIds`. Non-LogApp applications retain generic `AppResource` nodes instead of guessed semantics.
- `app.resolve-topic-ids` only supports `AppType=LogApp` and returns first-seen, deduplicated `TopicIds`; Trace resources contribute both Trace and Dependency topics.
- Expansion never routes across Regions automatically and never returns a partial result after a dependent call fails. `--dry-run` shows the dependent steps, but resource IDs and Region checks still require live responses.

## 5. `raw`

Use `raw` only when you already know the exact method and path. It makes a direct transport call without contract validation.

```bash
volclog --profile default raw --method GET --path /DescribeProjects
```

### 5.1 Method, path, query, header, body

| Flag | Behavior |
| --- | --- |
| `--method` | HTTP method; default `GET`, uppercased |
| `--path` | Required; `/` is prepended if missing |
| `--query k=v` | Repeatable query parameter |
| `--header k=v` | Repeatable header |
| `--body` / `--input` | Request body; `--input` is a compatibility alias for `--body` |
| `--request-format` | Request body format; default `json` |

`--body` and `--input` are mutually exclusive; using both fails with `conflicting body selectors`. The body value accepts inline JSON, `-` (stdin), `file://<path>`, a bare file path, or a plain string. An empty body becomes `{}`.

### 5.2 Dry-run

`--dry-run` for `raw` only validates transport and local shape (profile resolution, endpoint, region, body JSON validity). It does **not** validate tool or workflow API required fields, because `raw` has no contract. No network call is made.

```bash
volclog --profile default raw --method POST --path /CreateProject \
  --body '{"ProjectName":"my-project"}' --dry-run
```

## 6. Context, selectors, and dry-run

### 6.1 Global vs context selectors

An explicit identity selector is optional. With none, normal profile selection or static environment resolution applies.

A non-empty `--profile` or `context.profile` must name an existing profile. Its static AK/SK and token are never replaced or filled by environment credentials; missing static AK/SK fails before a request. Dynamic identity remains provider-backed. Without an explicit profile, the existing environment precedence remains in effect.

The identity selectors are: global `--profile`, global `--secrets-file`, `context.profile`, and `context.secrets_file`. The conflict rules are:

- Any profile selector (global `--profile` or `context.profile`) combined with any secrets-file selector (global `--secrets-file` or `context.secrets_file`) conflicts.
- Global `--secrets-file` combined with `context.secrets_file` conflicts.
- Global `--profile` combined with `context.profile` conflicts only when the profile names differ. Repeating the same profile name in both places is accepted but redundant; avoid it for clarity.

A conflict fails with `conflicting runtime selectors` (or `conflicting profile selectors` when two different profile names are supplied).

Global `--region` and `--endpoint` flags and `context.region`/`context.endpoint` are one-command runtime overrides. They take precedence over applicable profile, environment, and project defaults and are never persisted. When a global flag and context provide different non-empty values for the same field, validation fails. The full region/endpoint/timeout precedence is detailed in [Configuration](3-Configuration.md).

### 6.2 `--dry-run` and context dry-run

`--dry-run` is a global flag that works for `raw`, `tool exec`, and `workflow exec`. For `tool exec` and `workflow exec`, `execution.dry_run` in the `--context` JSON has the same effect. `--dry-run` is rejected for other groups.

When a dry-run plan includes `request_preview`, it recursively masks values under known sensitive keys such as password, secret, token, access key, and credential; it also masks a `Contents`/`LogTags` `Value` when the corresponding `Key` is sensitive. An unparseable body is omitted from the preview, and `redacted_fields` may list masked paths. This changes only the preview: the wire request and `body_sha256` are unchanged. Ordinary body strings are not guaranteed to be identified as secrets.

Malformed JSON-like query values are replaced with `[unparseable query omitted]`; the displayed endpoint removes user information, query, and fragment.

### 6.3 Write-operation safety

For write operations, run with `--dry-run` first to validate the request locally before sending it to TLS. A dry-run plan reports whether local checks (profile, endpoint, region, body JSON) passed.

For provider-specific login, refresh, and logout behavior, see [Authentication](2-Authentication.md). For profile and runtime precedence, see [Configuration](3-Configuration.md).

## 7. Output and delivery

### 7.1 Output values

`--output` accepts `json` (default) and `jsonl` on the default `volclog` agent path. `table` is rejected on the default path; it is supported only by `volclog-human` and only for specific shortcut surfaces (`project`/`topic`/`metric-topic` list and get, `index get`, `log search`). `text` and `raw` are not accepted.

`--output-mode` accepts `stdout` (default) and `file`. Any other value is rejected.

Without an explicit stdout filter, default `volclog` emits JSON envelopes for `raw`, `tool exec`, and `workflow exec` success and error results. A JSON scalar or string returned by `raw` is wrapped under `data`; `volclog-human` retains its prior display. An explicit stdout `--jmes-filter` may instead return an array, scalar, or `null`.

### 7.2 stdout vs file delivery

By default, results are written to stdout. With `--output-mode file` (forced file mode), the output contract depends on the command surface:

- For `raw`, `tool exec`, and `workflow exec`, a successful result is written to an artifact file under `--output-dir`: normally the complete envelope, or the selected value when `--jmes-filter` is set. Default `volclog` writes a JSON delivery envelope to stdout with `status=success`, `data=null`, and the file path in `artifacts[].path`; `volclog-human` keeps the Chinese file notice.
- For non-envelope command groups, the raw result is written to the selected file and stdout emits only the bare file path.
- For generic human-shortcut envelope commands (for example `project list`, `topic create`), the envelope artifact is written to the file and the complete envelope is also emitted to stdout.
- For human `volclog-human log export` / `log export-analysis` without `--jmes-filter`, exported data rows are streamed into the file artifact (JSON array for `--output json`, JSONL rows for `--output jsonl`) and stdout emits the complete envelope containing artifact metadata. The artifact itself is not an envelope.

For a successful default file delivery, `summary.deliveryMode` is `file_forced` or `file_auto`, `summary.itemCount` retains the business-result count, `summary.totalBytes` is the stdout delivery envelope's byte length, and `artifacts[].sizeBytes` is the actual file size. This is a compatibility change: scripts that parsed the Chinese notice must instead parse `artifacts[].path`. Human log shortcuts retain their own output contract.

Exception: without `--jmes-filter`, default `workflow exec log.export` and `log.export-analysis` stream exported rows directly into the data artifact (JSON array for `--output json`, JSONL rows for `--output jsonl`) rather than writing an envelope. The data artifact is not an envelope and does not contain envelope metadata. On success, stdout contains the JSON delivery envelope with the data-artifact path. With `--jmes-filter`, these exports accumulate their results in memory before filtering and file delivery. For large exports, prefer unfiltered streaming or server-side field selection where available; client-side filtering does not reduce server scanning or network traffic. See [Advanced](6-Advanced.md) section 2 for details.

When a streamed export stops at its page budget or a later failure, it returns nonzero. When valid written rows can be retained, they go to a unique partial file without replacing the completed target. The failure envelope's artifact descriptor reports `completeness` and `metadataPath`; the latter points to the partial file's `.meta.json` sidecar. If metadata delivery itself fails, the retained data path is reported with the delivery error.

`--output-file` is rejected for `tool`, `workflow`, and `raw`; use `--output-dir` for those groups. Other groups may use either `--output-file` or `--output-dir`.

### 7.3 Automatic spill (`file_auto`)

For `tool exec` and `workflow exec`, automatic spill requires the output format to be JSON (`--output json`, either as the default or set explicitly); `--output jsonl` does not trigger automatic spill. When the estimated JSON envelope size exceeds 16 KiB and the output mode is `stdout` (not explicitly set), there is no `--jmes-filter`, and no `execution.artifact`/`execution.projection`, the complete envelope is automatically written to a file under `--output-dir`. The envelope stored in the file has `summary.deliveryMode=file_auto`. Default stdout is a delivery envelope with `data=null` and the path in `artifacts[].path`, without a business-result preview; human stdout retains the Chinese notice. If `--output-dir` is missing or unwritable, the command errors with `result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery`.

### 7.4 Deterministic file delivery

```bash
volclog --profile default --output json --output-mode file --output-dir ./out \
  tool exec project.describe-projects
```

### 7.5 Frozen output for interactive auth commands

`login`, `logout`, `sso`, and `configure sso` always emit their exact JSON result shape to stdout. `--output jsonl|table` may parse but does not change the frozen JSON shape (JSON is forced); do not rely on it to change the output. The following are rejected before any auth side effect runs: non-`stdout` `--output-mode`, `--output-file`, `--jmes-filter`, `--trace-dir`, and `--secrets-file`. `--output-dir` alone and `--trace-redact` without `--trace-dir` do not divert the frozen result.

## 8. Filtering, projection, and envelopes

### 8.1 `--jmes-filter` applies to the complete envelope

The CLI compiles and validates `--jmes-filter` before dispatching any request. An invalid expression therefore fails locally without a network call. After a response is received, the filter is applied to the complete envelope (including `status`, `summary`, `data`, `error`) for `raw`, `tool exec`, and `workflow exec`; for other groups, it is applied to the raw result value.

### 8.2 Null, missing path, and invalid expression

- If the filter resolves to an existing `null` value, the command succeeds and emits literal `null` to stdout or writes it to the requested file.
- If the filter path does not exist (missing key, out-of-range index), the command fails with a `filter matched no value` error and exit code `3`; file delivery does not create or overwrite the result file.
- If the filter expression is invalid, the command fails with `invalid jmes-filter expression` and exit code `3`.

### 8.3 Filtering into a file

For `raw`, `tool exec`, and `workflow exec`, combine `--jmes-filter` with `--output-mode file` to write the selected value from the complete success envelope as a single JSON document. `tool exec` and `workflow exec` also support requesting file delivery through context `execution.artifact`. Even with `--output jsonl`, the file is JSON, not JSONL; it can contain an array, scalar, or `null` rather than an envelope. The filter changes the file content only: default stdout remains a JSON delivery envelope, while human stdout retains the Chinese notice. Without a filter, ordinary file output retains the complete envelope; the unfiltered streamed-export exception above is unchanged.

The filter sees `summary.deliveryMode=file_forced`; in that input, `summary.totalBytes` measures the full envelope before filtering. The default stdout delivery envelope has its own `summary.totalBytes`, while `artifacts[].sizeBytes` measures the selected file. A global filter disables automatic spill. If the upstream operation fails in file mode, the complete error is emitted with a non-zero exit code; the error is not filtered into a file.

For example, select project names from the envelope and write them to `./out`:

```bash
volclog --profile default --output jsonl --output-mode file --output-dir ./out \
  --jmes-filter 'data.Projects[].ProjectName' \
  tool exec project.describe-projects --input '{"PageSize":20}'
```

Alternatively, project the raw result first, then select the envelope's `data` and request file delivery through the context:

```bash
volclog --profile default --output-dir ./out --jmes-filter 'data' \
  tool exec project.describe-projects --input '{"PageSize":20}' \
  --context '{"execution":{"projection":"Projects[].ProjectName","artifact":true}}'
```

The first example writes the project-name array as JSON. The second retains the existing projection wrapper: the array is under `data.items`, alongside server metadata such as `Total` when present; selecting `data` writes that object. Read the path from the default stdout envelope's `artifacts[].path` to open the file.

### 8.4 CLI envelope filtering vs `execution.projection`

`--jmes-filter` is compiled before the request and then evaluated as a CLI-level filter on the final envelope (or raw result for non-envelope groups) after the response is received.

`execution.projection` (in the `--context` JSON for `tool exec` and `workflow exec`) is compiled before the request and evaluated on the raw result after the response is received, before the CLI builds the envelope. It is not server-side. It uses the same JMESPath engine as `--jmes-filter` but operates on the raw result rather than the final envelope. The examples above use `data.Projects[].ProjectName` for the envelope filter and `Projects[].ProjectName` for the earlier raw-result projection; when both are used, the filter sees the already projected result under `data`.

Dynamic JSON is decoded and encoded with `json.Number` preserved across request bodies, service responses, envelopes, JSONL, and JMESPath evaluation. Large integers and long decimals therefore do not pass through a `float64` conversion and lose precision.

### 8.5 Envelope fields and error output

Success envelope: `status`, `action`, `requestId`, `summary` (`outputMode`, `deliveryMode`, `remoteOutcome`, `deliveryOutcome`, `dryRun`, `itemCount`, `totalBytes`, optional `pagination`/`completeness`/`tracePath`), `artifacts`, `data`, `error` (`null`).

Error envelope: `status` (`failed`), `action`, `requestId`, `summary`, `artifacts`, `data`, `error` (`source`, `code`, `message`, `requestId`, `statusCode`, `kind`, `hint`, optional `details`). `data` is normally `null`, but may contain retained partial results or batch-ingest progress; `artifacts` may describe a retained partial file.

Default `volclog` also emits a failed JSON envelope on stdout and exits nonzero for early usage failures, including no arguments, unknown flags or groups, and missing option values. Explicit help and `--version` remain text; use the `version` command for JSON. Text-mode `tool`/`workflow` discovery and the frozen JSON shapes and error contracts of `login`, `logout`, `sso`, and `configure sso` remain unchanged. Human-shortcut envelope groups retain their existing error contract. If stdout is broken, the CLI exits with code 3 and makes a best-effort structured report on stderr.

`summary.remoteOutcome` describes the business operation: `not_sent`, `unknown`, `success`, `failed`, or `partial`. `accepted` is reserved for a future operation with an explicit asynchronous contract. `summary.deliveryOutcome` (`success` or `failed`) separately describes result delivery. For `output_failed`, repair delivery and do not repeat the original write. For `result_unknown`, reconcile the remote state before considering another write. The CLI adds no retry in this change; existing `http.Client`/`Transport` policies still apply, including any behavior of a custom transport.

For batch-ingest `partial_failure`, inspect `summary.progress` or `data.progress`: `confirmedBatches`, `confirmedLogs`, `confirmedLogOffset` (zero-based exclusive), `currentBatch` (one-based when a batch is active), `currentBatchOutcome`, and `lastConfirmedRequestId`. Only a confirmed successful response increments the counts. A read failure or invalid response after HTTP 200 is `unknown`; an unknown batch is not a safe automatic resume point. The current `requestId` and `statusCode` describe the current attempt and are not inherited from a previous confirmed batch.

The standard catalog JSON response path requires HTTP 200; a non-empty body must be valid JSON. Empty responses and special codec, `raw`, and protobuf paths retain their existing 2xx/text/empty/PB compatibility contracts. Errors can use `not_found`, `conflict`, `throttled`, `protocol`, and typed `validation` kinds. Exit codes 1, 2, and 3 retain their existing roles; a transient failure alone does not make retry safe. If stdout delivery fails, the CLI returns nonzero and makes a best-effort structured report on stderr; broken stdout cannot guarantee JSON output.

## 9. Trace and diagnostics

### 9.1 Trace directory and redaction

`--trace-dir <dir>` enables tracing. When the CLI creates new trace paths, it requests `0700` for the directory and `0600` for the JSONL file named `trace-<UTC timestamp>.jsonl`. Existing directories and files may retain their current permissions. Trace events include `http_request`, `http_response`, and `plan`.

Each trace event records the event type, and where applicable: the HTTP method and path, the response status, the Request ID, and the elapsed time in milliseconds. Normal structured request/response trace fields contain only header keys (never values), query keys (never values), and SHA-256 hashes of request and response bodies (never raw bytes). `Authorization` and `X-Security-Token` are always included in the redacted header key list.

The `error_message` field stores the transport error string directly. A transport error can include the full request URL and query values. Never put credentials or other secrets in query parameters. Treat trace files as sensitive diagnostic artifacts: keep their permissions restricted and share them only after inspection.

`--trace-redact` accepts `on` (default) and `off`, plus aliases (`true`/`1`/`yes`/`enabled`/`strict`/`default` map to `on`; `false`/`0`/`no`/`disabled` map to `off`). Unrecognized values default to `on`. `--trace-redact off` is accepted and normalized but does not disable the forced structured-field redaction and does not emit raw header, query, or body values.

### 9.2 Doctor and Request ID

`doctor` checks configuration and credentials locally; `doctor --online` performs a minimal live connectivity check against the TLS endpoint.

```bash
volclog --profile default doctor
volclog --profile default doctor --online
```

The `requestId` in the envelope (or `x-tls-requestid` response header) identifies the request for support and troubleshooting.

## 10. Human shortcuts and next steps

`volclog-human` shortcuts provide a shorter path for common interactive tasks. They reuse shared authentication, transport, envelope, and tracing infrastructure, but their parameters, contract validation, table support, and file/stdout behavior can differ from `tool`/`workflow`/`raw`. See section 7 for output behavior.

```bash
volclog-human project list --output table
volclog-human topic create --describe
volclog-human index create --print-request-template=required > index_req.json
volclog-human index create --topic-id 'YOUR_TOPIC_ID' --request file://index_req.json
```

For the complete human shortcut introduction, see [Human Shortcuts](7-Human-Shortcuts.md). For longer automation-oriented workflows, see the [Practical Guide](5-Practical-Guide.md); for advanced topics, see [Advanced](6-Advanced.md).

---

[← Previous: Configuration](3-Configuration.md) | [中文](4-Usage_zh.md) | [Next: Practical Guide →](5-Practical-Guide.md)
