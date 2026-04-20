---
name: volclog-core
description: Use when operating volclog as an agent and you need routing, cross-group SOPs, runtime recovery, or stateless credential guidance beyond tool describe and workflow describe.
---

# Volclog Core

## Overview

Use this skill only for agent-only incremental knowledge.

If `volclog tool describe ...` or `volclog workflow describe ...` already answers the question, stop and follow that contract instead of adding duplicate guidance here.

If both binaries are available, prefer `volclog` for agent or CI sessions. If only `volclog-human` is installed, stay on `tool / workflow / raw` and ignore human shortcut groups.

## Read Order

1. Read `tool describe` or `workflow describe` first.
2. If the contract returns `contract_cache_hint`, reuse cached results only while the same CLI build still reports the same `contract_digest`.
3. Read [references/routing.md](references/routing.md) only when the intent is still ambiguous.
4. Read [references/sops.md](references/sops.md) only for multi-step tasks.
5. Read [references/best-practices.md](references/best-practices.md) for runtime semantics, recovery, credential injection, and traps.

## Hard Rules

- Do not use human shortcut commands as the primary agent flow.
- Do not repeat schema details that already exist in `tool describe` or `workflow describe`.
- Use `tool` for published public APIs, `workflow` for CLI-owned orchestration, and `raw` only when method/path is already known.
- Prefer `--dry-run` before any write or destructive change.
- Never persist AK/SK or token values in prompts, memory, request artifacts, or skill content.

## Quick Map

| Need | Prefer |
| --- | --- |
| Public API contract already chosen | `volclog tool describe <group.action>` / `volclog tool exec <group.action>` |
| CLI workflow already chosen | `volclog workflow describe <group.command>` / `volclog workflow exec <group.command>` |
| Need help choosing the surface | read `references/routing.md` |
| Need multi-step execution order | read `references/sops.md` |
| Need runtime/error/recovery semantics | read `references/best-practices.md` |
| Use an exact method/path | `volclog raw --method ... --path ...` |
| Authenticate a stateless run | host-selected local profile -> one-shot `--secrets-file` or `context.secrets_file` |

## First Response Loop

1. Pick the surface: `tool / workflow / raw`.
2. Read the contract.
3. Confirm `profile`, explicit region, and credential injection strategy. Do not infer region from endpoint/domain.
4. Run with `--dry-run` when the operation mutates state.
5. Let CLI runtime signals such as `summary.deliveryMode` and the flat `error` object drive the next step.
