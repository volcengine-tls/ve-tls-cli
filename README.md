# volclog-cli

[中文版](README_CN.md) | [English](README.md)

The official Volcengine TLS (Tencent Log Service) CLI tool, built around an Agent-native `tool / workflow / raw` path while still keeping a full human-oriented edition with shortcuts and templates.

**What volclog provides**

- **Agent-first by default** — The primary execution path is `tool / workflow / raw`; Agents do not need to learn shortcut flags first.
- **Dual editions** — Releases publish `volclog-agent` for Agent/CI usage and `volclog` for the full human-oriented surface; both keep the same `tool`, `workflow`, and `raw` contracts.
- **Agent integration** — Includes optional skills so common TLS workflows can be handed to LLM-driven tooling or automation.
- **Broad coverage** — Covers 20+ domains; the full edition still includes human shortcuts for common tasks.
- **Clearer command constraints** — `tool/workflow describe`, `--dry-run`, structured output, and optional shortcut templates make it easier to inspect a call before sending it.
- **Straightforward setup** — Supports binary install, source build, local profiles, environment variables, and `--secrets-file`.
- **Reasonable safety defaults** — Includes `--dry-run`, redacted trace output, and environment-isolation related options.
- **Layered usage** — shortcut / tool / workflow / raw / skills are all available, so you can pick the level you need.

---

## Features

| Category | Highlights |
| --- | --- |
| 📁 **Project** | Create, query, update, and delete log projects |
| 📚 **Topic** | Create, query, update, and delete log topics, manage log lifecycle |
| 🔍 **Index & Log** | Create and manage indexes, powerful log search capabilities, SQL analytics export, and raw log download |
| 📈 **Metric Topic** | Create metric topics, support monitoring metric queries based on PromQL |
| 🚨 **Alarm** | Configure alarm policies and notification channels to ensure seconds-level alerts |
| 🖥️ **Dashboard** | Create and manage visual dashboards for real-time data insights |
| 🔄 **Consumer/Shipper** | Manage consumer groups, configure data shipping to external storage (e.g., TOS, Kafka) |
| 🛠 **ETL/Processor** | Log cleaning, enrichment, distribution, and formatting |
| 🌐 **Host/Collector** | Manage log collector host groups, configure and dispatch LogCollector rules |

---

## Installation & Quick Start

### Prerequisites

Before you start, make sure you have:
- A terminal environment for your operating system
- Your Volcengine AK (Access Key ID) and SK (Secret Access Key)
- Target Region (e.g., `cn-beijing`) and Endpoint (e.g., `https://tls-cn-beijing.volces.com`)

### Quick Start (AI Agent)

This is the primary path for Agent / CI / automation usage.

#### Step 1 — Install
```bash
# If in a development environment
bash scripts/install-local.sh
```

For the released agent edition, install `volclog-agent` instead of the full `volclog` binary:
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download VOLCLOG_EDITION=agent bash scripts/install-binary.sh
```

`volclog-agent` keeps the operational surface focused on `configure`, `doctor`, `skill`, `tool`, `workflow`, and `raw`.
Human shortcut groups such as `project`, `topic`, `log`, `host-group`, and `collector` stay in the full `volclog` edition.

If the npm package is available, you can also install it directly:

```bash
npm install -g @volcengine-tls/volclog
```

The npm package currently installs the full `volclog` edition. If you need the reduced Agent-only command surface, install the released `volclog-agent` binary instead.

#### Step 2 — Discover Tools & Contracts
For unfamiliar work, prefer the `tool / workflow` contract surfaces first, then fall back to raw transport only when method/path is already known:
```bash
# Check available tool groups (public API discovery)
volclog tool list
# Check action set in a group
volclog tool list project
# Check available workflow groups (CLI-owned orchestration)
volclog workflow list
# View the machine contract for one action
volclog tool describe project.create
# View the machine contract for one workflow
volclog workflow describe log.export
```

If you already know the transport method/path, use the expert raw surface:
```bash
# Direct transport call:
volclog raw --method POST --path /CreateProject --body file://req.json
```

#### Step 3 — Dry Run & Execute
Prepare `context` + `input` files and run with `execution.dry_run` in context first:
```bash
cat >ctx.json <<'EOF'
{
  "region": "cn-beijing",
  "execution": {"dry_run": true}
}
EOF
cat >req.json <<'EOF'
{
  "ProjectName": "test",
  "Region": "cn-beijing"
}
EOF
volclog tool exec project.create --context file://ctx.json --input file://req.json
```
Once it looks correct, remove `dry_run` from `ctx.json` and run it for real.

#### Step 4 — Data Filtering
Use `--jmes-filter` to extract key data and prevent long lists from blowing up your context window:
```bash
volclog tool exec project.describe-projects \
  --input '{"ProjectName":"demo"}' \
  --jmes-filter "data.Projects[].{Id: ProjectId, Name: ProjectName}"
