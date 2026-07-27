# Release Guide

本文件用于规范 `volclog` 的发布流程与质量门禁，避免“能发但不好用/不可复现”的情况。

## 发布前检查（建议作为 PR 合入门禁）

- `go test ./...` 通过
- `gofmt -w` 已执行，且 `gofmt -l .` 无输出
- `go vet ./...` 通过
- README/README_ZH 中的安装命令与示例路径可在仓库根目录直接执行
- `volclog --help`、`volclog <group> -h`、`volclog --version` 输出符合预期
- GitHub Release 产物命名与安装脚本一致：
  - macOS/Linux：`volclog_<os>_<arch>.tar.gz` 与 `volclog_<os>_<arch>.tar.gz.sha256`
  - Windows：`volclog_windows_<arch>.zip` 与 `volclog_windows_<arch>.zip.sha256`
  - 安装脚本：
    - macOS/Linux：`scripts/install-binary.sh`
    - Windows：`scripts/install.ps1`

## 版本号与 Tag 规则

- 正式版使用 Tag：`volclog-vX.Y.Z`（例如 `volclog-v1.0.5`）
- 预发布版使用标准 SemVer Tag：`volclog-vX.Y.Z-rc.N`（例如 `volclog-v1.0.5-rc.1`）
- RC 存在问题时递增 `rc.N`；验证完成后再发布不带预发布后缀的正式版。
- Release workflow 会将 `${GITHUB_REF_NAME}` 注入到二进制版本号中：
  - RC：`volclog --version` 输出 `volclog volclog-v1.0.5-rc.1`
  - 正式版：`volclog --version` 输出 `volclog volclog-v1.0.5`
  
版本建议：
- `0.0.0` 通常用于开发态占位（不建议作为对外发布版本号）
- npm 包版本不带 `volclog-v` 前缀，例如 `1.0.5-rc.1`。
- RC npm 包必须使用 `rc` dist-tag，不能更新 `latest`。

## 发布流程（GitHub Actions）

### 1) 打 Tag 并推送
```bash
git tag volclog-v1.0.5-rc.1
git push origin volclog-v1.0.5-rc.1
```

### 2) 等待工作流完成
- workflow：`.github/workflows/release-volclog.yml`
- 输出：Release assets（不同 OS/Arch 的 tar.gz/zip 与 sha256）
- Release 构建参数：
  - `go build -trimpath -ldflags "-s -w -X volclog/internal/version.Version=${GITHUB_REF_NAME}" -o <out> ./cmd/volclog`
  - 目的：减少二进制中的路径与调试符号，降低 release 产物体积

### 3) 验证安装（建议）

安装最新 release：
```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download" bash scripts/install-binary.sh
~/.local/bin/volclog --version
```

安装指定版本：
```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.5-rc.1" bash scripts/install-binary.sh
~/.local/bin/volclog --version
```

Windows 安装（示例）：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -BaseUrl "https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.5-rc.1"
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
npm view @volcengine-tls/volclog dist-tags
npm view @volcengine-tls/volclog-human dist-tags
npm install -g @volcengine-tls/volclog@rc
npm install -g @volcengine-tls/volclog-human@rc
```

## 常见问题

### Q: 为什么 release assets 不带版本号？
为了简化安装脚本与文档。不同版本通过 `VOLCLOG_BASE_URL` 指向不同 tag 的 download 页面区分。
