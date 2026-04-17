# Best Practices

## Surface Choice: tool / workflow / raw

- If both binaries are available, prefer `volclog-agent` for agent or CI runs. Use the full `volclog` binary only when a human explicitly needs shortcut-oriented interaction.
- Use `tool` for public API discovery and execution.
- Use `workflow` for CLI-owned orchestration such as `log.ingest`, `log.export`, and `log.export-analysis`.
- Use `raw` only when exact method/path is already known or the public contract surface cannot express the need.
- If you are unsure between `tool` and `workflow`, inspect both with `describe` before constructing input. Do not guess ids.
- `log.search` itself supports plain search and SQL/analysis queries in `Query`; prefer it for interactive analysis and small result previews.
- `log.describe-histogram-v1` is for time-distribution preview and total hit estimation only for pure search queries before widening or narrowing a search window.
- `log.export-analysis` is for large SQL/analysis row exports that should land in file delivery instead of full stdout.

## Command Examples

- Create a project with inline JSON:
  `volclog tool exec project.create --input '{"ProjectName":"test","Region":"cn-guilin-boe"}'`
- Dry-run a Describe-style GET action with flat input:
  `volclog --dry-run tool exec project.describe-projects --input '{"ProjectName":"demo"}'`
- Rediscover actions with a verb filter:
  `volclog tool list project --verb create`
- Inspect alarm list actions:
  `volclog tool list alarm --verb list`
- Inspect host-group list actions:
  `volclog tool list host-group --verb list`

## First-Response Pattern

1. Identify the surface: `tool / workflow / raw`.
2. Read the contract before building input.
3. Check `profile`, `region`, credential injection strategy, and output strategy.
4. Use `--dry-run` for writes and destructive changes.
5. Keep stdout small unless the user explicitly asks for full raw output.

## Token Control

- Default to `--dry-run` before writes.
- For large responses, prefer `--output-mode file --output-dir <writable-dir>`.
- Add `projection` only after the unfiltered shape is understood.
- `execution.projection` runs on the raw result, not on the CLI envelope; `--jmes-filter` runs on the full CLI envelope.
- If `--jmes-filter` targets an existing envelope field whose value is `null`, stdout returns literal `null` and the command still succeeds.
- Use `tool describe --view full` when compact output hides optional but important nested fields.
- For list, export, and search-heavy tasks, prefer file delivery over full stdout payloads.
- Under file delivery, stdout may contain only a fixed file notice; read the written full envelope from disk and inspect `summary.deliveryMode` there.
- If `tool describe --view full` still shows `query/body/path/header` nesting, treat `input_flat_schema` and `input_encoding_hint` as the execution guide for `tool exec`.
- `HitCount` is only the count returned in the current `SearchLogs` response; it is not the whole-window total.
- `Histogram.TotalCount` is the better whole-window hit count only for pure search when you use `log.describe-histogram-v1` before reading rows.
- `ResultStatus=incomplete` means the service returned only a partial scan; this can happen in `SearchLogs` for both search and analysis queries, so narrow the time range and rerun before trusting counts, returned rows, empty results, or bucket distribution.
- If the user first asks "when did it spike?" or "which bucket is hottest?" for a pure search query, run histogram first, then narrow the time window before `log.search`.
- For search+analysis or pure analysis queries, do not use histogram as the analysis row count. Use SQL like `select count(*)` for analysis totals.
- If the user wants interactive SQL exploration, use `log.search` with SQL in `Query`; if the user wants the full SQL row set, switch to `log.export-analysis`.

## Profile And Credential Selection

- Discover saved profiles with `volclog configure list`.
- Omit `profile` to use the active CLI profile.
- Set `context.profile` when switching tenant or environment inside one session.
- Prefer `secrets_file` or environment-backed profiles over inline credentials.
- When the user says "another account", "another tenant", or "another environment", treat that as a profile-selection problem before touching business input.
- For stateless service assistants and CI, keep long-lived credentials on the host. Let the host choose the local profile, then inject one-shot credentials into the sandbox for that run only.

## Stateless Agent / CI Credential Injection

- Treat credential injection as a host responsibility, not an agent-memory responsibility.
- Preferred pattern: host resolves a local profile, materializes a temporary dotenv file, runs `volclog --secrets-file <path> ...`, then deletes the file.
- Use `context.secrets_file` when the command must stay inside `tool exec` or `workflow exec` JSON context. The value is a plain local path string, not `file://...`.
- Use process-scoped environment variables only as a fallback. They must apply to one subprocess only, not the whole sandbox lifetime.

Required inputs before execution:

- `profile` or another non-secret logical selector such as environment or tenant label. The host uses this selector to resolve the real local profile.
- One credential transport:
  - preferred: `--secrets-file <path>` or `context.secrets_file`
  - fallback: process-scoped `VOLCENGINE_ACCESS_KEY_ID`, `VOLCENGINE_ACCESS_KEY_SECRET`, optional `VOLCENGINE_TOKEN`
- `region` when it is not already implied by the selected local profile or secrets file.
- `endpoint` only when the target uses a private endpoint, a custom endpoint, or a non-default regional endpoint.
- `--dry-run` for write and destructive actions.

