# SOPs

| Workflow | When To Use | First Surface | Output Strategy | Stop Condition |
| --- | --- | --- | --- | --- |
| Create a searchable log pipeline | Need project/topic/index and then verify reads | `tool` | stdout for ids, file only if payload gets large | ids confirmed and one read path works |
| Ingest local logs and validate search | Need to write local data and confirm it is queryable | `workflow log.ingest` then `tool` or `workflow` read path | stdout for preview, file if ingest/search response expands | one write path succeeds and one read path returns rows |
| Troubleshoot empty search | Write appeared to succeed but search returns empty | `tool` | keep results small with narrow time range and projection | wrong profile/topic/index found or results appear |
| Preview search volume before reading rows | Need to decide whether to narrow time range, use search preview, or jump to export | `tool log.describe-histogram-v1` | keep stdout small; only switch to file when later row results stay large | bucket distribution and next surface are clear |
| Export large results | Need full rows or analysis rows beyond token budget | `workflow log.export` or `workflow log.export-analysis` | prefer `--output-mode file` or artifact | artifact path and preview both available |

## Create A Searchable Log Pipeline

Use when the task is "create a place to write logs, then make them searchable".

1. `volclog tool describe project.create`
2. `volclog tool exec project.create --dry-run ...`
3. `volclog tool describe topic.create`
4. `volclog tool exec topic.create --dry-run ...`
5. `volclog tool describe index.create`
6. `volclog tool exec index.create --dry-run ...`
7. Wait for index readiness if the next step is immediate search validation.
8. Readiness check:
   - Prefer `volclog --dry-run tool exec index.describe --input '{"TopicId":"<TopicId>"}'` first to confirm the request shape.
   - Then poll `volclog tool exec index.describe --input '{"TopicId":"<TopicId>"}'`.
   - If the response contains `Status` or `State`, wait until it becomes `Ready`.
   - If no explicit readiness field is exposed, wait 10-30 seconds and retry the first `log.search` / `log.export` validation path.
9. Validate with `volclog tool describe log.search` or `volclog workflow describe log.export` depending on result size.

Stop when project/topic/index ids are confirmed and one read path succeeds.

## Ingest Local Logs And Validate Search

Use when the task is "load local sample logs or jsonl into a topic, then confirm they can be queried".

1. `volclog workflow describe log.ingest`
2. `volclog --dry-run workflow exec log.ingest --input file://req.json`
3. Execute `log.ingest` with the selected profile/topic.
4. If the dataset is small, validate with `volclog tool describe log.search` and then `tool exec log.search`.
5. If validation may return many rows, use `volclog workflow describe log.export` instead of full stdout search.

Output strategy: keep ingest stdout small; prefer file output once read validation becomes large.

Stop when one write path succeeds and one read path returns rows from the same topic/time range.

## Troubleshoot "Data Was Written But Search Returns Empty"

1. Confirm the runtime target with `volclog configure list` and the selected profile.
2. Re-run the write path in dry-run mode to confirm topic/profile/region.
3. Inspect topic and index contracts/results with `topic.describe-topic` and `index.describe`.
4. Re-run `log.search` with a narrow time range and minimal projection.
5. If results are still empty, collect trace artifacts before falling back to `raw`.

Stop when either the wrong profile/topic is identified or search starts returning rows.

## Preview Search Volume Before Reading Rows

Use when the task is "for a pure search query, how many hits are there in the whole time window?" or "which time bucket should I inspect first?".

1. `volclog tool describe log.describe-histogram-v1`
2. `volclog tool exec log.describe-histogram-v1 --input '{"TopicId":"<TopicId>","Query":"<query>","StartTime":<ms>,"EndTime":<ms>}'`
3. Read `Histogram.TotalCount` as the better whole-window hit count for pure search. Do not treat `SearchLogs.HitCount` as the whole-window total.
4. If `ResultStatus=incomplete`, treat the result as partial only: shrink the time range and rerun before using bucket counts or absence of hits as evidence.
5. Inspect histogram buckets to find hot time ranges before pulling rows.
6. If the query is search+analysis or pure analysis, do not rely on histogram counts; instead use SQL such as `select count(*)` for analysis totals.
7. If only a small preview is needed, switch to `volclog tool describe/exec log.search` on a narrower window.
8. If the user wants full raw rows and the result is still large, switch to `volclog workflow describe/exec log.export`.
9. If the user wants SQL/analysis rows and the result is still large, switch to `volclog workflow describe/exec log.export-analysis`.

Stop when the next execution surface is clear and the time window has been narrowed enough for preview or export.

## Export Large Result Sets

1. If the user needs full search rows, prefer `log.export`.
2. If the user needs only interactive SQL exploration or a small analysis preview, stay on `tool describe/exec log.search`.
3. If the user needs a full SQL/analysis row set, prefer `log.export-analysis`.
4. Keep `--output-mode file` unless the user explicitly asks for full stdout.
5. Add projection only after the base request returns the expected shape.

Fallback: if `workflow` is not suitable for the exact need, drop to `tool describe log.search` before considering `raw`.

Stop when the file artifact path is produced and the preview confirms the expected fields.
