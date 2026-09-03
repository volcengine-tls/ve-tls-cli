# 6. Advanced

[← Previous: Practical Guide](5-Practical-Guide.md) | [中文](6-Advanced_zh.md) | [Next: Human Shortcuts →](7-Human-Shortcuts.md)

This guide covers advanced operational decisions, edge cases, and patterns. For foundational contracts, see [Usage](4-Usage.md), [Configuration](3-Configuration.md), and [Authentication](2-Authentication.md).

## 1. Filtering and projection

`--jmes-filter` is compiled and validated before any request is dispatched. It is then applied after the response is received to the final envelope (for `raw`, `tool exec`, and `workflow exec`) or to the raw result (for other groups).

`execution.projection` (in the `--context` JSON for `tool exec` and `workflow exec`) is compiled before the request and applied to the raw result after the response is received and before the CLI builds the envelope. It is not server-side. It uses the same JMESPath engine as `--jmes-filter` but operates on the raw result rather than the final envelope.

Dynamic JSON keeps `json.Number` through request decoding, service responses, envelopes, JSONL output, and JMESPath evaluation. Large integers and long decimals are not converted through `float64`, so their precision is preserved. Invalid filters and projections fail during local preflight and do not send a request.

Behavior:

- If the filter resolves to an existing `null` value, the command prints literal `null` and succeeds.
- If the filter path does not exist (missing key, out-of-range index), the command fails with `filter matched no value` and exit code `3`.
- If the filter expression is invalid, the command fails with `invalid jmes-filter expression` and exit code `3`.

`--jmes-filter` cannot be combined with `--output-mode file` for `raw`, `tool exec`, and `workflow exec`. For other groups, the combination is allowed.

## 2. Large-result delivery

Forced file mode (`--output-mode file`) behavior depends on the command surface:

- `raw`, `tool exec`, `workflow exec`: the complete envelope is written to an artifact under `--output-dir`; stdout contains only the fixed Chinese file notice.
- Non-envelope groups: the raw result is written to the selected file and stdout emits only the bare file path.
- Generic human-shortcut envelope commands (for example `project list`, `topic create`): the envelope artifact is written to the file and the complete envelope is also emitted to stdout.
- Human `volclog-human log export` / `log export-analysis`: exported data rows are streamed into the file artifact (JSON array for `--output json`, JSONL rows for `--output jsonl`) and stdout emits the complete envelope containing artifact metadata. The artifact itself is not an envelope.

The fixed Chinese notice is limited to `raw`, `tool exec`, and `workflow exec`; human log exports do not use it. Automation must follow the output contract of the command surface it invokes.

Automatic spill (`file_auto`) applies to `tool exec` and `workflow exec` when all of these hold: output format is JSON (`--output json`, default or explicit), output mode is `stdout` and not explicitly set, there is no `--jmes-filter`, and no `execution.artifact`/`execution.projection`. When the estimated envelope size exceeds 16 KiB, the complete envelope is written to a file under `--output-dir`; stdout emits only the fixed file notice. If `--output-dir` is missing or unwritable, the command errors with `result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery`.

Default `workflow exec log.export` and `log.export-analysis` stream exported rows directly into the data artifact (JSON array for `--output json`, JSONL rows for `--output jsonl`) rather than writing an envelope. The data artifact is not an envelope and does not contain `status`, `summary.deliveryMode`, `requestId`, or `ResultStatus`. stdout still contains only the fixed file notice with the data-artifact path. `log.export` auto-paginates pure-search results; it defaults to at most 100 pages when `MaxPages` is omitted and can stop at the selected/default page limit without exposing a "more pages remained" signal. The exported data artifact contains rows only and does not preserve `ResultStatus` or the final `ListOver` state. Command success and file existence therefore prove only that those rows were exported, not that the evidence set is complete. For evidence-grade export, inspect a bounded preview first, do not proceed as complete when preview `ResultStatus=incomplete`, narrow or split the time window or query, and explicitly choose `MaxPages` based on expected volume. `log.export-analysis` performs analysis export and does not auto-paginate analysis rows (SQL `limit`/`offset` semantics apply).

## 3. Pagination