Safe examples:

- Host-generated one-shot secrets file for a stateless read:
  `volclog --secrets-file /tmp/volclog.env tool exec project.describe-projects --input '{}'`
- Tool context carrying both selector and temporary secret path:
  `volclog tool exec project.describe-projects --context '{"profile":"prod-readonly","secrets_file":"/tmp/volclog.env"}' --input '{}'`
- Workflow execution with a temporary secret path:
  `volclog workflow exec log.export --context '{"profile":"prod-readonly","secrets_file":"/tmp/volclog.env"}' --input '{"TopicId":"<TopicId>","From":1700000000,"To":1700003600}'`

Do not do this:

- Do not mount the full local `~/.volclog/config.json` into every sandbox by default.
- Do not paste AK/SK/token values into prompts, skills, req.json files committed to the repo, or long-lived session memory.
- Do not inject broad environment variables for the whole sandbox and then rely on `profile` selection. Environment credentials override profile resolution.

## Recovery Recipes

| HTTP / errorCode | Likely Cause | Exact Next Step |
| --- | --- | --- |
| `unknown tool` / `CLIError` | Wrong surface or wrong id | Re-run `tool list <group>` or `workflow list <group>` before guessing aliases |
| `missing --input` / `CLIError` | Non-empty input schema or wrong transport | Re-check `tool describe` or `workflow describe` for `input_schema` and `input_encoding_hint` |
| `filter matched no value` / `CLIError` | Wrong JMESPath or wrong expectation about envelope shape | Inspect one unfiltered response first; for `--jmes-filter` target envelope keys like `data` or `summary`, for `execution.projection` target raw result keys only |
| `401` / `Unauthorized` | Credentials, region, or endpoint do not match | Run `volclog doctor`; inspect AK/SK, region, and endpoint before retrying |
| `403` / `Forbidden or empty server errorCode` | Profile has no permission or wrong tenant/profile selected | Check whether the selected profile matches the target region and tenant, confirm the account has resource permissions, then switch profile before retrying |
| selected profile seems ignored | Process-wide env credentials overrode profile resolution | Remove broad env injection, switch to one-shot `--secrets-file` or `context.secrets_file`, then retry with the intended profile selector |
| `404` / `IndexNotExists` | Index does not exist or is not ready | Inspect `index.describe` / current topic index, then create or wait for readiness before retrying search |
| `409` / `TopicAlreadyExist` | Create request is targeting an existing topic | Re-run topic discovery/list for the target project, then switch to get/modify instead of create |
| `409` / `ProjectAlreadyExist` | Create request is targeting an existing project | Re-run project discovery/list, then switch to get/modify instead of create |
| `ResultStatus=incomplete` | Service returned only a partial scan to keep latency short | Narrow the time range and rerun before trusting counts, returned rows, empty results, or histogram buckets |
| empty search result after write | Wrong profile/topic/time range or index not ready | Verify profile/topic/index, then retry with narrow query and trace enabled |
| huge stdout payload | Output strategy chosen too late | Re-run with `--output-mode file --output-dir <writable-dir>` or a smaller `--jmes-filter` / `execution.projection` before continuing |

Recommended retry posture:

- Retry `unknown tool` only after rediscovery.
- Retry `filter matched no value` only after checking one unfiltered raw shape.
- Retry empty search only after profile/topic/index/time range are reconfirmed.
- Retry `403 Forbidden` only after profile/tenant/permission alignment is rechecked.

## Error Object

- Failed envelopes use one flat `error` object instead of nested upstream payloads.
- Prefer these fields in order:
  - `error.kind`
  - `error.code`
  - `error.message`
  - `error.details`
  - `error.requestId`
  - `error.statusCode`
- For upstream service failures, `error.code` is already the business error code such as `ProjectAlreadyExist`.
- If the service embeds extra JSON details inside the error message, the CLI lifts them into `error.details`; do not parse `error.message` again unless `details` is absent.

## Known Traps

- `page.all` increases completeness and payload size; it is not a compression flag, and it only applies when `tool describe` reports `execution.supports_all=true`.
- `--jmes-filter` runs on the complete CLI envelope. `execution.projection` is different: it runs on the raw result before envelope wrapping.
- `--jmes-filter 'error'` can legitimately return `null` on successful calls because the field exists and its value is null.
- With `--jmes-filter`, file delivery is not the default path; if you need the full large result, rerun without `--jmes-filter` and use `--output-mode file --output-dir <writable-dir>`.
- `workflow` ids such as `log.ingest` and `log.export` are not public API tool ids; they belong to the workflow surface.
- Use `tool` first for public APIs even if a human shortcut name looks more natural.
- `log.ingest` is the natural-language route for write/import/upload tasks; do not guess `log.put` unless the task is clearly about the raw public API contract itself.
- Describe-style GET actions may still show nested `input_schema.query` in full view; for execution, prefer the flat `--input '{"Field":"value"}'` form when `input_flat_schema` says the key maps unambiguously to `query`.
- Environment credentials override profile resolution. If a stateless run must target a specific local profile, prefer host-generated `--secrets-file` or `context.secrets_file` over sandbox-wide env injection.
