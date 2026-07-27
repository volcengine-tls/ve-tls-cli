# 4. Usage

[← Previous: Configuration](3-Configuration.md) | [中文](4-Usage_zh.md) | [Next: Practical Guide →](5-Practical-Guide.md)

This guide covers how to run commands with `volclog`: the command surface, how to discover tools and workflows, how `tool exec`, `workflow exec`, and `raw` work, how context and selectors interact, and how output, filtering, and tracing behave. For authentication and configuration details, see [Authentication](2-Authentication.md) and [Configuration](3-Configuration.md).

## 1. Command architecture and edition boundary

The default `volclog` command exposes these groups: `configure`, `doctor`, `skill`, `tool`, `workflow`, `raw`, `login`, `logout`, and `sso`.

`volclog-human` adds a human shortcut layer (`project`, `topic`, `metric-topic`, `index`, `log`, `host-group`, `collector`) alongside `tool`/`workflow`/`raw`. The shortcuts reuse shared authentication, transport, envelope, and tracing infrastructure, but have their own shortcut argument parsing and request construction. Contract validation, table support, and file/stdout behavior can differ from `tool`/`workflow`/`raw`. Users and automation must follow the documented contract for the selected command surface.

Choose the right surface:

- Use `tool` when you know the API action and want contract validation and a structured envelope.
- Use `workflow` when you want CLI-provided higher-level orchestration over one or more tools.
- Use `raw` only when you already know the exact method and path and need a direct transport call.
- Use `volclog-human` shortcuts only for interactive terminal work; agents and CI should default to `tool`/`workflow`/`raw`.

## 2. Discover before execution

Before running a tool or workflow, inspect its contract so you know the required input and the request shape.

### 2.1 `tool list` and `tool describe`

```bash
volclog tool list
volclog tool list project
volclog tool list --verb create --format json
volclog tool describe project.describe-projects
volclog tool describe project.create --view full
```

`tool list` takes the group as a positional argument (not a `--group` flag) and supports `--verb` to filter by verb and `--format text|json` (default `text`). `tool describe <group.action>` shows the tool's identity, input schema, context schema, execution schema, behavior, output policy, and contract digest. The `--view` flag selects `compact` (default) or `full`; explicit JSON output defaults to `full`.

### 2.2 `workflow list` and `workflow describe`

```bash
volclog workflow list
volclog workflow list log
volclog workflow list --format json
volclog workflow describe log.export
```

`workflow list` takes the group as a positional argument and supports `--format text|json`. `workflow describe <group.command>` shows the workflow's kind, source, input schema, context schema, execution schema, recommended global flags, and guidance.

## 3. `tool exec`

`tool exec` runs a single tool by its `<group.action>` identity.

```bash
volclog --profile default tool exec project.describe-projects
```

### 3.1 Input and context

`--input` supplies the request body. It accepts `file://<path>`, `-` (stdin), or an inline JSON object (must start with `{`). `--context` supplies runtime and execution controls using the same sources.

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

The identity selectors are: global `--profile`, global `--secrets-file`, `context.profile`, and `context.secrets_file`. The conflict rules are:

- Any profile selector (global `--profile` or `context.profile`) combined with any secrets-file selector (global `--secrets-file` or `context.secrets_file`) conflicts.
- Global `--secrets-file` combined with `context.secrets_file` conflicts.
- Global `--profile` combined with `context.profile` conflicts only when the profile names differ. Repeating the same profile name in both places is accepted but redundant; avoid it for clarity.

A conflict fails with `conflicting runtime selectors` (or `conflicting profile selectors` when two different profile names are supplied).

There are no global CLI `--region` or `--endpoint` flags. `context.region` and `context.endpoint` are per-execution fallback defaults (not identity selectors) available only through `tool`/`workflow` context; they take precedence over project defaults but do not override a non-empty selected-profile value or a dynamic environment value. The full region/endpoint/timeout precedence is detailed in [Configuration](3-Configuration.md).

### 6.2 `--dry-run` and context dry-run

`--dry-run` is a global flag that works for `raw`, `tool exec`, and `workflow exec`. For `tool exec` and `workflow exec`, `execution.dry_run` in the `--context` JSON has the same effect. `--dry-run` is rejected for other groups.

### 6.3 Write-operation safety

For write operations, run with `--dry-run` first to validate the request locally before sending it to TLS. A dry-run plan reports whether local checks (profile, endpoint, region, body JSON) passed.

For provider-specific login, refresh, and logout behavior, see [Authentication](2-Authentication.md). For profile and runtime precedence, see [Configuration](3-Configuration.md).

## 7. Output and delivery

### 7.1 Output values

`--output` accepts `json` (default) and `jsonl` on the default `volclog` agent path. `table` is rejected on the default path; it is supported only by `volclog-human` and only for specific shortcut surfaces (`project`/`topic`/`metric-topic` list and get, `index get`, `log search`). `text` and `raw` are not accepted.

`--output-mode` accepts `stdout` (default) and `file`. Any other value is rejected.

### 7.2 stdout vs file delivery

By default, results are written to stdout. With `--output-mode file` (forced file mode), the output contract depends on the command surface:

