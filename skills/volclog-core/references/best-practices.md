# Best Practices

Use this file only for runtime semantics, recovery posture, and credential handling. Routing belongs in `routing.md`; multi-step execution order belongs in `sops.md`.

Do not use this file to reopen surface selection once routing is already clear.
This file refines the quick recovery map in `SKILL.md`; it does not override the main skill's default first response.

## Contents

- [Runtime Signals](#runtime-signals)
- [Contract View And Recovery Precedence](#contract-view-and-recovery-precedence)
- [Invocation Controls](#invocation-controls)
- [Dry-Run Scope](#dry-run-scope)
- [Envelope Filtering](#envelope-filtering)
- [Search, Histogram, And Analysis](#search-histogram-and-analysis)
- [Error Object](#error-object)
- [Profile And Credential Selection](#profile-and-credential-selection)
- [Doctor Boundary](#doctor-boundary)
- [Thin Client Boundary](#thin-client-boundary)
- [Recovery Signals](#recovery-signals)
- [Known Traps](#known-traps)

## Runtime Signals

Read these signals in this order after routing is already clear:

1. keep the chosen `tool / workflow / raw` surface fixed
2. let CLI decide how the result is delivered
3. inspect the flat `error` object only if the call failed

Key rules:

- Keep surface choice and delivery choice separate. Skill decides `tool / workflow / raw`; CLI decides stdout vs file delivery.
- Do not let shortcut-oriented help or examples change the runtime reading order described here.
- Let CLI `deliveryMode` decide stdout vs `file_auto`.
- For `log.search`, let CLI `deliveryMode` decide stdout vs `file_auto` after the surface is already chosen.
- Treat `outputMode` as caller intent and `deliveryMode` as runtime result:
  - explicit file request -> `deliveryMode=file_forced`
  - auto spill because the envelope is too large -> `deliveryMode=file_auto`
  - normal stdout -> `deliveryMode=stdout`
- For large responses, prefer `--output-mode file --output-dir <writable-dir>`.
- If auto spill fails because no writable directory was provided, inspect `remoteOutcome` and repair delivery. Do not repeat a successful write; when a repeatable read may be rerun, provide `--output-dir <writable-dir>` instead of inventing a filename.
- On successful file delivery, `tool / workflow / raw` normally write the full JSON envelope; with `--jmes-filter` they write the selected value as a single JSON document, even when `--output jsonl` is set. Default stdout is a JSON delivery envelope with `data=null` and the file path in `artifacts[].path`; human stdout retains the Chinese notice. Migrate scripts that parsed the default edition's old notice to the artifact path.
- The delivery envelope's `summary.itemCount` retains the business-result count, `summary.totalBytes` is the stdout envelope's byte length, and `artifacts[].sizeBytes` is the actual file size.
- Without `--jmes-filter`, `workflow exec log.export` and `log.export-analysis` are the exception: their files contain streamed data rows, while default stdout contains the JSON delivery envelope. A filter disables the streamed writer, so the results are accumulated in memory before filtering. For large exports, prefer unfiltered streaming or server-side field selection where available; client-side filtering does not reduce server scanning or network traffic.

## Contract View And Recovery Precedence

- Use compact `tool describe` output for discovery and ordinary flat requests.
- Run `volclog tool describe <group.action> --view full` before authoring nested objects or whenever an expected optional field is absent from compact output.
- Treat full view as the request-authoring schema; do not infer omitted optional fields from compact view.
- Read `risk`, `recovery`, `recovery_source`, `recovery_notes`, and `usage_constraints` before executing or recovering an operation. These operation-specific fields override the generic recovery tables in this skill.
- `reviewed-read-contract` means the operation was reviewed as read or non-persistent; it does not prove service idempotency or permit automatic retry. For `unverified`, reconcile the outcome before retrying.
- Full describe includes `input_policy`, `doc_url`, `doc_url_source`, and `source`; `doc_url` is `null` if no exact URL is verified. `identity.api_version_default` is the client default header (`0.3.0`), with `identity.api_version_source` indicating its source, not a per-operation version guarantee. Unknown `identity.maintainer` and `identity.deprecated` are `null` in full view; compact may omit details.
- `tool exec` applies the declared schema to both flat and sectioned inputs. Sectioned extra top-level keys are rejected; section fields follow `additionalProperties`, whose absence permits extra fields. An operation without a section schema remains passthrough, subject to input and context shape rules. Schema defaults are not injected; human shortcuts retain legacy validation.
- Interpret `high-risk-retry` as “retrying is high risk,” not as permission to retry automatically.
- After an ambiguous high-risk result, use a get/describe/list reconciliation action only when the contract names one. If it does not, stop and escalate with the request ID and business error evidence.

## Invocation Controls

- Non-TTY means non-interactive automatically. `--non-interactive` or `context.execution.non_interactive=true` explicitly enables it; `false` cannot cancel non-TTY mode and conflicts with a global `true`.
- `--timeout 30s` and `context.execution.timeout="30s"` set a positive Go-duration budget for the entire invocation, starting at command entry and shared across pages/batches. There is no overall default; profile `timeout_seconds` remains a separate HTTP request timeout. Equal global/context durations may repeat; unequal ones conflict.
- TLS writes and unknown raw operations, including unknown GETs, require a CLI write permit. For an already user-authorized action, use `--allow-write`; `tool exec` / `workflow exec` may instead use `context.execution.allow_write=true`, but raw has no context. This does not grant IAM permissions. Global `true` conflicts with explicit context `false`. Reviewed reads such as POST `log.search` and dry-runs are exempt; human write shortcuts are gated too.
- `--trace-redact` accepts `on`/`true`/`1`/`yes`/`enabled`/`strict`/`default`; `off`/`false`/`0`/`no`/`disabled` and unknown values fail validation. Context `trace=false` or `enabled:false` disables global tracing.
- Non-interactive `login` uses a device code without opening a browser. If session replacement needs confirmation it returns `interaction_required`; select another `--profile` or an interactive terminal, not `--force`. SSO auto-selects one account/role but fails immediately when multiple candidates need an unspecified choice. The transactional token is rolled back and config changes are not committed; registration cache may already have been written.
- Darwin/Linux interactive prompts use cancellable polling. Windows consoles without read-deadline support return `interaction_required`, so provide explicit selectors to avoid prompting. Runtime verification of this change has been on Darwin only.
- Cancellation by SIGINT, SIGTERM, or `RunContext` shares the invocation budget: before send it is `not_sent`, and after send an unresolved outcome remains `unknown`. Keep partial results and follow completeness and reconciliation guidance.

## Dry-Run Scope

- `--dry-run` applies to `raw`, `tool exec`, and `workflow exec`. It does not apply to `tool describe`, `workflow describe`, or shortcut groups.
- For `tool exec` and `workflow exec`, `--dry-run` validates contract shape and execution plan without sending the real mutating call.
- For `raw`, `--dry-run` only validates the transport/local plan. It does not validate API-required fields the way `tool exec` or `workflow exec` can.
- Treat any dry-run success as transport or contract confidence only. It is not proof that the business query or time window is semantically correct.
- `workflow exec` rejects context `execution.page` and `execution.page_all`, including `false`; use the workflow's documented controls instead.

## Envelope Filtering

- `--jmes-filter` runs on the complete CLI envelope and can be combined with `--output-mode file`. `tool exec` and `workflow exec` also support context `execution.artifact` for file delivery; `raw` does not accept `--context`.
- In file mode the selected success value, which can be an array, scalar, or `null`, is the JSON file content; read the path from the default stdout envelope's `artifacts[].path` or the human notice. Without a filter, ordinary file delivery retains the complete envelope.
- In the file-mode filtering view, `summary.deliveryMode=file_forced`; `summary.totalBytes` measures the envelope before filtering. The default stdout delivery envelope's own `summary.totalBytes` measures its bytes, and `artifacts[].sizeBytes` measures the selected file. A global filter disables automatic spill.
- `execution.projection` is different: it runs on the raw result before envelope wrapping. For project names, use `data.Projects[].ProjectName` as `--jmes-filter` and `Projects[].ProjectName` as `execution.projection`; the final filter sees any earlier projection under `data`.
- Use `--jmes-filter` for envelope fields such as `data`, `summary`, or `error`.
- Use `execution.projection` only for raw service-result fields.
- If `--jmes-filter` targets an existing envelope field whose value is `null`, the command succeeds and emits or writes literal `null`.
- `filter matched no value` means the path was wrong, not that a null field was returned; in file mode the result file is not created or overwritten.
- An invalid `--jmes-filter` is a preflight `decode` failure with exit 3 and `remoteOutcome=not_sent`. A filter miss after business success returns `output_failed`, stage `projection`, exit 3, and `remoteOutcome=success`; repair delivery without repeating the write.
- If the original envelope already failed, keep the original non-zero exit and `error.kind`. In file mode emit the complete error without filtering it into a file; for stdout filtering, a filter miss only adds a warning instead of replacing the failure.

## Search, Histogram, And Analysis

Use this section to interpret result semantics after the surface has already been chosen.

- `log.search` itself supports plain search and SQL/analysis queries in `Query`.
- Use `log.search` for interactive analysis, small previews, and quick iteration.
- `log.describe-histogram-v1` is for time-distribution preview and total-hit estimation only for pure search queries.
- `log.export-analysis` uses the same SearchLogs SQL/analysis `Query` syntax as `log.search`; it does not add a new server-side analysis API or another pagination model.
- Choose `log.export-analysis` to write analysis rows to an export file for offline reading, downstream processing, or token-safe handoff; inspect completeness rather than assuming all rows were returned.
- Stay on `log.search` when the user is still validating SQL, iterating on the query, or only needs a small interactive preview.
- `raw --input` is only a compatibility alias for `--body`; it does not do `tool exec` style smart mapping for GET requests.
- `HitCount` is only the count returned in the current `SearchLogs` response window.
- `Histogram.TotalCount` is the better whole-window total only for pure search when histogram is the correct surface.
- For SearchLogs, `ResultStatus=incomplete` or `time_out` means incomplete data and `error` means failure. SQL/analysis is complete only with `ResultStatus=complete`; `ListOver` applies to pure search. Inspect `summary.completeness` before trusting counts, rows, empty results, or bucket distribution.
- Explicit SQL `LIMIT`/`OFFSET` calls are independent queries without a shared-snapshot guarantee; do not invent an offset to resume an incomplete response.
- For search+analysis or pure analysis queries, do not use histogram as the analysis row count. Use SQL such as `select count(*)` when the user asks for analysis totals.

## Error Object

Failed envelopes use one flat `error` object.

Read it in this order:

1. `error.source`
2. `error.kind`
3. `error.hint`
4. `error.code`
5. `error.statusCode`
6. `error.message`
7. `error.details`
8. `error.requestId`

Interpretation rules:

- For upstream service failures, `error.code` is already the business error code such as `ProjectAlreadyExist`.
- If the service embeds extra JSON details, the CLI lifts them into `error.details`; do not parse `error.message` again unless `details` is absent.
- Use `error.kind` to choose the smallest recovery action:
  - `validation`: fix input or selector shape first
  - `unsupported_feature`: remove the unsupported capability, or switch surface
  - `incompatible_flags`: remove one of the conflicting flags and rerun
  - `filesystem`: fix `--output-dir`, path, or local permissions before retrying

## Profile And Credential Selection

Credential selection is a host/runtime concern, not a business-input concern.

- Discover saved profiles with `volclog configure list`.
- Omit `profile` to use the active CLI profile.
- Set `context.profile` only when switching tenant or environment inside one session.
- For stateless service assistants and CI, keep long-lived credentials on the host. Let the host choose the local profile, then inject one-shot credentials with `--secrets-file` or `context.secrets_file`.

Preferred order:

1. non-secret selector such as profile or environment label
2. `--secrets-file`
3. `context.secrets_file`
4. process-scoped `VOLCENGINE_ACCESS_KEY_ID` / `VOLCENGINE_ACCESS_KEY_SECRET` only as fallback

Hard rule:

- Environment credentials override profile resolution. If a stateless run must target a specific local profile, use host-generated secrets files instead of sandbox-wide env injection.
- `--profile`/`context.profile` and `--secrets-file`/`context.secrets_file` are mutually exclusive runtime selectors; conflicting selectors fail fast instead of silently overriding each other.

## Doctor Boundary

- `volclog doctor` is for host/runtime diagnosis: credentials, config, selector visibility, endpoint reachability, and similar local setup issues.
- Use `volclog doctor` for `error.kind=config`, and for stubborn `401`/credential problems after selector and endpoint checks.
- Do not use `volclog doctor` as a replacement for `tool describe` or `workflow describe`.
- Do not expect `volclog doctor` to prove that a business query, time window, or SQL expression is semantically correct.

## Thin Client Boundary

The CLI validates contract shape and runtime preconditions. It does not validate business semantics for you.

What the CLI does validate:

- declared request types, enums, bounds, required fields, conditional requirements, and JSON shape on `tool exec`
- conflicting runtime selectors or output flags
- local output path / writable directory constraints

What the CLI does not try to judge:

- whether a timestamp is business-valid
- whether a query is semantically reasonable
- whether an empty result is surprising for the workload

If the command reached the service without a local validation/decode/filesystem error, debug the business query and time window before assuming the CLI blocked something.

## Recovery Signals

When the command already failed, follow the smallest next action:

| Signal | Smallest next action |
| --- | --- |
| `unknown tool` | Re-run `tool list` or `workflow list` for the group before guessing aliases or fallback names. |
| `missing --input` | Re-read `input_schema` and `input_encoding_hint`, then resend inline JSON, stdin, or file input using the documented shape. |
| `filter matched no value` | Inspect one unfiltered response first, then correct the `--jmes-filter` or `execution.projection` path. |
| `jmes filter returned literal null` | Treat literal `null` as a successful projection result; only debug further if the command returned a real filter error instead. |
| `401 Unauthorized` | Run `volclog doctor`, then inspect credentials, region, and endpoint. |
| `403 Forbidden` | Check tenant/profile alignment first, then verify resource permissions before retrying. |
| `error.kind=server` or `5xx` | Read `error.code`, `error.requestId`, and `error.statusCode` first. Retry with backoff only when the operation contract permits it and the failure is clearly transient; otherwise reconcile or escalate with those identifiers. |
| `IndexNotExists` | Inspect or create the index, then wait for readiness before retrying search. |
| `TopicAlreadyExist` | Re-list topics in the target project, then switch to get or modify instead of repeating create. |
| `ProjectAlreadyExist` | Re-list projects, then switch to get or modify instead of repeating create. |
| `ResultStatus=incomplete` / `time_out` / `error` | Inspect completeness and the error; do not present partial rows as complete. |
| `search returned empty after write` | Confirm selector, topic, time range, and index readiness before widening the query. |
| `huge stdout payload` | Rerun with `--output-mode file --output-dir <writable-dir>`, or reduce stdout with `--jmes-filter` or `execution.projection`. |
| `unsupported_feature` | Remove the unsupported capability such as `page.all`, or switch to a surface whose contract explicitly supports it. |
| `filesystem` | Supply a writable `--output-dir`, or fix the local path and permissions before retrying. |

## Known Traps

- `cli-write-gate-is-not-iam`: enable `--allow-write` only for a user-authorized write or unknown raw operation; IAM authorization is separate, and reviewed reads and dry-runs need no flag.
- `invocation-timeout-is-not-http-timeout`: set the optional whole-invocation Go duration independently of the profile's per-request HTTP timeout; it does not reset between pages or batches.
- `noninteractive-auth-requires-explicit-selection`: use another profile or interactive terminal for session replacement, and supply the SSO account or role when multiple candidates require a choice.
- `trace-redaction-cannot-be-disabled`: use `on` or an enabled alias; `off` and unknown values fail validation. To disable tracing, set context `trace=false` or `enabled=false`.
- `page-all-is-not-compression`: `page.all` accumulates pages and can increase payload size; `merged` does not prove completeness. Check `summary.completeness` and retained data, including on nonzero exit.
- `jmes-filter-and-projection-have-different-scope`: use `summary.*` or `data.*` under `--jmes-filter`, and raw keys only under `execution.projection`.
- `jmes-filter-null-is-still-success`: literal `null` means the selected field exists and is null; that is not the same as `filter matched no value`.
- `jmes-filter-file-contains-selected-json`: selected values are JSON documents; use the default stdout envelope's `artifacts[].path` (or the human notice) to locate the file and do not assume `summary` remains in its content.
- `deliverymode-belongs-to-runtime`: choose `tool / workflow / raw` first; when the envelope is retained, read `summary.deliveryMode` from the actual result.
- `hitcount-is-not-whole-window-total`: `HitCount` is only the current response window, not the whole-window total.
- `incomplete-means-partial-scan`: treat `ResultStatus=incomplete` or `time_out` as partial evidence and `error` as failure.
- `workflow-ids-are-not-tool-ids`: `log.ingest`, `log.export`, and `log.export-analysis` belong to `workflow`, not `tool`.
- `ingest-is-not-tool-put`: use `workflow log.ingest` for local file or stdin import, and `tool log.put` only for the public PutLogs contract.
- `shortcuts-are-human-first`: human shortcut groups are for humans; agent flows should stay on `tool / workflow / raw`.
- `thin-client-does-not-judge-business-semantics`: if there was no local validation, decode, or filesystem error, debug the query semantics and time window before blaming the CLI.
- `env-creds-override-profile`: process-wide environment credentials override local profile resolution.
- `profile-and-secrets-file-are-exclusive`: choose exactly one runtime selector family, not both `profile` and `secrets_file`.
- `high-risk-retry-is-not-auto-retry`: ambiguous high-risk outcomes require contract-named reconciliation or escalation, never an automatic replay.
- `compact-describe-can-omit-optional-fields`: use `tool describe --view full` before authoring nested optional configuration.
- `topic-resolution-is-logapp-only`: if `app.resolve-topic-ids` reports `unsupported_feature`, follow its hint and use `app.resolve-resources`; do not guess Topic IDs for opaque resources.
