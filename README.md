# volclog-cli

[中文版](README_CN.md) | [English](README.md)

The official Volcengine TLS (Tencent Log Service) CLI tool, built for both human developers and AI Agents. It covers the main TLS domains, including projects, topics, indexes, search & analysis, alarms, consumer groups, and data processing, with shortcuts, raw API access, and optional skills.

**What volclog provides**

- **Agent integration** — Includes optional skills so common TLS workflows can be handed to LLM-driven tooling or automation.
- **Broad coverage** — Covers 20+ domains; use shortcuts for common tasks and drop to raw OpenAPI when you need exact control.
- **Clearer command constraints** — `--describe`, request templates, `--dry-run`, and structured output make it easier to inspect a call before sending it.
- **Straightforward setup** — Supports binary install, source build, local profiles, environment variables, and `--secrets-file`.
- **Reasonable safety defaults** — Includes `--dry-run`, redacted trace output, and environment-isolation related options.
- **Layered usage** — Shortcut / API / skills are all available, so you can pick the level you need.

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
bash scripts/install-binary.sh
```

**Option 2: Install via npm**
```bash
npm install -g @volcengine-tls/volclog
```

**Option 3: Build from Source**
Requires Go v1.22+.
```bash
git clone https://github.com/volcengine/ve-tls-cli.git
cd ve-tls-cli
make install # or bash scripts/install-local.sh
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

#### Step 2 — Discover Capabilities & Constraints
For unfamiliar APIs, check constraints via `capabilities` and `--describe` before guessing parameters:
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
Once it looks correct, remove `--dry-run` and run it for real.

#### Step 4 — Data Filtering
Use `--jmes-filter` to extract key data and prevent long lists from blowing up your context window:
```bash
volclog project list --jmes-filter "Projects[].{Id: ProjectId, Name: ProjectName}"
```

---

## Agent Skills

The repository includes a set of Agent Skills under `skills/`:

| Skill | Description |
| --- | --- |
| **volclog-shared** | Common config & diagnostics (base for all other skills), handles env, auth, and common queries |
| **volclog-project** | Project lifecycle management and query planning |
| **volclog-topic** | Topic configuration and constraint validation |
| **volclog-index** | Index analysis and creation, auto-handles full-text and key-value index structures |
| **volclog-log** | Core log search, SQL analytics, and big data export routing |
| **volclog-metric-topic** | Metric stream query and PromQL support |
| **volclog-alarm** | Alarm rule troubleshooting and configuration |
| **volclog-api-explorer** | Provides full detection and execution capabilities for underlying OpenAPIs |

**Install Skills:**
```bash
volclog skill install --dir skills/
```

For a one-off install, `npx` works as well:

```bash
npx @volcengine-tls/volclog skill install --dir skills/
```

---

## Advanced & Best Practices

Beyond the basic commands, a few capabilities are especially useful in troubleshooting and automation:

- **Automated Discovery**: Fetch API constraints and complex JSON payload templates instantly using `--describe` and `--print-request-template`.
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
- **Contributing**: We welcome PRs to enhance the CLI. When adding new OpenAPI commands or skills, be sure to run `scripts/update_capabilities_contract.sh` to update the API contract.
