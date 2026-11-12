# Changelog

## volclog-v1.2.0

- Add four SavedSearch tools, align existing API request contracts, and cap `SearchLogs.Limit` at 1000 rows per page.
- Honor explicit profile selection, validate runtime context, and improve Console Login diagnostics and Agent login guidance.
- Require explicit permission for business writes and support non-interactive execution, an invocation deadline, and redacted tracing and dry-run previews.
- Report execution and delivery outcomes separately. Include completeness and continuation for paginated results and retain partial results when later requests fail.
- Add `tool describe --view input` and `tool schema`, support JMESPath filters with file delivery, and surface `--page-all` in the main help.
- Improve Skill installation conflict and directory error messages, and document installation of a pinned CLI version.

### Upgrade notes

- Pass `--allow-write` or set `context.execution.allow_write=true` for business writes. Check `summary.remoteOutcome` before retrying a write after an output error.
- Check `summary.completeness` before reporting a collection total. Use `--page-all` where supported or resume with the returned `next` input.
- In the default edition, read invocation failures and file-delivery notices from stdout. Use `artifacts[].path` to locate a delivered file.
- Refresh cached tool contracts and update bundled Skills before preparing requests against the updated schemas.

## volclog-v1.2.0-rc.2

- Fix explicit profile selection in `configure show` and align authentication guidance with runtime precedence.
- Improve Console Login error stages and document foreground fallback for Agent sessions and installation by version.
- Cap the SearchLogs request `Limit` at 1000 rows per page.
- Add `tool describe --view input` and `tool schema` for complete request fields and cacheable shared schemas, with updated bundled Skill guidance.
- Include Topic storage entitlement selection for release-candidate testing.

## volclog-v1.2.0-rc.1

- Add four SavedSearch tools for creating, deleting, reading, and listing shared Topic saved queries, with exact field spelling, explicit write permission, upstream error propagation, and server-source provenance.
- Correct 39 required-field discrepancies across 30 existing operations and persist the corrections in generator inputs. The catalog now contains 169 operations, including 163 public tools.
- Keep explicitly selected profiles authoritative over environment credentials and targets; validate runtime context fields and conflicts, and redact credential-bearing dry-run previews.
- Separate remote execution outcomes from output delivery failures, classify uncertain request results, and preserve confirmed progress across multi-batch writes.
- Report collection completeness and continuation input for default single-page lists, pagination, searches, and exports; retain acquired partial results after later request failures.
- Standardize default-edition invocation errors and file-delivery notices as JSON, preserve remote outcome evidence when filtering or writing output fails, and support JMESPath selection with file delivery.
- Enforce declared input types, unknown-field rejection, conditional request requirements, and explicit recovery provenance in tool contracts.
- Add automatic non-interactive behavior without a TTY, an optional whole-invocation deadline, explicit business-write permission, and validated trace controls.
- Classify an existing Skill installation as a conflict and provide status/update guidance; align bundled profile-selection instructions with runtime behavior.

### Upgrade notes

- Business writes now require `--allow-write` or `context.execution.allow_write=true`. Inspect the contract and use `--dry-run` before granting write permission.
- Read `summary.remoteOutcome` and `summary.deliveryOutcome` separately. An output failure does not establish that a write failed remotely; reconcile an unknown result before retrying.
- Inspect `summary.completeness` before reporting a total. `summary.itemCount` counts returned items. Follow a supplied `next.operationId` and `next.input` as a single-page call with the same runtime selectors and page-all disabled, or use `--page-all` for full collection where supported.
- In the default edition, parse structured invocation failures and file-delivery results from stdout. Obtain delivered file paths from `artifacts[].path`; human-edition file notices retain their text format.
- Supply `--output-dir` for large-result delivery and forced file output on the default tool/workflow/raw path. Invalid context fields and disabled trace-redaction values now fail validation; `context.trace=false` disables tracing even with a global trace directory.
- Refresh cached tool contracts and update bundled Skills before preparing requests against the corrected schemas. SavedSearch availability still depends on the deployed service and IAM permissions.

