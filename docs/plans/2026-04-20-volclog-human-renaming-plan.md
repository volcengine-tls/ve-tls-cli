# Volclog Human Renaming Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `volclog` the default agent/CI edition, rename the human shortcut edition to `volclog-human`, and remove all `volclog-agent` naming and packaging.

**Architecture:** Keep the current dual-surface model but flip the defaults: the untagged build becomes the agent surface and the `human` build tag enables shortcut groups. Release assets, install scripts, npm wrappers, and docs all follow the same naming so users never need to infer which binary is agent-first.

**Tech Stack:** Go CLI, build tags, GitHub Actions, shell install scripts, npm package wrappers, Markdown docs.

### Task 1: Flip edition defaults in runtime code

**Files:**
- Modify: `internal/cli/edition.go`
- Modify: `internal/cli/edition_full.go`
- Modify: `internal/cli/edition_agent.go`
- Modify: `internal/cli/edition_group_full.go`
- Modify: `internal/cli/edition_group_agent.go`
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/edition_test.go`
- Modify: `internal/cli/edition_agent_help_test.go`
- Modify: `internal/cli/edition_agent_dispatch_test.go`
- Modify: `internal/cli/edition_agent_source_test.go`

**Steps:**
1. Rename the build-tagged human edition so the untagged build is the agent surface and `//go:build human` enables shortcut groups.
2. Update edition enums and helper text from `agent/full` to `default/human` or equivalent internal wording while keeping user-facing names `volclog` and `volclog-human`.
3. Rewrite tests to assert:
   - default build only exposes `configure/doctor/skill/tool/workflow/raw`
   - `-tags=human` exposes shortcut groups
   - help and edition errors mention `volclog-human`, not `volclog-agent`

### Task 2: Rename release assets and local installers

**Files:**
- Modify: `.github/workflows/release-volclog.yml`
- Modify: `.github/workflows/ci.yml`
- Modify: `scripts/install-binary.sh`
- Modify: `scripts/install-binary_test.sh`
- Modify: `scripts/install.ps1`

**Steps:**
1. Change release packaging from `volclog` + `volclog-agent` to `volclog` + `volclog-human`.
2. Replace `agent` edition selection with `human`; default install path becomes `volclog`.
3. Update installer tests to verify default installs `volclog` and explicit human installs `volclog-human`.

### Task 3: Rename npm human package and remove old agent package naming

**Files:**
- Modify: `package.json`
- Modify: `package-lock.json`
- Rename/Modify: `npm/agent-package/**` to `npm/human-package/**`
- Modify: `npm/test/*.js`

**Steps:**
1. Keep root package as `@volcengine-tls/volclog` for default agent behavior.
2. Rename the secondary package to `@volcengine-tls/volclog-human`.
3. Update wrapper scripts, binary paths, fixture names, and tests from `volclog-agent` to `volclog-human`.
4. Remove any remaining `volclog-agent` package mentions.

### Task 4: Rewrite user-facing docs around the new names

**Files:**
- Modify: `README.md`
- Modify: `README_CN.md`
- Modify: `docs/cli-best-practices.md`
- Modify: `docs/cli-practical-guide.md`
- Modify: `docs/cli-human-shortcuts.md`
- Modify: `docs/agent-blackbox-evaluation-checklist.md`
- Modify: `skills/volclog-core/**` if naming is user-visible

**Steps:**
1. Replace `volclog-agent` with `volclog` in agent-first sections.
2. Rename human/full wording to `volclog-human` consistently.
3. Update install and npm examples to reflect:
   - default `volclog`
   - optional `volclog-human`
4. Keep human-only guidance at the end or in dedicated docs.

### Task 5: Verify end-to-end behavior

**Files:**
- Modify tests only if verification exposes drift

**Steps:**
1. Run `go test ./...`.
2. Run `go test ./... -tags=human`.
3. Run `npm run test:npm --silent` or the direct node test fallback if needed.
4. Run `bash scripts/install-binary_test.sh`.
5. Build both binaries:
   - `go build -o /tmp/volclog ./cmd/volclog`
   - `go build -tags=human -o /tmp/volclog-human ./cmd/volclog`
6. Confirm no `volclog-agent` strings remain in shipped paths with targeted `rg`.
