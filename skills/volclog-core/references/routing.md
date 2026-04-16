# Routing

Use this table only when the user intent is still ambiguous after checking `tool list` or `workflow list`.

| Intent | Keywords | Prefer | First Command | Example |
| --- | --- | --- | --- | --- |
| Discover public resources in one domain | list, show, inspect, enumerate | `tool list <group>` | `volclog tool list <group>` | `volclog tool list topic` |
| Create, inspect, modify, or delete a public resource | create, get, modify, delete, update | `tool describe/exec <group.action>` | `volclog tool describe <group.action>` | `volclog tool describe topic.create` |
| List resources from a `Describe...s` style API | list, all, describe plural | `tool list <group> --verb list` | `volclog tool list <group> --verb list` | `volclog tool list project --verb list` |
| Inspect host groups or machine membership | host-group, machine group, agent list, inspect hosts | `tool list host-group --verb list` | `volclog tool list host-group --verb list` | `volclog tool describe host-group.describe-host-groups-v2` |
| Inspect or troubleshoot alarm rules | alarm, alert, notification policy, incident rule | `tool list alarm --verb list` | `volclog tool list alarm --verb list` | `volclog tool describe alarm.describe-alarms` |
| Ingest local lines, jsonl, or json-array into a topic | ingest, upload, ship, write logs, put logs | `workflow describe/exec log.ingest` | `volclog workflow describe log.ingest` | `volclog workflow describe log.ingest` |
| Export many raw search rows | export, download logs, dump search results | `workflow describe/exec log.export` | `volclog workflow describe log.export` | `volclog workflow describe log.export` |
| Export SQL/analysis rows | analyze, sql export, export analysis | `workflow describe/exec log.export-analysis` | `volclog workflow describe log.export-analysis` | `volclog workflow describe log.export-analysis` |
| Exact method/path is already known | raw, method, path, transport | `raw` | `volclog raw --method <METHOD> --path <PATH>` | `volclog raw --method POST --path /SearchLogs` |

## Decision Tree

- If the target is a published public API and you do not yet know the exact action id, start with `tool list`.
- If the user intent says ingest, export, or another CLI-owned orchestration, prefer `workflow`.
- If the user already supplied exact method and path, use `raw`.
- If two surfaces seem possible, prefer `tool`, then `workflow`, and keep `raw` as the last resort.

## Rediscovery Pattern

- If `unknown tool` happens, re-run `volclog tool list <group>` before guessing aliases.
- Default rediscovery form: re-run `volclog tool list <group> --verb <verb>` when the user intent already implies a verb.
- If that list is still noisy, re-run `volclog tool list <group> --verb <user-intent-verb>`.
- Example: user says "create project" → re-run `volclog tool list project --verb create`.
- Example: user says "list alarms" → re-run `volclog tool list alarm --verb list`.
- If `tool list` still does not fit and the user intent is ingest/export, switch to `workflow list <group>`.

## Surface Rules

- `tool` remains the default surface for published public APIs.
- `workflow` is for CLI-owned orchestration such as ingest, paginated export, or analysis export.
- `raw` is the escape hatch, not the discovery surface.
- Human shortcut groups are for humans. Do not route an agent there unless the user explicitly asks for shortcut behavior.
