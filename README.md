# volclog

[中文](README_ZH.md)

The official Volcengine TLS CLI. `volclog` is the default agent and automation edition built around the contract-first `tool / workflow / raw` path. `volclog-human` adds the human shortcut layer for frequent interactive work while sharing the same runtime semantics.

## Capabilities

- **Contract-first path** — discover and execute TLS APIs through `tool describe/exec`, `workflow describe/exec`, and `raw` instead of guessing flags or request shapes.
- **Dual editions** — `volclog` for agents/CI and `volclog-human` for interactive shortcuts, both with the same `tool / workflow / raw` runtime.
- **Broad TLS coverage** — projects, topics, indexes, search and analysis, alarms, host groups, collectors, ETL, consumer groups, and more.
- **Safer execution** — `--dry-run`, structured envelopes, trace artifacts, and file delivery for preview, validation, and recovery.
- **Flexible credentials** — long-lived AK/SK and STS temporary credentials via local profiles, environment variables, or one-shot `--secrets-file`.

## Quick start

Install the current stable binary from GitHub Release.

On Unix:

```bash
tag=volclog-v1.0.6
base_url="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}"
curl -fsSLO "${base_url}/install-binary.sh"
VOLCLOG_BASE_URL="${base_url}" bash install-binary.sh
export PATH="$HOME/.local/bin:$PATH"
```

On Windows PowerShell:

```powershell
$tag = "volclog-v1.0.6"
$baseUrl = "https://github.com/volcengine-tls/ve-tls-cli/releases/download/$tag"
Invoke-WebRequest -Uri "$baseUrl/install.ps1" -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1 -BaseUrl $baseUrl
$env:Path = "$env:LOCALAPPDATA\Programs\volclog;$env:Path"
```

Alternatively, install via npm (no repository checkout required):

```bash
npm install -g @volcengine-tls/volclog@latest --registry https://registry.npmjs.org/
```

For the human edition and source installation, see [Getting Started](docs/1-Getting-Started.md).

Configure a static AK/SK profile and verify with `doctor`:

```bash
volclog configure set \
  --profile default \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com

volclog --profile default doctor
```

Run your first read-only request:

```bash
volclog --profile default tool exec project.describe-projects
```

## Documentation

| # | Document | Description |
| --- | --- | --- |
| 1 | [Getting Started](docs/1-Getting-Started.md) | Install, configure credentials, and run the first request |
| 2 | [Authentication](docs/2-Authentication.md) | AK/SK, STS, Console Login, SSO, RAM Role, OIDC, and ECS Role |
| 3 | [Configuration](docs/3-Configuration.md) | Profiles, regions, endpoints, and output controls |
| 4 | [Usage](docs/4-Usage.md) | `tool`, `workflow`, `raw`, dry-run, filtering, and file delivery |
| 5 | [Practical Guide](docs/5-Practical-Guide.md) | End-to-end task walkthroughs and troubleshooting |
| 6 | [Advanced](docs/6-Advanced.md) | Best practices, agent skills, and advanced workflows |
| 7 | [Human Shortcuts](docs/7-Human-Shortcuts.md) | Interactive shortcut commands and common terminal workflows |

## Contributing & Security

- **Contributing** — open an issue or pull request on GitHub. Before submitting, run the relevant checks (`go test ./...` for Go changes; `npm run test:npm` for npm packaging).
- [SECURITY.md](https://github.com/volcengine-tls/ve-tls-cli/blob/main/SECURITY.md) — vulnerability reporting and supported versions
- [RELEASING.md](https://github.com/volcengine-tls/ve-tls-cli/blob/main/RELEASING.md) — release process
