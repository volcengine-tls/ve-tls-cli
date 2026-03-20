# volclog（火山引擎 TLS 日志服务 CLI）

`volclog` 用于通过命令行管理 TLS 日志服务资源（Project/Topic/Index/MetricTopic）并进行日志/指标查询与导出。面向自动化场景（CI、批处理、Agent）默认输出结构化 JSON/JSONL，便于二次处理。

更详细的逐命令参数手册与示例文件见：
- 中文完整指南：[README_CN.md](./README_CN.md)
- 示例文件目录：[examples](./examples/README.md)
- CLI 参数最佳实践：[docs/cli-best-practices.md](./docs/cli-best-practices.md)

## 目录
- 安装
- 配置
- 快速上手
- 输入与输出约定
- 自动化与诊断
- 常用命令
- 进一步阅读

## 安装

### 方式 A：GitHub Release 预编译包（无需 Go）

macOS/Linux 依赖：`bash`、`curl`、`tar`、（可选）`shasum` 或 `sha256sum`

安装最新 release：
```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download" \
bash scripts/install-binary.sh

~/.local/bin/volclog --help
```

固定安装某个版本（推荐用于生产/CI）：
```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v0.0.2" \
bash scripts/install-binary.sh
```

Windows 依赖：PowerShell 5+（或 PowerShell 7+）

安装最新 release：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

固定安装某个版本：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -BaseUrl "https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v0.0.2"
```

### 方式 B：本地编译安装（需要 Go）

依赖：`bash`、`go 1.22+`

一键安装到 `~/.local/bin/volclog`：
```bash
bash scripts/install-local.sh
~/.local/bin/volclog --help
```

### 方式 C：Docker（无需本机 Go）

依赖：`docker`

```bash
docker build -t volclog:local .
docker run --rm volclog:local --help
```

## 配置

### 环境变量（优先级最高，适合 CI/容器）
- `VOLCENGINE_ACCESS_KEY_ID`
- `VOLCENGINE_ACCESS_KEY_SECRET`
- `VOLCENGINE_TOKEN`（可选）
- `VOLCENGINE_ENDPOINT`（例如 `https://tls-cn-beijing.volces.com`）
- `VOLCENGINE_REGION`（可选；当 endpoint 形如 `tls-<region>.volces.com` 时可从 endpoint 推导）

### 本地 Profile（默认 `~/.volclog/config.json`）
```bash
volclog configure set --profile default --ak "$VOLCENGINE_ACCESS_KEY_ID" --sk "$VOLCENGINE_ACCESS_KEY_SECRET" --endpoint https://tls-cn-beijing.volces.com
volclog configure show --profile default
volclog configure use default
```

多账号/多 region 场景建议使用多个 profile，并在单次命令上使用 `--profile <name>` 选择；跨多个 region/endpoint 复用同一 AK/SK 时，推荐使用 `--cred-ref`（详见 README_CN.md 的“多租户 / 多账号 / 多 Region 组合配置”）。

## 安全建议

- 建议通过环境变量或 CI Secret 注入 AK/SK，避免在命令行参数或脚本中明文写入。
- 如使用本地 Profile，请妥善保护 `~/.volclog/config.json`（包含敏感凭证），并避免将其纳入版本控制。
- 建议在共享终端/日志环境中谨慎开启调试输出（如后续版本增加更多调试信息）。
- 请求失败时可使用 stderr 中的 `requestId` 协助排障，避免在 issue/工单中粘贴完整密钥。

## 快速上手

查看命令入口：
```bash
volclog --help
volclog project -h
volclog topic -h
volclog metric-topic -h
volclog log -h
```

资源管理（建议 ID 优先）：
```bash
volclog project list
volclog project create --project-name demo --description test
volclog project get --project-id <pid>

volclog topic list --project-id <pid>
volclog topic create --project-id <pid> --topic-name demo-topic --ttl 30 --shard-count 2 --auto-split --max-split-shard 10
volclog topic get --topic-id <tid>

volclog index create --topic-id <tid> --body file://./index.json
```

日志查询与导出：
```bash
volclog log search --topic-id <tid> --query "*" --from "2026-03-14 00:00:00" --to "2026-03-14 01:00:00"
volclog --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 10
```

指标主题与 Prom API：
```bash
volclog metric-topic list --project-id <pid>
volclog metric-topic prom -h
volclog metric-topic prom query --topic-id <metric_tid> --query 'up' --time 1710374400000
volclog metric-topic prom query-range --topic-id <metric_tid> --query 'rate(up[5m])' --start 1710374400000 --end 1710378000000 --step 15
```

## 输入与输出约定

### file:// 文件输入
参数值以 `file://` 开头时会从文件读取内容。例如：
```bash
volclog index create --topic-id <tid> --body file://./index.json
```

### stdin 输入（-）
部分参数支持使用 `-` 从 stdin 读取内容（便于与管道组合）。例如：
```bash
cat ./examples/create_topic.json | volclog topic create --request -
```

### --request file://... 覆盖完整请求体
部分命令支持 `--request file://...` 直接传入 JSON 请求体，以覆盖更多服务端参数（以 swagger 为准）：
```bash
volclog topic create --request file://./examples/create_topic.json
volclog log search --request file://./examples/search_logs.json
```

### 输出格式
- `--output json`：默认，适合资源管理与查询
- `--output jsonl`：每行一条 JSON，适合日志导出/流式处理

### 错误输出
请求失败时在 stderr 输出结构化 JSON（包含 `errorCode/errorMessage/requestId/statusCode/kind/hint`），便于自动化系统识别与告警。

## 自动化与诊断

### doctor
快速检查当前环境与配置是否满足请求条件：
```bash
volclog doctor
```

### trace（工件化复盘）
为任意命令生成 trace 工件（脱敏 JSONL），并在输出中返回 trace 路径：
```bash
volclog --trace-dir ./.volclog/traces log search --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000
```

### 输出落盘（stdout/file）
将命令输出写入文件并在 stdout 返回输出路径（适合 CI/Agent 降低 stdout 体积）：
```bash
volclog --output-mode file log search --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000
```

### 项目级默认配置
在仓库根目录放置 `./.volclog/cli.config.json` 可为该项目提供非敏感默认值（如 region/endpoint/timeout/output/output_mode/trace_redact）。

### secrets file（dotenv）
通过 `--secrets-file` 从 dotenv 文件加载环境变量（不会覆盖当前已存在的环境变量）：
```bash
volclog --secrets-file ./.env project list
```

## 常用命令

### OpenAPI 兜底调用
当某个接口 CLI 尚未封装时，可用 `api call` 直接调用：
```bash
volclog api call --method GET --path /DescribeProject --query ProjectId=<pid>
volclog api call --method POST --path /SearchLogs --body file://./examples/search_logs.json
```

## 进一步阅读
- 中文逐命令参数手册：[README_CN.md](./README_CN.md)
- 示例文件（请求体、PromQL、match 等）：[examples](./examples/README.md)
- 版本变更记录：[CHANGELOG.md](./CHANGELOG.md)
