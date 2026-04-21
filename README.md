# volclog-cli

[中文版](README_CN.md) | [English](README.md)

The official Volcengine TLS CLI. `volclog` is the default agent and automation edition with the contract-first `tool / workflow / raw` path, while `volclog-human` keeps the human shortcut layer for frequent interactive work.

**What volclog provides**

- **Agent-native contract path** — Use `tool describe/exec`, `workflow describe/exec`, and `raw` instead of guessing flags or request shapes.
- **Dual editions** — Ship both `volclog` (agent/CI focused) and `volclog-human` (human-friendly shortcut layer) while keeping the same `tool / workflow / raw` runtime semantics.
- **Broad TLS coverage** — Covers projects, topics, indexes, search and analysis, alarms, host groups, collectors, ETL, consumer groups, and more.
- **Safer execution flow** — `--dry-run`, structured envelopes, trace artifacts, and file delivery make preview, validation, and recovery easier.
- **Flexible credential setup** — Supports local profiles, explicit region/endpoint, environment variables, and one-shot `--secrets-file` injection.
- **Layered usage** — Start with `tool / workflow / raw`; use human shortcuts only when you intentionally want the full interactive layer.

---

## Features

| Category | Capabilities |
| --- | --- |
| 📁 **Project** | Create, query, update, and delete log projects |
| 📚 **Topic** | Create, query, update, and delete log topics and manage lifecycle settings |
| 🔍 **Index & Log** | Manage indexes, run log search, histogram preview, SQL analysis, and export large result sets |
| 📈 **Metric Topic** | Create metric topics and query metrics through PromQL-compatible APIs |
| 🚨 **Alarm** | Configure alarm policies, templates, and notification channels |
| 🔄 **Consumer / Shipper** | Manage consumer groups and ship data to downstream storage systems |
| 🛠 **ETL / Processor** | Clean, enrich, distribute, and transform logs |
| 🌐 **Host / Collector** | Manage host groups and LogCollector rules |

---

## Installation & Quick Start

### Prerequisites

Before you start, make sure you have:

- A terminal environment for your operating system
- Your Volcengine AK (Access Key ID) and SK (Secret Access Key)
- An explicit target `region` such as `cn-beijing`
- The matching TLS endpoint such as `https://tls-cn-beijing.volces.com`

`region` must be provided explicitly. The CLI does not infer region from endpoint or hostname.

### Quick Start (AI Agent)

This is the recommended path for Agent, CI, and automation.

Commands in this section use `volclog` by default.

#### 1. Install

Recommended: install the default `volclog` binary:

```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download \
bash scripts/install-binary.sh
```

You can also install the human shortcut edition explicitly:

```bash
bash scripts/install-binary.sh --edition human
```

`volclog` only exposes:

- `configure`
- `doctor`
- `skill`
- `tool`
- `workflow`
- `raw`

If you install via npm, install the default package first:

```bash
npm install -g @volcengine-tls/volclog
```

Install `@volcengine-tls/volclog-human` only if you explicitly need the human shortcut layer.

#### 2. Configure And Verify

Set up a local profile or inject one-shot credentials, then verify with `doctor`:

```bash
volclog configure set \
  --profile default \
  --ak <ak> \
  --sk <sk> \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com

volclog doctor
```

For stateless runs, prefer one-shot `--secrets-file` over broad environment injection. Do not combine `--profile` and `--secrets-file` in the same run.

#### 3. Discover Contracts Before Execution

Default `volclog` only exposes readonly agent actions. If the task needs create/modify/delete/import, switch to `volclog-human`.

Use `tool` and `workflow` to discover the contract before guessing input:

```bash
volclog tool list
volclog tool list project
volclog tool describe project.describe-projects

volclog workflow list
volclog workflow describe log.export
```

Use `raw` only when the exact `method/path` is already known:

```bash
volclog raw --method POST --path /SearchLogs --body file://req.json
```

`raw` also accepts `--input` as a compatibility alias for `--body`, but `--body` and `--input` must not be passed together.

