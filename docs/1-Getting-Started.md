# 1. Getting Started

[← Back to README](../README.md) | [中文](1-Getting-Started_zh.md) | [Next: Authentication →](2-Authentication.md)

## 1. Choose an edition

`volclog` is the default edition for agents, CI, and automation. It exposes `configure`, `doctor`, `skill`, `tool`, `workflow`, `raw`, `login`, `logout`, and `sso`.

`volclog-human` adds the human shortcut layer for interactive terminal work. Install it only when you explicitly need shortcuts; otherwise stick with `volclog`.

## 2. Prerequisites

- A terminal environment for your operating system
- One supported authentication method (see [Authentication](2-Authentication.md)) — the static AK/SK example in section 4 specifically requires an Access Key ID and Secret Access Key
- An explicit target `region` such as `cn-beijing`
- The matching TLS endpoint such as `https://tls-cn-beijing.volces.com`

`region` must be provided explicitly. The CLI does not infer region from endpoint or hostname.

## 3. Installation

Prebuilt binaries are published for Linux, macOS, and Windows on amd64 and arm64.

### 3.1 Install the binary

The Unix installer requires `curl`, `tar`, and either `sha256sum` or `shasum`. The Windows installer requires PowerShell.

Download the installer from the target release and run it from any directory:

```bash
tag=volclog-v1.0.5
base_url="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}"
curl -fsSLO "${base_url}/install-binary.sh"
VOLCLOG_BASE_URL="${base_url}" bash install-binary.sh
```

The Unix installer writes the binary to `$HOME/.local/bin` by default. Ensure that directory is on your `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

To install the human edition, add `--edition human`:

```bash
tag=volclog-v1.0.5
base_url="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}"
curl -fsSLO "${base_url}/install-binary.sh"
VOLCLOG_BASE_URL="${base_url}" bash install-binary.sh --edition human
```

On Windows, download the PowerShell installer from the target release:

```powershell
$tag = "volclog-v1.0.5"
$baseUrl = "https://github.com/volcengine-tls/ve-tls-cli/releases/download/$tag"
Invoke-WebRequest -Uri "$baseUrl/install.ps1" -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1 -BaseUrl $baseUrl
```

The Windows installer writes the binary to `$env:LOCALAPPDATA\Programs\volclog` by default. Add it to your `PATH`:

```powershell
$env:PATH = "$env:LOCALAPPDATA\Programs\volclog;$env:PATH"
```

### 3.2 Install via npm

Requires Node.js 18+. Install the stable release from the public npm registry:

```bash
npm install -g @volcengine-tls/volclog@latest --registry https://registry.npmjs.org/
```

For the human edition:

```bash
npm install -g @volcengine-tls/volclog-human@latest --registry https://registry.npmjs.org/
```

### 3.3 Build from source

Requires Go 1.22+. Run from the repository root and install to a user-writable directory:

```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/volclog" ./cmd/volclog
export PATH="$HOME/.local/bin:$PATH"
```

For the human edition:

```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
mkdir -p "$HOME/.local/bin"
go build -tags=human -o "$HOME/.local/bin/volclog-human" ./cmd/volclog
export PATH="$HOME/.local/bin:$PATH"
```

## 4. Configure static AK/SK

This example uses static AK/SK credentials. For other supported authentication methods, see [Authentication](2-Authentication.md). The legacy static AK/SK flow is fully preserved; omit `--mode` to keep it:

```bash
volclog configure set \
  --profile default \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

## 5. Verify with doctor

Local doctor checks configuration and credentials without making network calls:

```bash
volclog --profile default doctor
```

Online doctor performs a minimal live connectivity check against the TLS endpoint:

```bash
volclog --profile default doctor --online
```

## 6. First read-only request

List projects to confirm the CLI can reach TLS and sign requests correctly:

```bash
volclog --profile default tool exec project.describe-projects
```

## 7. Next steps

- [Authentication](2-Authentication.md) — all credential providers and login flows
- [Configuration](3-Configuration.md) — profiles, regions, endpoints, and output controls
- [Usage](4-Usage.md) — `tool`, `workflow`, `raw`, dry-run, filtering, and file delivery

---

[← Back to README](../README.md) | [中文](1-Getting-Started_zh.md) | [Next: Authentication →](2-Authentication.md)
