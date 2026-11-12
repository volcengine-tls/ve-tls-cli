---
name: volclog-core
description: Use when operating volclog in agent or CI sessions and you need a contract-first workflow for runtime selector resolution, discover/describe/exec routing, App resource or Topic resolution, envelope reading, large-result delivery, and error recovery without guessing commands, fields, or shortcut flows.
---

# Volclog Core

## Overview

Use this skill only for agent-only incremental knowledge.

Treat this file as the dominant generic operating model for `volclog`, not as an environment-specific bootstrap note and not as an API reference. This file is the behavior summary; references are the authoritative detail. If `volclog tool describe ...` or `volclog workflow describe ...` already answers the question, stop and follow that contract instead of adding duplicate guidance here.

## Operating Stance

- Treat `tool describe` or `workflow describe` as the contract truth source. Read `tool describe` or `workflow describe` first.
- Treat profile configuration as a runtime-selector problem, not a region-discovery ritual.
- Think in runtime selectors: active profile, explicit `--profile <name>`, one-shot `--secrets-file`, `context.secrets_file`, or process-scoped environment credentials.
- Use `tool` for published public APIs, `workflow` for CLI-owned orchestration, and `raw` only when method/path is already known.
- Keep one-call App, LogApp, Trace, and TraceScore capabilities on `tool`; use `workflow app.resolve-resources` or `app.resolve-topic-ids` only for CLI-owned cross-call expansion.
- Prefer structured JSON input such as `--input '{...}'` when `tool exec` or `workflow exec` already supports it.

## Runtime Selector Model