- For `raw`, `tool exec`, and `workflow exec`, the complete envelope is written to an artifact file under `--output-dir`. stdout contains only the fixed Chinese file notice (`结果已写入文件。\n文件: <path>\n`), not the envelope. Automation must read the artifact path from the notice and read the file.
- For non-envelope command groups, the raw result is written to the selected file and stdout emits only the bare file path.
- For generic human-shortcut envelope commands (for example `project list`, `topic create`), the envelope artifact is written to the file and the complete envelope is also emitted to stdout.
- For human `volclog-human log export` / `log export-analysis`, exported data rows are streamed into the file artifact (JSON array for `--output json`, JSONL rows for `--output jsonl`) and stdout emits the complete envelope containing artifact metadata. The artifact itself is not an envelope.

Automation must follow the output contract of the command surface it invokes; the fixed Chinese notice rule applies specifically to `raw`, `tool exec`, and `workflow exec`. Human log exports do not use the fixed notice.

Exception: default `workflow exec log.export` and `log.export-analysis` stream exported rows directly into the data artifact (JSON array for `--output json`, JSONL rows for `--output jsonl`) rather than writing an envelope. The data artifact is not an envelope and does not contain envelope metadata. stdout still contains only the fixed file notice with the data-artifact path. See [Advanced](6-Advanced.md) section 2 for details.

`--output-file` is rejected for `tool`, `workflow`, and `raw`; use `--output-dir` for those groups. Other groups may use either `--output-file` or `--output-dir`.

### 7.3 Automatic spill (`file_auto`)

For `tool exec` and `workflow exec`, automatic spill requires the output format to be JSON (`--output json`, either as the default or set explicitly); `--output jsonl` does not trigger automatic spill. When the estimated JSON envelope size exceeds 16 KiB and the output mode is `stdout` (not explicitly set), there is no `--jmes-filter`, and no `execution.artifact`/`execution.projection`, the complete envelope is automatically written to a file under `--output-dir`. The envelope stored in the file has `summary.deliveryMode=file_auto`. stdout emits only the fixed file notice (`结果过大，已写入文件。\n文件: <path>\n`); the internal preview is not printed to stdout. Automation must read the artifact path from the notice and read the file. If `--output-dir` is missing or unwritable, the command errors with `result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery`.

### 7.4 Deterministic file delivery

```bash
volclog --profile default --output json --output-mode file --output-dir ./out \
  tool exec project.describe-projects
```

### 7.5 Frozen output for interactive auth commands

`login`, `logout`, `sso`, and `configure sso` always emit their exact JSON result shape to stdout. `--output jsonl|table` may parse but does not change the frozen JSON shape (JSON is forced); do not rely on it to change the output. The following are rejected before any auth side effect runs: non-`stdout` `--output-mode`, `--output-file`, `--jmes-filter`, `--trace-dir`, and `--secrets-file`. `--output-dir` alone and `--trace-redact` without `--trace-dir` do not divert the frozen result.

## 8. Filtering, projection, and envelopes

### 8.1 `--jmes-filter` applies to the complete envelope

For `raw`, `tool exec`, and `workflow exec`, `--jmes-filter` is applied to the complete envelope (including `status`, `summary`, `data`, `error`). For other groups, it is applied to the raw result value.

### 8.2 Null, missing path, and invalid expression

- If the filter resolves to an existing `null` value, the command prints literal `null` and succeeds.
- If the filter path does not exist (missing key, out-of-range index), the command fails with a `filter matched no value` error and exit code `3`.
- If the filter expression is invalid, the command fails with `invalid jmes-filter expression` and exit code `3`.

### 8.3 Incompatibility with file delivery

`--jmes-filter` cannot be combined with `--output-mode file` for `raw`, `tool exec`, and `workflow exec`. For other groups, the combination is allowed.

### 8.4 CLI envelope filtering vs `execution.projection`

`--jmes-filter` is a CLI-level filter applied to the final envelope (or raw result for non-envelope groups) after the response is received.

`execution.projection` (in the `--context` JSON for `tool exec` and `workflow exec`) is a CLI-local JMESPath projection applied to the raw result after the response is received and before the CLI builds the envelope. It is not server-side. It uses the same JMESPath engine as `--jmes-filter` but operates on the raw result rather than the final envelope.

### 8.5 Envelope fields and error output

Success envelope: `status`, `action`, `requestId`, `summary` (`outputMode`, `deliveryMode`, `dryRun`, `itemCount`, `totalBytes`, optional `pagination`/`tracePath`), `artifacts`, `data`, `error` (`null`).

Error envelope: `status` (`failed`), `action`, `requestId`, `summary`, `artifacts` (empty), `data` (`null`), `error` (`source`, `code`, `message`, `requestId`, `statusCode`, `kind`, `hint`, optional `details`).

Structured error envelopes are used by `raw`, all `tool` commands (including `list`/`describe`), all `workflow` commands (including `list`/`describe`), and the human-shortcut envelope groups (`project`, `topic`, `metric-topic`, `index`, `log`). Other non-envelope groups write flat JSON to stderr in this field order: `errorCode`, `errorMessage`, `requestId`, `statusCode`, `kind`, `hint`.

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
volclog-human index create --topic-id <topic-id> --request file://index_req.json
```

For the complete human shortcut introduction, see [Human Shortcuts](7-Human-Shortcuts.md). For longer automation-oriented workflows, see the [Practical Guide](5-Practical-Guide.md); for advanced topics, see [Advanced](6-Advanced.md).

---

[← Previous: Configuration](3-Configuration.md) | [中文](4-Usage_zh.md) | [Next: Practical Guide →](5-Practical-Guide.md)
