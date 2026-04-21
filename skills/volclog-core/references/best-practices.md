# Best Practices

Use this file only for runtime semantics, recovery posture, and credential handling. Routing belongs in `routing.md`; multi-step execution order belongs in `sops.md`.

## Runtime Signals

Read these signals in this order:

1. choose the surface with `tool / workflow / raw`
2. let CLI decide how the result is delivered
3. inspect the flat `error` object only if the call failed

Key rules:

- Keep surface choice and delivery choice separate. Skill decides `tool / workflow / raw`; CLI decides stdout vs file delivery.
- If both binaries are available, prefer `volclog` for agent or CI sessions.
- Default `volclog` only exposes readonly agent actions. Switch to `volclog-human` before create/modify/delete/import or local ingest.
- `volclog-human` keeps the human shortcut layer; do not let that change the runtime reading order described here.
- Let CLI `deliveryMode` decide stdout vs `file_auto`.
- For `log.search`, let CLI `deliveryMode` decide stdout vs `file_auto` after the surface is already chosen.
- Treat `outputMode` as caller intent and `deliveryMode` as runtime result:
  - explicit file request -> `deliveryMode=file_forced`
  - auto spill because the envelope is too large -> `deliveryMode=file_auto`
  - normal stdout -> `deliveryMode=stdout`
- For large responses, prefer `--output-mode file --output-dir <writable-dir>`.
- If auto spill fails because no writable directory was provided, rerun with `--output-dir <writable-dir>` instead of inventing a filename.
- `tool / workflow / raw` write a full JSON envelope when file delivery is used, even if stdout format preference was `jsonl`.
- `log.export` and `log.export-analysis` are the exception: their files contain exported data rows, while stdout keeps the envelope and points to the file through `artifacts`.

## Envelope Filtering

- `--jmes-filter` runs on the complete CLI envelope.
- `--jmes-filter` is stdout-only; do not combine it with file delivery.
- `execution.projection` is different: it runs on the raw result before envelope wrapping.
- Use `--jmes-filter` for envelope fields such as `data`, `summary`, or `error`.
- Use `execution.projection` only for raw service-result fields.
- If `--jmes-filter` targets an existing envelope field whose value is `null`, stdout returns literal `null` and the command still succeeds.
- `filter matched no value` means the path was wrong, not that a null field was returned.
- `filter matched no value` and `invalid --jmes-filter` are `decode` failures and return exit 3.
- If the original envelope already failed, keep the original non-zero exit and `error.kind`; a filter miss only adds a warning instead of replacing the failure.

## Search, Histogram, And Analysis

Use this section to interpret result semantics after the surface has already been chosen.

- `log.search` itself supports plain search and SQL/analysis queries in `Query`.
- Use `log.search` for interactive analysis, small previews, and quick iteration.
- `log.describe-histogram-v1` is for time-distribution preview and total-hit estimation only for pure search queries.
- `log.export-analysis` uses the same SearchLogs SQL/analysis `Query` syntax as `log.search`; it does not add a new server-side analysis API or another pagination model.
- Choose `log.export-analysis` when the user needs the full analysis row set written as an export file for offline reading, downstream processing, or token-safe handoff.
- Stay on `log.search` when the user is still validating SQL, iterating on the query, or only needs a small interactive preview.
- `raw --input` is only a compatibility alias for `--body`; it does not do `tool exec` style smart mapping for GET requests.
- `raw --dry-run` only validates the transport/local plan. It does not validate API-required fields the way `tool exec` or `workflow exec` can.
- In default `volclog`, mutating/non-readonly `raw` method/path pairs are rejected before transport execution. Treat that as a surface-selection issue and switch to `volclog-human`.
- `HitCount` is only the count returned in the current `SearchLogs` response window.
- `Histogram.TotalCount` is the better whole-window total only for pure search when histogram is the correct surface.
- `ResultStatus=incomplete` means the service returned only a partial scan. This can happen for SearchLogs in both search and analysis mode, so narrow the time range and rerun before trusting counts, rows, empty results, or bucket distribution.
- For search+analysis or pure analysis queries, do not use histogram as the analysis row count. Use SQL such as `select count(*)` when the user asks for analysis totals.

## Error Object

Failed envelopes use one flat `error` object.

Read it in this order:

1. `error.source`
2. `error.kind`
3. `error.code`
4. `error.message`
5. `error.details`
6. `error.requestId`
7. `error.statusCode`

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

## Thin Client Boundary

The CLI validates contract shape and runtime preconditions. It does not validate business semantics for you.

What the CLI does validate:

- required fields and JSON shape
- conflicting runtime selectors or output flags
- local output path / writable directory constraints

What the CLI does not try to judge:

- whether a timestamp is business-valid
- whether a query is semantically reasonable
- whether an empty result is surprising for the workload

If the command reached the service without a local validation/decode/filesystem error, debug the business query and time window before assuming the CLI blocked something.

## Recovery Signals

When the command already failed, follow the smallest next action:

- `403 Forbidden`: check tenant/profile alignment first, then verify resource permissions before retrying.
- `401 Unauthorized`: run `volclog doctor`, then inspect credentials, region, and endpoint.
- `IndexNotExists`: inspect or create the index, then wait for readiness before retrying search.
- `ResultStatus=incomplete`: narrow the time range and rerun before trusting the result.
- Empty search after write: confirm profile, topic, time range, and index readiness before widening the query.

## Known Traps

- `page.all` increases completeness and payload size; it is not a compression flag, and it only applies when the contract reports `execution.supports_all=true`.
- `workflow` ids such as `log.ingest`, `log.export`, and `log.export-analysis` are workflow identities, not tool ids.
- `readonly edition` errors mean the default `volclog` binary blocked a mutating/import path by design; switch to `volclog-human` instead of retrying the same command.
- Human shortcut groups are for humans. Agent flows should stay on `tool / workflow / raw`.
