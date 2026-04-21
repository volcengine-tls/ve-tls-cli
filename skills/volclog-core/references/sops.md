# SOPs

Use this file only for cross-group execution order, stop conditions, and hand-off points between surfaces.

## Escalate Mutating Or Import Tasks

1. switch to `volclog-human`
2. rediscover the write/import contract there
3. return to readonly validation in `volclog` only when the task becomes read-only again

Stop when:
- the write/import contract is clear in `volclog-human`, or
- the task is reclassified as readonly and can stay in `volclog`

Notes:
- Typical `volclog-human` write paths are `workflow log.ingest`, `project.create`, `topic.create`, and `index.create`.
- After any write path, prefer moving back to readonly validation in `volclog`.

## Validate Existing Searchable Log Pipeline

1. `configure.list`
2. `topic.describe-topic`
3. `index.describe`
4. `log.search`

Stop when:
- the wrong profile, topic, or time range is identified, or
- one narrow validation query returns the expected rows

Notes:
- Prefer `index.describe` and read `Status` or equivalent readiness fields when available.
- If readiness is not explicit, wait `10-30 seconds`, then retry one narrow validation query.
- Keep validation queries small and recent.

## Troubleshoot Empty Search

1. `configure.list`
2. `topic.describe-topic`
3. `index.describe`
4. `log.search`

Stop when:
- the wrong profile, topic, or time range is identified, or
- search starts returning rows

Notes:
- Reconfirm the target profile before changing business input.
- If index readiness is unclear, check `Status` first, then retry with a narrow window.

## Validate Data After A Write Path Already Finished Elsewhere

1. `log.search`
2. `workflow log.export`

Stop when:
- one narrow validation query returns rows, or
- the task clearly needs a wider readonly export instead of another preview

Notes:
- If the task still needs import or another write, switch back to `volclog-human` instead of probing default `volclog`.
- Keep the first validation query narrow before escalating to export.

## Preview Search Volume Before Reading Rows

1. `tool describe log.describe-histogram-v1`
2. `tool exec log.describe-histogram-v1`
3. `tool describe log.search`
4. `tool exec log.search`

Stop when:
- the next execution surface is clear, and
- you know whether to stay interactive or switch to export

Notes:
- Use this only for pure search queries.
- `Histogram.TotalCount` is the whole-window estimate for pure search.
- `HitCount` is only the count in the current `SearchLogs` response.

## Interactive SQL Exploration

1. `tool describe log.search`
2. `tool exec log.search`
3. If the row set grows beyond comfortable stdout, switch to `workflow log.export-analysis`

Stop when:
- the user has the answer from preview rows, or
- the task clearly needs a full analysis row set

Notes:
- `log.search` supports interactive SQL exploration directly in `Query`.
- Switch only when the preview no longer answers the user question.

## Export Large Result Sets

1. `workflow log.export`
2. `workflow log.export-analysis`

Stop when:
- stdout or the written output confirms the expected envelope shape, and
- the file contains the full output needed by the user

Notes:
- Read the stdout notice or written envelope and stop once the expected output shape is confirmed.