- Treat the execution environment as unknown until the available runtime selectors are confirmed.
- Do not assume the active or default profile is the right selector for the task.
- Run `volclog configure list` only when local profile discovery is actually relevant.
- Use `region` only when the contract or task explicitly requires an override. Do not infer it from endpoint or domain.
- `--profile`/`context.profile` and `--secrets-file`/`context.secrets_file` are runtime selectors, not business input fields.
- A non-empty explicit profile must exist and supply its static credentials or dynamic provider. For an explicit profile, region and endpoint resolve from invocation overrides, then the profile, then project defaults. See [profile and credential selection](references/best-practices.md#profile-and-credential-selection) for implicit selection and an example.
- Global `--region`/`--endpoint` and context `region`/`endpoint` override profile, applicable environment, and project defaults. Different non-empty global and context values for the same field fail validation.
- Context validation rejects unknown fields, wrong types, and conflicting aliases before applying the context. Legacy trace boolean/string/object and execution artifact/output/projection forms remain supported; page controls are for supported tools, not workflows.
- Non-TTY runs are automatically non-interactive; `--non-interactive` or `context.execution.non_interactive=true` also enables the mode. Context `false` cannot turn off automatic non-TTY mode and conflicts with global `true`.
- `--timeout 30s` or `context.execution.timeout="30s"` supplies an optional positive Go-duration budget from invocation start, shared across pages and batches. It is separate from profile `timeout_seconds`; equal global/context durations are accepted, different ones conflict.
- A global flag that needs a separate value will not consume a following `--flag`; prefix a relative path beginning with `--` with `./`.
- Never read, print, export, or persist AK/SK or token values in prompts, memory, request artifacts, or skill content.

## Canonical Agent Loop

Discover -> Describe -> Exec -> Read Result

1. Resolve runtime selectors.
   - decide whether this run is using profile, secrets file, or environment credentials
   - if local profile discovery matters, run `volclog configure list`
   - use one selector family per invocation. For process-scoped AK/SK, confirm that implicit selection resolves to static mode.
2. Discover the callable surface.
   - `volclog tool list`
   - `volclog tool list <group>`
   - `volclog workflow list <group>`
3. Read the contract.
   - `volclog tool describe <group.action> --view input` returns the complete sectioned request schema and operation capabilities
   - when you need context or execution fields, follow `schema_ref.command` (`volclog tool schema`) and reuse the shared schemas by `schema_digest`
   - use `--view full` for a self-contained contract or on a CLI without `--view input`. Check `volclog tool describe --help` if support is unclear.
   - `volclog workflow describe <group.command>`
4. Execute with structured input.
   - `volclog tool exec <group.action> --input '{...}'`
   - `volclog workflow exec <group.command> --input '{...}'`
   - `volclog raw --method <METHOD> --path <PATH> --body '{...}'`
5. Read result in this order.
   - `status` (`"success"` or `"failed"`)
   - `summary.remoteOutcome` and `summary.deliveryOutcome`, where present
   - `summary.deliveryMode`
   - the flat `error` object (when `status` is `"failed"`)
   - `summary.completeness`, then business fields under `data` and any `artifacts`; failed results may retain partial data or a partial artifact

## Reference Escalation

- Do not read every reference file by default.
- Read `tool describe` or `workflow describe` first.
- Read [references/routing.md](references/routing.md) only when the surface is still unclear after describe or list.
- Read [references/sops.md](references/sops.md) only when the task clearly spans multiple calls and you need stop conditions.
- Read [references/best-practices.md](references/best-practices.md) only when runtime semantics, delivery mode, or recovery posture are still unclear after the contract is known.
- `contract_cache_hint` and `contract_digest` come from `tool describe` or `workflow describe` output. Read `contract_cache_hint.safe_scope` and `contract_cache_hint.refresh_when`, and reuse cached results only while the same CLI build still reports the same `contract_digest`.
- Stop after one reference resolves the current uncertainty; do not keep browsing all references “just in case”.

## Key Rules

- Do not guess command names, tool ids, workflow ids, JSON fields, or output shape.
- If the action id is still unknown, run `volclog tool list <group>` or `volclog workflow list <group>` before guessing.
- Let the operation's `risk`, `recovery`, `recovery_source`, `recovery_notes`, and `usage_constraints` override the generic recovery map below; use full describe for details, input policy, documentation source, and API default-header metadata.
- `reviewed-read-contract` identifies a reviewed read or non-persistent purpose, not proven service idempotency or automatic retry permission. Reconcile unverified outcomes before considering another attempt.
- Treat `high-risk-retry` as a warning that retrying is high risk. After an ambiguous result, reconcile only through a get/describe/list action named by the contract; otherwise stop and escalate instead of retrying automatically.
- Do not use human shortcut commands as the primary agent flow.
- Do not repeat schema details that already exist in `tool describe` or `workflow describe`.
- Do not rebuild a valid `tool exec` or `workflow exec` request into flag-heavy shortcut form just because the shell path looks shorter.
- Do not pipe `volclog` output into `jq` or `grep` just to rediscover schema or field paths.
- Read the full JSON envelope first; use `--jmes-filter` only when you already know the envelope path you want.
- Prefer `--dry-run` before any write or destructive change, but only on `raw`, `tool exec`, or `workflow exec`.
- TLS writes and unknown raw operations, including unknown GETs, fail with `permission` / `WritePermissionRequired` unless the CLI write gate is enabled. Only for a user-authorized write, use `--allow-write`; `tool exec` / `workflow exec` can instead use `context.execution.allow_write=true`, but raw has no context. This is not an IAM grant. Reviewed reads such as POST `log.search` and dry-runs do not need it; human shortcuts use the same gate. Global `true` conflicts with explicit context `false`.
- Treat `--dry-run` as contract or plan validation, not proof that the business query is correct.
- Trace redaction accepts only `on`/`true`/`1`/`yes`/`enabled`/`strict`/`default`; `off`/`false`/`0`/`no`/`disabled` and unknown values fail. `context.trace=false` or `enabled:false` disables a global trace directory.
- Use non-interactive device-code login when the session can keep the process running and show stderr to the user as it arrives. Set a whole-login `--timeout` that gives the user time to authorize. If the host cannot keep the process alive, have the user run a fresh login in a foreground terminal with the intended profile, Region, and Endpoint. Session replacement requiring confirmation returns `interaction_required`. Use another profile or an interactive terminal. SSO automatically selects a single candidate and requires an explicit choice when there are several.
- Darwin/Linux interactive prompts use cancellable polling. A Windows console without read-deadline support returns `interaction_required`; provide explicit selectors to avoid the prompt. Runtime verification of this behavior has been on Darwin only.
- SIGINT, SIGTERM, and `RunContext` cancellation propagate. Before send the outcome is `not_sent`; after send an uncertain result remains `unknown`. Preserve completed partial results and reconcile ambiguous writes before any retry.
- A dry-run `request_preview` masks known sensitive keys recursively, including sensitive `Contents`/`LogTags` key/value entries; malformed bodies are omitted and `redacted_fields` may list paths. This affects only the preview, not the wire request or body hash. Ordinary body strings are not guaranteed to be recognized as secrets.
- Malformed JSON-like query values use `[unparseable query omitted]`; displayed endpoints omit user information, query, and fragment.
- `summary.remoteOutcome` distinguishes `not_sent`, `unknown`, `accepted`, `success`, `failed`, and `partial`; `summary.deliveryOutcome` is independently `success` or `failed`. Download-task creation reports `accepted`, not completion; inspect `summary.async` and the task-specific `summary.asyncTasks` on a later status query. The CLI does not wait automatically; see [references/sops.md](references/sops.md) for the single-call next steps.
- `summary.pagination.merged=true` means pages were accumulated, not that they are complete. Preserve service `data.Total`; `summary.itemCount` counts collected rows. Inspect `summary.completeness` (`complete`, `stopReason`, `pageCount`, `itemCount`, `resumable`, and optional `next`). A page-all/export budget or later failure returns nonzero with collected data retained when available.
- If `next` is present, execute its `operationId` and `input` as a single `tool exec` request with the same runtime selectors and page-all disabled (`next.pageAll=false`); do not invent SQL `OFFSET` for continuation. SQL/analysis is complete only for `ResultStatus=complete`; `ListOver` applies to pure search. `incomplete`/`time_out` are incomplete and `error` is failure. User-authored SQL `LIMIT`/`OFFSET` calls are separate queries without a shared-snapshot guarantee.
- A retained streamed partial uses a unique partial data file and `.meta.json` sidecar; inspect the artifact's `completeness` and `metadataPath`. The completed target is preserved. If sidecar delivery fails, use the retained data path and error.
- For batch-ingest failures, inspect `summary.progress` or `data.progress`: `confirmedBatches`, `confirmedLogs`, zero-based exclusive `confirmedLogOffset`, one-based active `currentBatch`, `currentBatchOutcome`, and `lastConfirmedRequestId`. Only confirmed success responses count. HTTP 200 followed by a read or protocol failure is `unknown`; never treat an unknown batch as an automatically safe resume point. Current request ID and status refer to the current attempt, not an earlier confirmed batch.
- Standard catalog JSON responses require HTTP 200 and valid JSON when the body is non-empty. Empty responses and special codec, `raw`, and protobuf paths retain their existing 2xx/text/empty/PB contracts. The CLI adds no retry here; existing `http.Client`/`Transport` and custom-transport behavior still applies.
- A stdout write failure returns exit code 3 and reports a structured error to stderr on a best-effort basis; broken stdout cannot guarantee JSON. Exit codes 1, 2, and 3 keep their existing roles.
- If fields are uniquely named, prefer flat JSON input before manual nesting. `tool exec` applies the declared schema consistently to flat and sectioned inputs; sectioned extra top-level keys are rejected, section fields follow `additionalProperties`, and an operation without a section schema remains passthrough. Defaults are not injected. Human shortcuts retain legacy validation.
- `workflow exec` rejects context `execution.page` and `execution.page_all` even when false; use the workflow's own documented controls.

## Error Recovery Quick Map

Use this table as the default first response after a failed envelope only after reading the operation's `risk`, `recovery`, and `usage_constraints`. The operation contract takes precedence. Read `references/best-practices.md` only when you need runtime-specific detail beyond the quick action below.

| Signal | Next action |
| --- | --- |
| `error.kind=validation` | Go back to `describe` and fix shape or selector problems first. |
| `error.kind=auth` or `401` | Re-check runtime selector, then credentials, then endpoint or region override. |
| `permission` / `WritePermissionRequired` | For a user-authorized write or unknown raw operation, enable the CLI write gate; keep IAM checks separate. Do not add permission to reviewed reads or dry-runs. |
| `403 Forbidden` | Re-check tenant or profile alignment before widening the permission search. |
| `error.kind=usage` | Re-run `tool list` or `workflow list`; do not invent aliases. |
| `error.kind=unsupported_feature` | Read `error.hint`, then remove the unsupported flag or switch to the named surface; non-LogApp Topic resolution should switch to `app.resolve-resources`. |
| `error.kind=incompatible_flags` | Remove one conflicting flag and rerun. |
| `error.kind=filesystem` or `output_failed` | Fix delivery; inspect `remoteOutcome` and do not repeat the original write solely because output failed. |
| `error.kind=result_unknown` or `protocol` | Reconcile the remote state before considering another write. A transient error alone does not make retry safe. |
| `error.kind=partial_failure` | Inspect confirmed progress and the current batch outcome; reconcile unknown batches before any resume. |
| `error.kind=not_found`, `conflict`, or `throttled` | Inspect the operation contract and current state; do not infer retry safety from the kind alone. |
| `error.kind=decode` | Read `error.hint` and inspect the filter or projection; do not repeat a successful write to repair delivery. |
| `error.kind=server` | Read `error.code`, `error.requestId`, and `remoteOutcome`; retry only when the operation contract and reconciled state permit it. |
| `error.kind=config` | Run `volclog doctor` for host-side configuration checks, then inspect the reported runtime issue. |
| `error.kind=unknown` | Read `error.hint`, `error.message`, and `error.details`; reconcile before repeating a write. |
| `ResultStatus=incomplete` / `time_out` / `error` | Inspect completeness and the error; do not treat partial rows or an empty result as complete. Narrow the time window when appropriate. |

`volclog doctor` checks host/runtime configuration such as credentials, endpoint, and local setup. It does not replace `describe`, and it does not validate business query semantics.

## Large Result Handling

- Keep surface choice and delivery choice separate.
- When a result may be large, provide a writable `--output-dir`.
- Let `summary.deliveryMode` tell you what actually happened at runtime.
- For successful default `raw`/`tool exec`/`workflow exec` file delivery, stdout is a JSON envelope with `data=null` and the file path in `artifacts[].path`; read the business result from the file. Human stdout retains the Chinese notice.
- Read [references/best-practices.md](references/best-practices.md) for exact `outputMode`, `deliveryMode`, file delivery, and `--jmes-filter` semantics instead of duplicating them here.

## Quick Map

| Need | Prefer |
| --- | --- |
| Public API contract already chosen | `volclog tool describe <group.action> --view input` / `volclog tool exec <group.action>` |
| CLI workflow already chosen | `volclog workflow describe <group.command>` / `volclog workflow exec <group.command>` |
| Need to discover action or workflow ids | `volclog tool list <group>` / `volclog workflow list <group>` |
| Need help choosing the surface | read `references/routing.md` |
| Need multi-step execution order | read `references/sops.md` |
| Resolve an App resource graph | `volclog workflow describe app.resolve-resources` / `volclog workflow exec app.resolve-resources` |
| Resolve Topic IDs for a LogApp App | `volclog workflow describe app.resolve-topic-ids` / `volclog workflow exec app.resolve-topic-ids` |
| Need runtime/error/recovery semantics | read `references/best-practices.md` |
| Use an exact method/path | `volclog raw --method ... --path ...` |
| Authenticate a stateless run | host-selected local profile -> one-shot `--secrets-file` or `context.secrets_file` |
| Need contract reuse safety | reuse cached results only while the same CLI build still reports the same `contract_digest` |
