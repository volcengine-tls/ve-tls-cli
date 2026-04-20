# Routing

Use this file only to choose the execution surface. Do not use it for runtime/output decisions.

| Intent | Prefer | First Command | Escalate When |
| --- | --- | --- | --- |
| Import local lines/jsonl/json-array from file or stdin | `workflow log.ingest` | `volclog workflow describe log.ingest` | You actually need the raw public `PutLogs` contract |
| Preview rows or do interactive analysis | `tool log.search` | `volclog tool describe log.search` | You need the full analysis row set or explicit export |
| Preview pure-search time buckets | `tool log.describe-histogram-v1` | `volclog tool describe log.describe-histogram-v1` | You already know the final row query and do not need bucket preview |
| Export many raw search rows | `workflow log.export` | `volclog workflow describe log.export` | You only need a small preview, not export |
| Export full SQL/analysis rows | `workflow log.export-analysis` | `volclog workflow describe log.export-analysis` | You only need interactive analysis or a small preview |
| Exact method/path is already known | `raw` | `volclog raw --method <METHOD> --path <PATH>` | A public `tool` or `workflow` contract exists |

## Decision Rules

- Start with `tool` for published public APIs.
- Use `workflow` only when the intent is clearly local import/export or another CLI-owned orchestration.
- For `log.ingest` vs `tool log.put`:
  - local file/stdin import where CLI should normalize lines/jsonl/json-array -> `log.ingest`
  - explicit public `PutLogs` contract work or direct API control -> `tool log.put`
- Human shortcut groups are for humans. Do not route an agent there unless the user explicitly asks for shortcut behavior.

## Rediscovery

- If `unknown tool` happens, rerun `volclog tool list <group>` before guessing aliases.
- If the intent already implies a verb, re-run `volclog tool list <group> --verb <user-intent-verb>`.
- Example: create project -> `volclog tool list project --verb create`
- Example: list alarms -> `volclog tool list alarm --verb list`
- If `tool list` still does not fit and the intent is ingest/export, switch to `volclog workflow list <group>`.
