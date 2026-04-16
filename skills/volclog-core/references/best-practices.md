# Best Practices

## Surface Choice: tool / workflow / raw

- Use `tool` for public API discovery and execution.
- Use `workflow` for CLI-owned orchestration such as `log.ingest`, `log.export`, and `log.export-analysis`.
- Use `raw` only when exact method/path is already known or the public contract surface cannot express the need.
- If you are unsure between `tool` and `workflow`, inspect both with `describe` before constructing input. Do not guess ids.

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
3. Check `profile`, `region`, and output strategy.
4. Use `--dry-run` for writes and destructive changes.
5. Keep stdout small unless the user explicitly asks for full raw output.

## Token Control

- Default to `--dry-run` before writes.
- For large responses, prefer `--output-mode file` or execution artifact output.
- Add `projection` only after the unfiltered shape is understood.
- `projection` runs on the raw result, not on the CLI envelope.
- Use `tool describe --view full` when compact output hides optional but important nested fields.
- For list, export, and search-heavy tasks, prefer preview + file/artifact over full stdout payloads.
- If `tool describe --view full` still shows `query/body/path/header` nesting, treat `input_flat_schema` and `input_encoding_hint` as the execution guide for `tool exec`.

## Profile And Credential Selection

- Discover saved profiles with `volclog configure list`.
- Omit `profile` to use the active CLI profile.
- Set `context.profile` when switching tenant or environment inside one session.
- Prefer `secrets_file` or environment-backed profiles over inline credentials.
- When the user says "another account", "another tenant", or "another environment", treat that as a profile-selection problem before touching business input.

## Recovery Recipes

| HTTP / errorCode | Likely Cause | Exact Next Step |
| --- | --- | --- |
| `unknown tool` / `CLIError` | Wrong surface or wrong id | Re-run `tool list <group>` or `workflow list <group>` before guessing aliases |
| `missing --input` / `CLIError` | Non-empty input schema or wrong transport | Re-check `tool describe` or `workflow describe` for `input_schema` and `input_encoding_hint` |
| `filter matched no value` / `CLIError` | Wrong JMESPath or wrong expectation about envelope shape | Inspect one unfiltered response, then apply projection to raw keys only |
| `401` / `Unauthorized` | Credentials, region, or endpoint do not match | Run `volclog doctor`; inspect AK/SK, region, and endpoint before retrying |
| `403` / `Forbidden or empty server errorCode` | Profile has no permission or wrong tenant/profile selected | Check whether the selected profile matches the target region and tenant, confirm the account has resource permissions, then switch profile before retrying |
| `404` / `IndexNotExists` | Index does not exist or is not ready | Inspect `index.describe` / current topic index, then create or wait for readiness before retrying search |
| `409` / `TopicAlreadyExist` | Create request is targeting an existing topic | Re-run topic discovery/list for the target project, then switch to get/modify instead of create |
| `409` / `ProjectAlreadyExist` | Create request is targeting an existing project | Re-run project discovery/list, then switch to get/modify instead of create |
| empty search result after write | Wrong profile/topic/time range or index not ready | Verify profile/topic/index, then retry with narrow query and trace enabled |
| huge stdout payload | Output strategy chosen too late | Re-run with `--output-mode file`, artifact output, or a smaller projection before continuing |

Recommended retry posture:

- Retry `unknown tool` only after rediscovery.
- Retry `filter matched no value` only after checking one unfiltered raw shape.
- Retry empty search only after profile/topic/index/time range are reconfirmed.
- Retry `403 Forbidden` only after profile/tenant/permission alignment is rechecked.

## Known Traps

- `page.all` increases completeness and payload size; it is not a compression flag, and it only applies when `tool describe` reports `execution.supports_all=true`.
- `raw` and `tool exec` use the same JMESPath semantics, but `tool exec` also has `context.execution.projection`.
- `workflow` ids such as `log.ingest` and `log.export` are not public API tool ids; they belong to the workflow surface.
- Use `tool` first for public APIs even if a human shortcut name looks more natural.
- `log.ingest` is the natural-language route for write/import/upload tasks; do not guess `log.put` unless the task is clearly about the raw public API contract itself.
- Describe-style GET actions may still show nested `input_schema.query` in full view; for execution, prefer the flat `--input '{"Field":"value"}'` form when `input_flat_schema` says the key maps unambiguously to `query`.
