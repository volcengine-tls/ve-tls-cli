# volclog-cli

[中文版](README_CN.md) | [English](README.md)

The official Volcengine TLS (Tencent Log Service) CLI tool, built for human developers and AI Agents. It covers all core business domains of the Log Service, including projects, topics, indexes, search & analysis, alarms, consumer groups, and data processing, supporting over 20 domains, hundreds of raw APIs, and native Agent Skills.

**Why volclog?**

- **Agent-Native Design** — Out-of-the-box structured Agent Skills compatible with popular AI tool calling specifications. Agents can operate TLS with zero extra setup.
- **Wide Coverage** — Covers 20+ business domains. Whether you need high-frequency shortcuts or full raw OpenAPI access, it's just one command away.
- **AI-Friendly & Optimized** — Every command is tested with real Agents, featuring concise parameters, smart defaults, self-explaining `--describe` capabilities, and standardized Envelope JSON structured output to maximize Agent call success rates.
- **Up and Running in 3 Minutes** — Supports fast installation and interactive configuration. From download to your first API call in just 3 steps.
- **Secure & Controllable** — Comprehensive `--dry-run` pre-execution validation, terminal output sanitization, and environment isolation design.
- **Three-Layer Architecture** — Shortcuts (human & AI friendly) → API Commands (platform-synced, full coverage) → Agent Skills (AI-exclusive), choose the right granularity for your needs.

---

## Features

| Category | Capabilities |
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

Notes:
- Project, Topic, Metric Topic, Index, Log, Host Group, Collector, Assistant, and Doctor are available as first-class CLI groups.
- Alarm, Dashboard, Consumer, Shipper, and ETL/Processor capabilities are primarily exposed through the `api` group and bundled Agent Skills.

---

## Installation & Quick Start

### Prerequisites

Before you start, make sure you have:
- A terminal environment for your operating system
- Your Volcengine AK (Access Key ID) and SK (Secret Access Key)
- Target Region (e.g., `cn-beijing`) and Endpoint (e.g., `https://tls-cn-beijing.volces.com`)

### Quick Start (Human Users)

*Note for AI assistants: If you are an AI Agent helping the user with installation, jump directly to **Quick Start (AI Agent)**.*

#### 1. Install
**Option 1: Download Binary (Recommended)**
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download bash scripts/install-binary.sh
# Windows (PowerShell):
# powershell -File .\scripts\install.ps1
```

**Option 2: Build from Source**
Requires Go v1.22+.
```bash
# Option 2A: Install without cloning (requires Go toolchain)
go install github.com/volcengine-tls/ve-tls-cli/cmd/volclog@latest

# Option 2B: Build from source
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
bash scripts/install-local.sh
# or
go build -o $HOME/.local/bin/volclog ./cmd/volclog
```

Verify the installation:
```bash
volclog --version
volclog doctor
```

#### 2. Configure Credentials
Set up a profile non-interactively:
```bash
volclog configure set --profile default --ak <ak> --sk <sk> --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
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

If you are an AI Agent, you can follow this standardized process to help users configure and verify.

#### Step 1 — Install
```bash
# If in a development environment
bash scripts/install-local.sh
```

#### Step 2 — Discover Capabilities & Constraints
For unfamiliar APIs, Agents should **ALWAYS** discover constraints first via `capabilities` and `--describe` instead of guessing parameters:
```bash
# Check available domains
volclog capabilities --view groups
# Check specific domain interfaces
volclog capabilities --group project --view text
# Check input requirements, templates, and limits
volclog api project CreateProject --describe
```

#### Step 3 — Dry Run & Execute
Validate your request payload via `--dry-run`:
```bash
volclog --dry-run api project CreateProject --request '{"ProjectName":"test", "Region": "cn-beijing"}'
```
Once verified, remove `--dry-run` to execute for real.

#### Step 4 — Data Filtering
Use `--jmes-filter` to extract key data and prevent long lists from blowing up your context window:
```bash
volclog project list --jmes-filter "Projects[].{Id: ProjectId, Name: ProjectName}"
```

---

## Agent Skills

We have prepared standard Agent Skills for popular AI development workflows. You can find them in the `skills/` directory:

| Skill | Description |
| --- | --- |
| **volclog-shared** | Common config & diagnostics (base for all other skills), handles env, auth, and common queries |
| **volclog-api-explorer** | Full discovery and execution capabilities for underlying OpenAPIs |
| **volclog-project** | Project lifecycle management and query planning |
| **volclog-topic** | Topic configuration and constraint validation |
| **volclog-index** | Index analysis and creation, auto-handles full-text and key-value index structures |
| **volclog-log** | Core log search, SQL analytics, and big data export routing |
| **volclog-metric-topic** | Metric stream query and PromQL support |
| **volclog-collector** | Manage and install LogCollector rules and assets |
| **volclog-host-group** | Manage collector host groups and dispatch strategies |
| **volclog-shipper** | Configure data shipping to external storage (TOS/Kafka etc.) |
| **volclog-alarm** | Alarm rule troubleshooting and configuration |
| **volclog-trace** | Generate and manage redacted trace artifacts for troubleshooting |

**Install Skills:**
```bash
volclog skill install --dir skills/
```

---

## Advanced & Best Practices

Beyond basic commands, `volclog` provides numerous advanced capabilities designed specifically for CLI and automation (CI/CD, Agents):

- **Automated Discovery**: Fetch API constraints and complex JSON payload templates instantly using `--describe` and `--print-request-template`.
- **Safe Pre-execution**: Use `--dry-run` to intercept network requests and locally validate JSON syntax and required parameters.
- **Flexible Payload Inputs**: Pass JSON payloads via inline strings, local files (`file://...`), or standard input (`-`) streams.
- **Troubleshooting & Redaction**: Generate sanitized Trace artifacts using `--trace-dir` combined with `--trace-redact strict`.
- **Big Data Streaming**: Process massive log exports and SQL analysis results efficiently with `--output jsonl` and `--output-mode file`.
- **Multi-Account Isolation**: Inject localized environment variables via `--secrets-file ./.env`.

For detailed scenario guides (including specific commands for log search, ETL, alarms, and multi-tenant isolation), we have extracted them into a dedicated document.

👉 **[Highly Recommended: CLI Best Practices & Scenario Guide](docs/cli-best-practices.md)**

---

## Security & Contributing

- **Security**: Avoid hardcoding plaintext AK/SK in command arguments. Use `volclog configure`, inject them via environment variables (`VOLCENGINE_ACCESS_KEY_ID`, `VOLCENGINE_ACCESS_KEY_SECRET`), or use `--secrets-file <path>`.
- **Contributing**: We welcome PRs to enhance the CLI. When adding new OpenAPI commands or skills, be sure to run `scripts/update_capabilities_contract.sh` to update the API contract.
