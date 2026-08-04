# Release Guide

本文件用于规范 `volclog` 的发布流程与质量门禁，避免“能发但不好用/不可复现”的情况。

## 发布前检查（建议作为 PR 合入门禁）

- `go test ./...` 通过
- `gofmt -w` 已执行，且 `gofmt -l .` 无输出
- `go vet ./...` 通过
- README/README_ZH 与快速开始文档中的二进制安装命令可在空目录直接执行
- `volclog --help`、`volclog <group> -h`、`volclog --version` 输出符合预期
- GitHub Release 产物命名与安装脚本一致：
  - macOS/Linux：`volclog_<os>_<arch>.tar.gz` 与 `volclog_<os>_<arch>.tar.gz.sha256`
  - Windows：`volclog_windows_<arch>.zip` 与 `volclog_windows_<arch>.zip.sha256`
  - 安装脚本：
    - macOS/Linux：`install-binary.sh`
    - Windows：`install.ps1`

安装脚本由 tag 触发的 release workflow 自动上传。历史 Release 仅在仍需支持对应固定版本的脚本安装时按需回填。

## 版本号与 Tag 规则

- 正式版使用 Tag：`volclog-vX.Y.Z`（例如 `volclog-v1.0.5`）
- 预发布版使用标准 SemVer Tag：`volclog-vX.Y.Z-rc.N`（例如 `volclog-v1.0.5-rc.3`）
- RC 存在问题时递增 `rc.N`；验证完成后再发布不带预发布后缀的正式版。
- Release workflow 会将 `${GITHUB_REF_NAME}` 注入到二进制版本号中：
  - RC：`volclog --version` 输出 `volclog volclog-v1.0.5-rc.3`
  - 正式版：`volclog --version` 输出 `volclog volclog-v1.0.5`
  
版本建议：
- `0.0.0` 通常用于开发态占位（不建议作为对外发布版本号）
- npm 包版本不带 `volclog-v` 前缀，例如 `1.0.5-rc.3`。
- RC npm 包必须使用 `rc` dist-tag，不能更新 `latest`。

## 发布流程（GitHub Actions）

### 1) 打 Tag 并推送
```bash
git tag volclog-v1.0.5-rc.3
git push origin volclog-v1.0.5-rc.3
```

### 2) 等待工作流完成
- workflow：`.github/workflows/release-volclog.yml`
- 输出：Release assets（不同 OS/Arch 的 tar.gz/zip、sha256 与安装脚本）
- Release 构建参数：
  - `go build -trimpath -ldflags "-s -w -X github.com/volcengine-tls/ve-tls-cli/internal/version.Version=${GITHUB_REF_NAME}" -o <out> ./cmd/volclog`
  - 目的：减少二进制中的路径与调试符号，降低 release 产物体积

### 3) 验证安装（建议）

以下命令适用于已经包含安装脚本资产的 Release。安装指定 RC：
```bash
tag="volclog-v1.0.5-rc.3"
base_url="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}"
curl -fsSLO "${base_url}/install-binary.sh"
VOLCLOG_BASE_URL="${base_url}" bash install-binary.sh
~/.local/bin/volclog --version
```

正式 `v1.0.5` 发布、`latest` 已包含安装脚本资产后：
```bash
curl -fsSLO https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download/install-binary.sh
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download" bash install-binary.sh
~/.local/bin/volclog --version
```

Windows 安装（示例）：
```powershell
$tag = "volclog-v1.0.5-rc.3"
$baseUrl = "https://github.com/volcengine-tls/ve-tls-cli/releases/download/$tag"
Invoke-WebRequest -Uri "$baseUrl/install.ps1" -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1 -BaseUrl $baseUrl
```

## npm RC 发布

必须先等待同版本 GitHub Release 产物上传完成，因为 npm 安装脚本会按照 npm
包版本下载对应的 `volclog-vX.Y.Z-rc.N` 二进制。

发布两个 RC 包：

```bash
npm run publish:npm:rc
npm run publish:npm:human:rc
```

两个包的 `publishConfig.tag` 均固定为 `rc`，registry 固定为官方
`https://registry.npmjs.org/`；上面的脚本也显式携带相同参数，避免 RC
意外更新 `latest` 或被错误发布到本机配置的镜像站。`prepublishOnly`
还会拒绝任何没有显式传入 `--tag rc` 的 RC 发布命令。

验证：

```bash
npm view @volcengine-tls/volclog dist-tags --registry https://registry.npmjs.org/
npm view @volcengine-tls/volclog-human dist-tags --registry https://registry.npmjs.org/
npm install -g @volcengine-tls/volclog@rc --registry https://registry.npmjs.org/
npm install -g @volcengine-tls/volclog-human@rc --registry https://registry.npmjs.org/
```

## 常见问题

### Q: 为什么 release assets 不带版本号？
为了简化安装脚本与文档。不同版本通过 `VOLCLOG_BASE_URL` 指向不同 tag 的 download 页面区分。