#### 4. Dry Run, Execute, And Filter

Validate first, then execute:

```bash
cat >ctx.json <<'EOF'
{
  "region": "cn-beijing",
  "execution": { "dry_run": true }
}
EOF

cat >req.json <<'EOF'
{
  "ProjectName": "demo"
}
EOF

volclog tool exec project.describe-projects --context file://ctx.json --input file://req.json
```

For large results, prefer file delivery:

```bash
volclog --output-mode file --output-dir ./out \
  workflow exec log.export-analysis --input file://req.json
```

For envelope projection, use `--jmes-filter` on stdout-only runs:

```bash
volclog tool exec project.describe-projects \
  --jmes-filter "data.Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
```

`--jmes-filter` runs on the complete CLI envelope, so paths such as `data.*`, `summary.*`, and `error.*` are valid. If the selected field exists but is `null`, stdout prints the literal `null` and the command still succeeds. Missing paths, including out-of-bounds array indexes, fail with `filter matched no value`. It cannot be combined with file delivery.

### Quick Start (Human Users)

If you are working directly in a terminal and want the shortcut layer, install `volclog-human`.

#### 1. Install

**Option 1: Download Binary (Recommended)**

```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download \
bash scripts/install-binary.sh --edition human
```

**Option 2: Install via npm**

```bash
npm install -g @volcengine-tls/volclog-human
```

**Option 3: Install with Go**

Requires Go 1.22+.

```bash
go build -tags=human -o /usr/local/bin/volclog-human ./cmd/volclog
```

**Option 4: Install from Local Source**

```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
VOLCLOG_EDITION=human bash scripts/install-local.sh
```

#### 2. Configure Credentials

```bash
volclog-human configure
```

#### 3. Start Using

```bash
volclog-human project list
volclog-human topic list --project-id <your-project-id>
```

For the full shortcut layer, see [docs/cli-human-shortcuts.md](docs/cli-human-shortcuts.md).

---

## Agent Skills

The repository includes one bundled agent skill package under `skills/`:

| Skill | Description |
| --- | --- |
| **volclog-core** | Agent-only incremental knowledge for intent routing, cross-group SOPs, runtime semantics, recovery posture, and stateless credential guidance beyond `tool describe` / `workflow describe` |

Install it into your agent skill directory:

```bash
volclog skill install --dir <agent-skills-dir>
```

For one-off installs, `npx` also works:

```bash
npx @volcengine-tls/volclog skill install --dir <agent-skills-dir>
```

---

## Advanced & Best Practices

- **Prefer `tool / workflow / raw` for agent flows** — Human shortcuts remain in `volclog-human`, but they are not the default agent path.
- **Read the contract before execution** — Start with `tool describe` or `workflow describe`, then build `context` and `input`.
- **Use `--dry-run` for writes** — Preview request shape and runtime selection before sending mutating calls.
- **Use file delivery for large results** — Prefer `--output-mode file --output-dir <writable-dir>` when stdout may be too large.
- **Understand runtime signals** — `summary.deliveryMode` tells you whether the result stayed on stdout or was written to file.
- **Use the flat error object** — On failures, read `error.kind`, `error.code`, `error.message`, and `error.details` in that order.
- **Keep region explicit** — Do not assume endpoint-derived region inference.

Further reading:

- [docs/cli-practical-guide.md](docs/cli-practical-guide.md)
- [docs/cli-best-practices.md](docs/cli-best-practices.md)
- [docs/cli-human-shortcuts.md](docs/cli-human-shortcuts.md)

---

## Security & Contributing

- **Security** — Avoid hardcoding plaintext AK/SK in command arguments. Prefer local profiles, one-shot `--secrets-file`, or scoped environment injection.
- **Region / endpoint discipline** — Always set `region` explicitly. The CLI does not infer it from endpoint or hostname.
- **Contributing** — When changing the public tool catalog, regenerate it with:

  ```bash
  go run ./internal/openapigen --spec repos/docs/swagger.json
  ```

  Then run:

  ```bash
  go test ./...
  ```
