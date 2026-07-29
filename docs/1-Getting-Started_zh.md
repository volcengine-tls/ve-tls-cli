# 1. 快速开始

[← 返回 README](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇：认证 →](2-Authentication_zh.md)

## 1. 选择版本

`volclog` 是默认版本，面向 agent、CI 和自动化，暴露 `configure`、`doctor`、`skill`、`tool`、`workflow`、`raw`、`login`、`logout` 和 `sso`。

`volclog-human` 额外提供面向人工交互的 shortcut 层。只有在你明确需要 shortcut 时才安装它；否则继续使用 `volclog`。

## 2. 准备工作

- 已具备可用的终端环境
- 一种受支持的认证方式（见[认证](2-Authentication_zh.md)）——第 4 节的静态 AK/SK 示例明确需要 Access Key ID 和 Secret Access Key
- 已明确目标 `region`，例如 `cn-beijing`
- 已明确对应的 TLS endpoint，例如 `https://tls-cn-beijing.volces.com`

`region` 必须显式提供。CLI 不会从 endpoint 或域名反推 region。

## 3. 安装

预构建二进制发布于 Linux、macOS 和 Windows，支持 amd64 与 arm64。

### 3.1 安装二进制

Unix 安装脚本需要 `curl`、`tar`，以及 `sha256sum` 或 `shasum` 之一。Windows 安装脚本需要 PowerShell。

从目标 Release 下载安装脚本，可在任意目录运行：

```bash
tag=volclog-v1.0.5-rc.2
base_url="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}"
curl -fsSLO "${base_url}/install-binary.sh"
VOLCLOG_BASE_URL="${base_url}" bash install-binary.sh
```

Unix 安装脚本默认将二进制写入 `$HOME/.local/bin`。请确保该目录在 `PATH` 中：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

如需安装人工交互版，加上 `--edition human`：

```bash
tag=volclog-v1.0.5-rc.2
base_url="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}"
curl -fsSLO "${base_url}/install-binary.sh"
VOLCLOG_BASE_URL="${base_url}" bash install-binary.sh --edition human
```

在 Windows 上，从目标 Release 下载 PowerShell 安装脚本：

```powershell
$tag = "volclog-v1.0.5-rc.2"
$baseUrl = "https://github.com/volcengine-tls/ve-tls-cli/releases/download/$tag"
Invoke-WebRequest -Uri "$baseUrl/install.ps1" -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1 -BaseUrl $baseUrl
```

Windows 安装脚本默认将二进制写入 `$env:LOCALAPPDATA\Programs\volclog`。请将其加入 `PATH`：

```powershell
$env:PATH = "$env:LOCALAPPDATA\Programs\volclog;$env:PATH"
```

### 3.2 通过 npm 安装

需要 Node.js 18+。通过公开 npm registry 的 `rc` tag 安装当前候选版本：

```bash
npm install -g @volcengine-tls/volclog@rc --registry https://registry.npmjs.org/
```

如需人工交互版：

```bash
npm install -g @volcengine-tls/volclog-human@rc --registry https://registry.npmjs.org/
```

### 3.3 从源码构建

需要 Go 1.22+。在仓库根目录运行，并安装到用户可写目录：

```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/volclog" ./cmd/volclog
export PATH="$HOME/.local/bin:$PATH"
```

如需人工交互版：

```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
mkdir -p "$HOME/.local/bin"
go build -tags=human -o "$HOME/.local/bin/volclog-human" ./cmd/volclog
export PATH="$HOME/.local/bin:$PATH"
```

## 4. 配置静态 AK/SK

本示例使用静态 AK/SK 凭证。其他受支持的认证方式见[认证](2-Authentication_zh.md)。原有静态 AK/SK 配置方式完整保留；省略 `--mode` 即可继续使用：

```bash
volclog configure set \
  --profile default \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

## 5. 用 doctor 验证

本地 doctor 只检查配置和凭证，不发起网络请求：

```bash
volclog --profile default doctor
```

在线 doctor 会对 TLS endpoint 做一次最小化的连通性检查：

```bash
volclog --profile default doctor --online
```

## 6. 第一条只读请求

列出项目，确认 CLI 能正常访问 TLS 并完成签名：

```bash
volclog --profile default tool exec project.describe-projects
```

## 7. 下一步

- [认证](2-Authentication_zh.md) — 全部凭证提供方与登录流程
- [配置](3-Configuration_zh.md) — profile、region、endpoint 与输出控制
- [使用](4-Usage_zh.md) — `tool`、`workflow`、`raw`、dry-run、过滤与 file delivery

---

[← 返回 README](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇：认证 →](2-Authentication_zh.md)