```

Notes:
- `--jmes-filter` runs on the full CLI envelope, so `data.*`, `summary.*`, and `error.*` are all valid paths.
- If the selected field exists but its value is `null`, stdout returns literal `null` and the command still succeeds.
- Failed executions use one flat `error` object. Prefer `error.kind`, `error.code`, `error.message`, and `error.details` instead of reparsing error text.

---

### Quick Start (Human Users / Full Edition)

If you are using the CLI directly as a human operator, install the full edition and treat shortcuts as an optional convenience layer on top of the same `tool / workflow / raw` foundation.

#### 1. Install
**Option 1: Download Binary (Recommended)**
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download bash scripts/install-binary.sh
```

To install the agent edition instead:
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download VOLCLOG_EDITION=agent bash scripts/install-binary.sh
# or
bash scripts/install-binary.sh --edition agent
```

On Windows PowerShell:
```powershell
pwsh -File scripts/install.ps1
pwsh -File scripts/install.ps1 -Edition agent
```

**Option 2: Install via npm**
```bash
npm install -g @volcengine-tls/volclog
```

**Option 3: Install with Go**
Requires Go v1.22+.
```bash
go install github.com/volcengine-tls/ve-tls-cli/cmd/volclog@latest
```

**Option 4: Install from Local Source**
```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
bash scripts/install-local.sh
```

#### 2. Configure Credentials
Run the configure command and enter your AK, SK, Region, and Endpoint when prompted:
```bash
volclog configure
```

#### 3. Start Using
```bash
volclog project list
volclog topic list --project-id <your-project-id>
```

If you specifically want the human shortcut path and template-oriented workflow, go straight to:

👉 **[Human Shortcut Guide](docs/cli-human-shortcuts.md)**

## Agent Skills

The repository includes a directly installable Agent Skill under `skills/`:

| Skill | Description |
| --- | --- |
| **volclog-core** | Agent-only incremental knowledge beyond `tool describe` / `workflow describe`: intent routing, cross-group SOPs, recovery recipes, profile selection, and large-result control |

`volclog-core` stays thin and is split into:

- `routing`: natural-language intent to `tool / workflow / raw`
- `sops`: common cross-group workflows
- `best-practices`: recovery recipes, known traps, and token / large-result control

**Install Skills:**
```bash
volclog skill install --dir /path/to/agent/skills
```

For a one-off install, `npx` works as well:

```bash
npx @volcengine-tls/volclog skill install --dir /path/to/agent/skills
```

---

## Advanced & Best Practices

Beyond the basic commands, a few features are especially useful in troubleshooting and automation:

- **Automated Discovery**: Read machine contracts via `tool describe` / `workflow describe`, and use shortcut `--print-request-template` only when you need a human-oriented JSON skeleton.
- **Safe Pre-execution**: Use `--dry-run` to intercept network requests and locally validate JSON syntax and required parameters.
- **Flexible Payload Inputs**: Pass JSON payloads via inline strings, local files (`file://...`), or standard input (`-`) streams.
- **Troubleshooting & Redaction**: Generate sanitized Trace artifacts using `--trace-dir` combined with `--trace-redact strict`.
- **Big Data Streaming**: Process massive log exports and SQL analysis results efficiently with `--output jsonl` and `--output-mode file`.
- **Multi-Account Isolation**: Inject localized environment variables via `--secrets-file ./.env`.

For the Agent-first practical flow, start with:

👉 **[CLI Practical Guide](docs/cli-practical-guide.md)**

For shared runtime/output/security guidance:

👉 **[CLI Best Practices & Scenario Guide](docs/cli-best-practices.md)**

For full-edition human shortcut usage:

👉 **[Human Shortcut Guide](docs/cli-human-shortcuts.md)**

---

## Security & Contributing

- **Security**: Avoid hardcoding plaintext AK/SK in command arguments. Use `volclog configure`, inject them via environment variables (`VOLCENGINE_ACCESS_KEY_ID`, `VOLCENGINE_ACCESS_KEY_SECRET`), or use `--secrets-file <path>`.
- **Contributing**: We welcome PRs to enhance the CLI. When changing the public tool catalog, regenerate it with `go run ./internal/openapigen --spec repos/docs/swagger.json` and run `go test ./...`.
