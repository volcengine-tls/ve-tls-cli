# volclog

[English](README.md)

火山引擎 TLS 官方 CLI。`volclog` 是默认的 agent/自动化版，主路径使用契约优先的 `tool / workflow / raw`；`volclog-human` 则在共享相同运行时语义的基础上，额外提供面向人工交互的 shortcut 层。

## 核心能力

- **契约优先主路径** — 通过 `tool describe/exec`、`workflow describe/exec` 与 `raw` 发现并执行 TLS API，而不是先猜 flags 和请求体。
- **双发行版** — `volclog` 面向 agent/CI，`volclog-human` 面向人工交互 shortcut，两者共享相同的 `tool / workflow / raw` 运行时。
- **TLS 覆盖范围广泛** — 覆盖项目、主题、索引、检索分析、告警、机器组、采集规则、ETL、消费组等主要领域。
- **执行路径更安全** — `--dry-run`、结构化 envelope、trace 工件与 file delivery 让预检查、验证和恢复更直接。
- **凭证接入更灵活** — 同时支持长期 AK/SK 和 STS 临时凭证，可通过本地 profile、环境变量或一次性的 `--secrets-file` 注入。

## 快速开始

从 GitHub Release 安装当前候选版本的二进制。

Unix：

```bash
tag=volclog-v1.1.1-rc.1
base_url="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}"
curl -fsSLO "${base_url}/install-binary.sh"
VOLCLOG_BASE_URL="${base_url}" bash install-binary.sh
export PATH="$HOME/.local/bin:$PATH"
```

Windows PowerShell：

```powershell
$tag = "volclog-v1.1.1-rc.1"
$baseUrl = "https://github.com/volcengine-tls/ve-tls-cli/releases/download/$tag"
Invoke-WebRequest -Uri "$baseUrl/install.ps1" -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1 -BaseUrl $baseUrl
$env:Path = "$env:LOCALAPPDATA\Programs\volclog;$env:Path"
```

也可以通过 npm 安装（无需克隆仓库）：

```bash
npm install -g @volcengine-tls/volclog@rc --registry https://registry.npmjs.org/
```

人工交互版和源码安装方式见[快速开始](docs/1-Getting-Started_zh.md)。

配置静态 AK/SK profile 并用 `doctor` 验证：

```bash
volclog configure set \
  --profile default \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com

volclog --profile default doctor
```

执行第一条只读请求：

```bash
volclog --profile default tool exec project.describe-projects
```

## 文档

| # | 文档 | 说明 |
| --- | --- | --- |
| 1 | [快速开始](docs/1-Getting-Started_zh.md) | 安装、配置凭证并运行第一条请求 |
| 2 | [认证](docs/2-Authentication_zh.md) | AK/SK、STS、Console Login、SSO、RAM Role、OIDC 与 ECS Role |
| 3 | [配置](docs/3-Configuration_zh.md) | Profile、region、endpoint 与输出控制 |
| 4 | [使用](docs/4-Usage_zh.md) | `tool`、`workflow`、`raw`、dry-run、过滤与 file delivery |
| 5 | [实战指导](docs/5-Practical-Guide_zh.md) | 端到端任务链路与排障 |
| 6 | [进阶](docs/6-Advanced_zh.md) | 最佳实践、agent skills 与高级工作流 |
| 7 | [Human Shortcuts](docs/7-Human-Shortcuts_zh.md) | 面向人工终端操作的快捷命令与常用场景 |

## 贡献与安全

- **贡献** — 在 GitHub 上提交 issue 或 pull request。提交前请运行相关检查（Go 改动运行 `go test ./...`；npm 打包运行 `npm run test:npm`）。
- [SECURITY.md](https://github.com/volcengine-tls/ve-tls-cli/blob/main/SECURITY.md) — 漏洞报告与支持版本
- [RELEASING.md](https://github.com/volcengine-tls/ve-tls-cli/blob/main/RELEASING.md) — 发布流程
