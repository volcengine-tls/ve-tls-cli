# Release Guide

本文件用于规范 `volclog` 的发布流程与质量门禁，避免“能发但不好用/不可复现”的情况。

## 发布前检查（建议作为 PR 合入门禁）

- `go test ./...` 通过
- `gofmt -w` 已执行，且 `gofmt -l .` 无输出
- `go vet ./...` 通过
- README/README_CN 中的二进制安装命令可在空目录直接执行
- `volclog --help`、`volclog <group> -h`、`volclog --version` 输出符合预期
- GitHub Release 产物命名与安装脚本一致：
  - macOS/Linux：`volclog_<os>_<arch>.tar.gz` 与 `volclog_<os>_<arch>.tar.gz.sha256`
  - Windows：`volclog_windows_<arch>.zip` 与 `volclog_windows_<arch>.zip.sha256`
  - 安装脚本：
    - macOS/Linux：`install-binary.sh`
    - Windows：`install.ps1`

安装脚本由 tag 触发的 release workflow 自动上传，后续版本不需要手工添加。历史 Release 仅在仍需支持对应固定版本的脚本安装时按需回填，不要求全量补齐。

## 版本号与 Tag 规则

- 使用 Tag 触发发布：`volclog-vX.Y.Z`（例如 `volclog-v1.0.0`）
- Release workflow 会将 `${GITHUB_REF_NAME}` 注入到二进制版本号中：
  - `volclog --version` 输出：`volclog volclog-v1.0.0`
  
版本建议：
- `0.0.0` 通常用于开发态占位（不建议作为对外发布版本号）
- 首个稳定版本建议使用 `volclog-v1.0.0`

## 发布流程（GitHub Actions）

### 1) 打 Tag 并推送
```bash
git tag volclog-v1.0.0
git push origin volclog-v1.0.0
```

### 2) 等待工作流完成
- workflow：`.github/workflows/release-volclog.yml`
- 输出：Release assets（不同 OS/Arch 的 tar.gz/zip 与 sha256）
- Release 构建参数：
  - `go build -trimpath -ldflags "-s -w -X volclog/internal/version.Version=${GITHUB_REF_NAME}" -o <out> ./cmd/volclog`
  - 目的：减少二进制中的路径与调试符号，降低 release 产物体积

### 3) 验证安装（建议）

以下命令用于验证已经包含安装脚本资产的 Release；尚未回填脚本的历史版本不能直接使用对应的 `install-binary.sh` / `install.ps1` Release URL。

安装最新 release：
```bash
curl -fsSLO https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download/install-binary.sh
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download" bash install-binary.sh
~/.local/bin/volclog --version
```

安装指定版本：
```bash
tag="volclog-vX.Y.Z"
curl -fsSLO "https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}/install-binary.sh"
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/download/${tag}" bash install-binary.sh
~/.local/bin/volclog --version
```

Windows 安装（示例）：
```powershell
Invoke-WebRequest -Uri "https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download/install.ps1" -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

## 常见问题

### Q: 为什么 release assets 不带版本号？
为了简化安装脚本与文档。不同版本通过 `VOLCLOG_BASE_URL` 指向不同 tag 的 download 页面区分。