## volclog-v1.1.1

- Preserve safe Console Login diagnostics through CLI error handling, including the failing stage, allowlisted OAuth error codes, HTTP status, and validated request IDs, without exposing tokens or raw response bodies.
- Distinguish browser callback rejection, missing authorization codes, state mismatches, callback timeouts, cancellation, and network/DNS/TLS failures instead of reporting only `console login failed`.
- Provide targeted guidance for sign-in HTTP 429 and 403 responses, and add regression coverage for nested errors, secret redaction, and the existing per-call token refresh retry budget.
- Align ordinary authentication HTTP retry backoff with `ve` CLI by adding less than 100ms of random jitter to the existing exponential delay. Preserve valid `Retry-After` precedence and the delay cap; retry counts, token refresh timing, default login flow, and device-code polling intervals are unchanged.

## volclog-v1.1.0

- Support both local browser callback + PKCE Console Login (default) and explicit Device Code authorization for remote or sandboxed environments, while retaining locked caches, atomic configuration writes, refresh behavior, and secret redaction.
- Preserve `json.Number` across dynamic request, response, envelope, JSONL, and JMESPath processing, and reject invalid filters or projections before any TLS request is sent.
- Add machine-readable build and catalog metadata through `volclog version`.
- Add explicit-only CLI upgrades for npm and standalone installations; no background update checks are performed.
- Add managed skill status, safe update, and uninstall commands with version and digest based modification detection.
- Expose `ManualMergeShard` as the high-risk `shard.merge` operation with contract-first dry-run support and `shard.describe` reconciliation guidance.
- Fix the `log.consume` request contract: `Cursor` is required, while the Kafka-only `Offset` field is no longer exposed or required.
- Correct existing log, processor, index, project, topic, tag, and download-task request contracts, including nested object types, server-owned defaults, and conditional requirements. No new AI assistant or Copilot APIs are exposed.

### Upgrade notes

- Replace `log.create` with `log.create-download-task`, and `log.cancel` with `log.cancel-download-task` in scripts and Agent tool references. The old short names are removed, not retained as aliases; calls using them fail with a replacement-name hint. The underlying API actions, HTTP methods, and paths are unchanged.
- Pass an explicit `MaxResults` between 10 and 100 to `tag.list`; do not rely on the previously documented default of 20.
- Refresh cached tool contracts with `volclog tool describe <tool-id> --view full` before authoring requests. Schema defaults describe server behavior and are not injected by the CLI; dry-run does not replace service-side type, range, enum, or conditional validation.

## volclog-v1.0.6

- Bundle the `tls-logcollector` Agent skill for contract-first LogCollector resource setup, parser and processor validation, Linux or Kubernetes deployment guidance, and end-to-end heartbeat, binding, and ingestion verification.
- Expose TLS account activation and collector configuration validation helpers, including regex generation, sample parsing, delimiter preview, latest-log inspection, and corrected processor debug contracts.
- Align log-back-flow contracts to the current server model, including ETL and AgentLoop evaluation-set delivery fields and removal of retired scheduled-SQL inputs.
- Add 24 atomic App, LogApp, Trace span-search, and TraceScore operations aligned to the pinned log-service server implementation.
- Add `app.resolve-resources` and `app.resolve-topic-ids` workflows for normalized App resource traversal and deduplicated LogApp Topic resolution.
- Preserve repeated query parameters such as `DescribeTraceScores.SpanIds` through execution, runtime transport, and redacted tracing.
- Document the supplemental-operation source revision and symbol mapping, and update the bundled `volclog-core` routing and SOP guidance.
- Build local and release macOS binaries with the external Go linker so generated Mach-O executables contain the `LC_UUID` required by dyld, while keeping the deployment target at macOS 11.0.

## volclog-v1.0.5