`tool exec --page-all` requests all pages of a paginated result, but only where the tool contract supports it. Inspect `tool describe <group.action>` and check `execution.supports_all` (or `supports_all` in the describe output). If `supports_all` is `false`, `--page-all` is not supported for that action and you must paginate manually.

`workflow exec` does not consume `execution.page.all` / `execution.page_all` context controls. Pagination semantics are workflow-specific; follow the guidance in `workflow describe` for the workflow you are using.

## 4. Incomplete results

Some search and analysis responses include a `ResultStatus` field. When `ResultStatus` is `incomplete`, the service returned only a partial scan. Do not present partial results as complete. Narrow the time range or query and rerun before trusting counts, rows, or absence of hits. For analysis queries, `ResultStatus=incomplete` can also affect bucket counts and totals.

## 5. Trace and diagnostic artifacts

When the CLI creates new trace paths, it requests `0700` for the directory and `0600` for the JSONL trace file. Existing directories and files may retain their current permissions.

Normal structured request/response trace fields contain only header keys (never values), query keys (never values), and SHA-256 hashes of request and response bodies (never raw bytes). `--trace-redact off` is accepted and normalized but does not disable this forced structured-field redaction and does not emit raw header, query, or body values.

The `error_message` field stores the transport error string directly, which can include the full request URL and query values. Never put credentials or other secrets in query parameters. Treat trace files as sensitive diagnostic artifacts: keep their permissions restricted and share them only after inspection.

## 6. Error recovery

`raw`, all `tool` commands (including `list`/`describe`), all `workflow` commands (including `list`/`describe`), and human-shortcut envelope groups use structured error envelopes (`status: failed`, `error` object). Other non-envelope groups write flat JSON to stderr in this field order: `errorCode`, `errorMessage`, `requestId`, `statusCode`, `kind`, `hint`.

The `requestId` in the envelope (or `x-tls-requestid` response header) identifies the request for support and troubleshooting. Capture it when reporting issues.

Errors fall into broad categories: local validation (contract shape, missing required fields), authentication (credential resolution, login), transport (network, endpoint, DNS), and TLS service response (status code, service error message). Use `--dry-run` to isolate local validation errors before a network call, and `--trace-dir` to capture the request/response exchange for transport or service errors.

## 7. Thin-client boundaries

The CLI validates local contract shape (input schema, required fields) and transports requests to the TLS API. The server remains authoritative for permissions, resource state, query semantics, and service limits. A successful dry-run or local validation does not guarantee the request will be accepted by the service.

## 8. Stable search, histogram, and analysis rules

`log.search` supports both plain search syntax and SQL/analysis syntax (`* | select ...`). `HitCount` is only the count returned in the current response window, not the whole-window total. For pure search queries, `log.describe-histogram-v1` can provide a time-distribution preview and a better whole-window hit estimate via the top-level `TotalCount` field (in the CLI envelope, `data.TotalCount`). For analysis queries, body fields such as `Context`, `Sort`, `Limit`, and `Offset` do not page analysis rows; use SQL `limit`/`offset` inside the query instead.

If `ResultStatus=incomplete`, narrow the time range and rerun before trusting counts or rows.

## 9. Agent Skills and automation

Use `skill list` and `skill install --name <name> --dir <dir>` only after verifying their exact syntax with `--help`. Follow the discover → describe → dry-run → execute order: inspect the contract, validate locally, then send.

The 1.0.7 build bundles two skills:

- `volclog-core`: the generic contract-first runtime, routing, delivery, and recovery model.
- `tls-logcollector`: LogCollector collection design, non-persistent sample validation, TLS resource reconciliation, Linux/Kubernetes deployment guidance, and end-to-end verification.

Install only the skill needed by the target agent, or omit `--name` to install all bundled skills:

```bash
volclog skill list
volclog skill install --name volclog-core --dir <agent-skills-dir>
volclog skill install --name tls-logcollector --dir <agent-skills-dir>
```

### 9.1 Skill lifecycle and modification protection

All lifecycle commands require `--dir`; repeat `--name` for multiple Skills or omit it for all bundled Skills:

```bash
volclog skill status --dir <agent-skills-dir> [--name <name>]
volclog skill update --dir <agent-skills-dir> [--name <name>] [--force]
volclog skill uninstall --dir <agent-skills-dir> [--name <name>] [--force]
```

Each installed Skill has a `.volclog-skill.json` sidecar with `schema_version`, `name`, `installed_version`, `source_digest`, and `installed_digest`. The digest is a stable SHA-256 over sorted relative POSIX paths and length-framed file contents, excluding the sidecar; symlinks and special files are rejected. `status` runs fully offline and reports `not_installed`, `current`, `outdated`, `modified`, `untracked`, or `invalid_manifest`; version mismatch is reported separately from content modification.

`current` means the disk content equals `installed_digest` and the bundled source equals `source_digest`; `outdated` means the content is unchanged but the bundled source digest changed; `modified` means the disk content differs from `installed_digest`. A missing sidecar is `untracked`, and an unreadable or inconsistent sidecar is `invalid_manifest`.

`install` fails on an existing target unless `--force` is explicit. `update` and `uninstall` leave `modified`, `untracked`, and `invalid_manifest` Skills untouched by default; `--force` explicitly opts into replacement or removal. Install and update build a complete temporary directory, replace it with same-filesystem renames, and restore the prior directory if replacement fails.

### 9.2 Version and explicit upgrade

`volclog version` emits machine-readable JSON with `schema_version`, `version`, `edition`, `commit`, `catalog_digest`, `operation_count`, `public_operation_count`, and `workflow_count`. `volclog --version` remains the text-compatible form for existing scripts.

Upgrade is explicit only and never runs a background check:

```bash
volclog upgrade --check
volclog upgrade --version 1.0.7
volclog upgrade --version 1.0.7 --yes
volclog upgrade --yes
```

Checks and version selection without `--yes` are no-write operations. npm installations delegate to npm; standalone binaries verify the release checksum and use an atomic replacement.

`tls-logcollector` complements rather than replaces `volclog-core`. It uses the current `tool describe` contract and the non-persistent collector, index-preview, and processor-debug helpers before any collector-rule write. Its temporary self-log POC path is opt-in because broad or recursive self-collection can amplify ingestion.

For automation: pass `--profile` (or `--secrets-file`) explicitly, use deterministic file output (`--output-mode file --output-dir <dir>`) for large results, and never persist plaintext credentials in scripts.

Identity determinism: in static AK mode, a complete `VOLCENGINE_ACCESS_KEY_ID` + `VOLCENGINE_ACCESS_KEY_SECRET` environment pair bypasses the explicitly selected profile. Automation that intends to use a static profile must remove unintended static credential environment variables, or use a controlled complete `--secrets-file` as the intended identity source. Dynamic provider modes ignore environment AK/SK. See [Configuration](3-Configuration.md) section 5 for the exact precedence.

Request-ID sources: for ordinary envelope-producing commands, capture `requestId` from the envelope. For default forced-file `workflow exec log.export` / `log.export-analysis`, stdout is only the fixed notice and the data artifact has no `requestId`. If transport Request IDs for streamed or multi-page export are required, enable a restricted `--trace-dir`, inspect the trace file, and follow the trace sensitivity guidance in section 5.

## 10. General troubleshooting

- Use `volclog --profile <profile> doctor` for local config/credential checks and `doctor --online` for a minimal live connectivity check.
- If a command fails with `conflicting runtime selectors`, check the identity selector rules in [Configuration](3-Configuration.md) section 5.3: an explicit selector is optional; any profile selector combined with any secrets-file selector conflicts; two secrets-file selectors conflict; global/context profiles conflict only if the names differ; repeating the same profile name is accepted but redundant.
- If `--output table` is rejected, you are on the default `volclog` agent path; use `--output json` or `--output jsonl`, or switch to `volclog-human` for the specific shortcut surfaces that support table.
- If `--output-mode file` produces no envelope on stdout for `tool exec`/`workflow exec`, that is expected: stdout contains only the fixed file notice; read the artifact file.

---

[← Previous: Practical Guide](5-Practical-Guide.md) | [中文](6-Advanced_zh.md) | [Next: Human Shortcuts →](7-Human-Shortcuts.md)
