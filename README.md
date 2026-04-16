# volclog-cli

[中文版](README_CN.md) | [English](README.md)

The official Volcengine TLS (Tencent Log Service) CLI tool, built for both human developers and AI Agents. It covers the main TLS domains, including projects, topics, indexes, search & analysis, alarms, consumer groups, and data processing, with shortcuts, tool contracts, raw transport access, and optional skills.

**What volclog provides**

- **Agent integration** — Includes optional skills so common TLS workflows can be handed to LLM-driven tooling or automation.
- **Broad coverage** — Covers 20+ domains; use shortcuts for common tasks and drop to raw transport calls when you need exact method/path control.
- **Clearer command constraints** — `tool/workflow describe`, shortcut request templates, `--dry-run`, and structured output make it easier to inspect a call before sending it.
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

### Quick Start (Human Users)

*If you are using an AI assistant for installation, jump directly to **Quick Start (AI Agent)**.*

#### 1. Install
**Option 1: Download Binary (Recommended)**
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download bash scripts/install-binary.sh
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
# Query project list
volclog project list
# Query log topics
volclog topic list --project-id <your-project-id>
```

---

### Quick Start (AI Agent)

If you are an AI Agent, this is a practical order for helping users configure and verify the CLI.

#### Step 1 — Install
```bash
# If in a development environment
bash scripts/install-local.sh
```

If the npm package is available, you can also install it directly:

```bash
npm install -g @volcengine-tls/volclog
```

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
volclog project list --jmes-filter "Projects[].{Id: ProjectId, Name: ProjectName}"
```

---

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

For detailed scenario guides (including specific commands for log search, ETL, alarms, and multi-tenant isolation), we have extracted them into a dedicated document.

👉 **[CLI Best Practices & Scenario Guide](docs/cli-best-practices.md)**

---

## Security & Contributing

- **Security**: Avoid hardcoding plaintext AK/SK in command arguments. Use `volclog configure`, inject them via environment variables (`VOLCENGINE_ACCESS_KEY_ID`, `VOLCENGINE_ACCESS_KEY_SECRET`), or use `--secrets-file <path>`.
- **Contributing**: We welcome PRs to enhance the CLI. When changing the public tool catalog, regenerate it with `go run ./internal/openapigen --spec repos/docs/swagger.json` and run `go test ./...`.