- Add standalone Console Login, SSO, RAM Role ARN, OIDC, and ECS Role authentication while preserving legacy AK/SK and manual STS behavior.
- Unify explicit TLS region, endpoint, and timeout configuration across authentication modes, with safer runtime validation and authentication help discovery.
- Unify agent and human execution around one generated Operation catalog and reusable Executor while preserving the legacy tool contract surface.
- Remove unreachable and duplicated CLI/catalog code, harden runtime boundaries, and retain default and human quality gates.
- Publish verified Unix and Windows binary installers and support stable npm installation for both `volclog` and `volclog-human`.

## volclog-v1.0.5-rc.3

- Unify public tool and ordinary human CRUD execution around one generated Operation catalog and reusable Executor while preserving all 125 legacy tool contract digests.
- Derive capabilities and request templates from the canonical catalog, removing duplicated generated metadata and stale static templates.
- Separate runtime selector, authentication provider, TLS client, transport, and tracing responsibilities from CLI parsing and presentation.
- Remove unreachable command islands, enforce default and human quality gates, and harden typed-nil runtime boundaries without changing legacy AK/SK behavior.
- Improve authentication help discovery and guide users from profile setup through `doctor` to the first `tool exec` request.

## volclog-v1.0.5-rc.2

- Let ECS Role refresh use the caller's timeout budget so the documented retry policy can complete.
- Restore the default SSO scopes when older configuration contains an empty scope list.
- Normalize secure-store and documentation roots on macOS before enforcing path boundaries.
- Publish the Unix and Windows binary installers with each GitHub Release, reject downloaded checksum mismatches on Windows, and document checkout-free installation.

## volclog-v1.0.5-rc.1

- Add standalone SSO and Console Login (`mode=sso` / `mode=console-login`) with no runtime dependency on `ve`, `~/.volcengine`, or `volcengine-cli`.
- Add standalone workload providers for RAM Role ARN, OIDC, and ECS Role.
- Store SSO and Console Login token/STS caches in `0600` files under `<state-root>/sso/cache/` and `<state-root>/login/cache/`; cache roots can be overridden with `VOLCLOG_SSO_CACHE_DIRECTORY` / `VOLCLOG_LOGIN_CACHE_DIRECTORY`.
- Dynamic mode never falls back to static AK/SK on failure; `ReauthRequired` errors recover with `volclog login --profile NAME` or `volclog sso login --profile NAME`.
- Legacy AK/SK, environment variables, `--secrets-file`, `cred-ref`, and manual STS behaviors are unchanged.
- Add `volclog login [--profile NAME] [--remote]`, `volclog logout [--profile NAME|--all]`, `volclog configure sso-session`, `volclog configure sso`, `volclog sso login|logout`.
- Unify explicit TLS region, endpoint, and timeout configuration across authentication modes without deriving endpoints from regions.
- Reject runtime and context fields placed in `tool exec --input`, including sectioned input, instead of silently ignoring them.

## volclog-v1.0.0

- First stable release.
- CLI structure stabilised around shortcut / api / raw-api flows.
- Add Agent-oriented capabilities discovery, `--describe`, templates and `--dry-run`.
- Add `doctor`, `--trace-dir`, `--output-mode file`, `--output-file`, and `--secrets-file`.
- Add bundled skill installer and npm bootstrap entry.

## volclog-v0.0.2

- Add `doctor` command for offline/online diagnostics.
- Add `--trace-dir` to generate redacted trace artifacts (JSONL).
- Add `--output-mode file` and `--output-file` to write output to disk.
- Add `--secrets-file` (dotenv) and `./.volclog/cli.config.json` project defaults.
- Add stdin input support via `-` for `file://`-style parameters.

## volclog-v0.0.1

- Initial public release.
- Resource management: Project/Topic/MetricTopic/Index.
- Query: SearchLogs and MetricTopic Prometheus-compatible endpoints.
- Structured output: JSON / JSONL.
