---
name: volclog-core
description: Use when operating volclog as an agent and you need intent routing, cross-group SOPs, recovery recipes, profile selection, or large-result control beyond tool describe and workflow describe.
---

# Volclog Core

## Overview

Use this skill only for agent-only incremental knowledge.

If `volclog tool describe ...` or `volclog workflow describe ...` already answers the question, stop and follow that contract instead of adding duplicate guidance here.

Treat this skill as the thin delta above runtime contracts:

- `routing`: natural-language intent to `tool / workflow / raw`
- `sops`: reusable cross-group workflows and stop conditions
- `best-practices`: recovery recipes, token control, profile switching, stateless credential injection, and known traps

## Load Order

1. Start from `tool list/describe` or `workflow list/describe`.
2. When `tool describe` or `workflow describe` returns `contract_cache_hint`, reuse the cached contract only while the same CLI build still reports the same contract_digest; refresh immediately after digest changes, CLI/build changes, or contract-mismatch style execution errors.
3. Read [references/routing.md](references/routing.md) when natural-language intent does not obviously map to a `tool` or `workflow` id.
4. Read [references/sops.md](references/sops.md) for multi-step tasks that span more than one group.
5. Read [references/best-practices.md](references/best-practices.md) for token control, profile switching, stateless agent/CI credential injection, recovery recipes, and known traps.

## Hard Rules

- Do not use human shortcut commands as the primary agent flow.
- Do not repeat schema details that already exist in `tool describe` or `workflow describe`.
- Prefer `tool` for published public APIs, `workflow` for CLI-owned high-level flows, and `raw` only when method/path is already known or no public contract exists.
- Prefer `--dry-run` before any write or destructive change.
- Prefer file or artifact output for large responses.
- Never persist AK/SK or token values in the skill, prompt, session memory, or request artifacts. For stateless agents and CI, ask the host or broker for a one-shot `--secrets-file` or process-scoped environment injection instead.

## First Response Checklist

1. Identify whether the task is public API execution, CLI-owned workflow, or exact transport call.
2. Read the matching `tool describe` or `workflow describe` contract before constructing input.
3. If the task is write, destructive, or account-sensitive, confirm `profile` and start with `--dry-run`.
4. If the result may be large, choose file or artifact output before execution.

## Quick Reference

| Need | Prefer |
| --- | --- |
| Discover public API actions | `volclog tool list <group>` |
| Inspect public API contract | `volclog tool describe <group.action>` |
| Execute public API contract | `volclog tool exec <group.action>` |
| Use CLI-owned ingest/export workflow | `volclog workflow describe/exec <group.command>` |
| Call an exact method/path | `volclog raw --method ... --path ...` |
| Authenticate a stateless agent/CI run | host-selected local profile -> one-shot `--secrets-file` or `context.secrets_file` |

## Common Mistakes

- Using shortcut `project/topic/index/log ...` as the default agent entry.
- Treating `page.all` as an output-shrinking flag, or assuming every list action supports it. Use it only when the contract says `execution.supports_all=true`.
- Writing `data.Total` in `projection`; filters run on the raw result, not the CLI envelope.
- Forgetting `tool describe --view full` when compact output omits low-frequency optional nesting.
- Guessing `ingest` or `export` as `tool` ids; these belong to the `workflow` surface.
- Injecting broad environment variables into the whole sandbox and assuming `profile` still chooses the account. Environment credentials override profile resolution.
