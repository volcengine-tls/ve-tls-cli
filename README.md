# volclog（火山引擎 TLS 日志服务 CLI）

`volclog` 用于管理 TLS 日志服务资源（Project/Topic/Index/MetricTopic），并执行日志检索、分析导出、指标查询与 Prometheus 兼容接口调用。它以自动化为优先：默认输出结构化 JSON/JSONL，适合 CI、批处理、平台接入与 Agent 编排。

优先阅读：
- 中文完整指南：[README_CN.md](./README_CN.md)
- 示例文件目录：[examples](./examples/README.md)
- CLI 参数最佳实践：[docs/cli-best-practices.md](./docs/cli-best-practices.md)
- 版本变更记录：[CHANGELOG.md](./CHANGELOG.md)

## Contents

- [架构速览](#架构速览)
- [Agent Integration](#agent-integration)
- [安装](#安装)
- [配置](#配置)
- [快速上手](#快速上手)
- [输入与输出](#输入与输出)
- [常用命令](#常用命令)
- [进一步阅读](#进一步阅读)

## 架构速览

当前仓库的 CLI 执行入口是一个很薄的封装：`cmd/volclog/main.go` 调用 `internal/cli.Run`，整体链路是：
- 全局参数解析：`--profile`、`--output`、`--output-mode`、`--trace-dir`、`--dry-run` 等先于 group 生效
- 运行时上下文：统一加载环境变量、profile、本地项目默认值 `./.volclog/cli.config.json`
- 命令分组：`configure`、`capabilities`、`commands`、`api`、`project`、`topic`、`metric-topic`、`index`、`log`、`assistant`、`doctor`、`completion`
- 输出路径：stdout / file 两种模式；失败时 stderr 输出结构化 JSON
- Agent 能力：`commands` + `capabilities` 负责发现能力，`api --describe` / `--print-request-template` 负责约束与模板，`--dry-run` / `--trace-dir` / `--output-mode file` 负责执行前校验与工件化

## Agent Integration

### 建议工作流

1. 用 `volclog commands` 或 `volclog capabilities` 发现命令空间  
2. 用 `volclog api <group> <action> --describe` 读取机器可消费的参数约束  
3. 用 `--print-request-template=required|full` 生成请求模板  
4. 用 `volclog --dry-run api ...` 做本地校验  
5. 用 `--output-mode file` 承接大结果，用 `--trace-dir` 保留脱敏工件  
6. 失败时解析 stderr JSON，必要时再执行 `doctor` / `doctor --online`

### 能力发现

```bash
volclog commands --group log
volclog capabilities --group log --action SearchLogs
volclog capabilities --group log --action SearchLogs --view full
```

`capabilities` 会输出：
- schema / contract 元信息
- action 的 method / path / summary
- 参数约束、请求体文档与风险提示
- `supports_dry_run`、`output_mode_hint`、`risk_level`、`idempotency`

### API 自解释与模板

```bash
volclog api log SearchLogs --describe
volclog api log SearchLogs --print-request-template=required
volclog api log SearchLogs --print-request-template=full
```

### Dry Run、Envelope 与 Trace

`--dry-run` 当前只支持 `api` group，会返回一个可供 Agent 直接消费的 envelope：

```bash
volclog --dry-run api call --method GET --path /DescribeProjects
volclog --dry-run --output-mode file api log SearchLogs --request file://./examples/search_logs.json
volclog --dry-run --trace-dir ./.volclog/traces api log SearchLogs --request file://./examples/search_logs.json
```

要点：
- `summary.dryRun=true` 表示未真正发出请求
- `summary.tracePath` 指向脱敏 trace 工件
- `artifacts[].path` 指向落盘结果
- API 组在成功时默认返回 envelope，便于工作流系统稳定解析

### 运行时诊断

```bash
volclog doctor
volclog doctor --online
```

`doctor` 会聚合：
- 当前生效 profile
- endpoint / region 来源
- 凭证是否齐备
- timeout 来源
- 在线链路检查与时钟偏移

### Assistant 命令

当前 `assistant` 组已实现的命令是：

```bash
volclog assistant describe-session-answer --topic-id <tid> --question 'What happened?'
```

它会自动处理：
- 通过 `TLS_AI_ASSISTANT_INSTANCE_ID` 复用实例
- 缺少实例时根据 `--account-id` 或 `LOG_SERVICE_ACCOUNT_ID` 查找 / 创建实例
- 创建会话并通过 SSE 聚合 `DescribeSessionAnswer` 的流式结果

## 安装

### 方式 A：GitHub Release 预编译包

macOS/Linux：

```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download" \
bash scripts/install-binary.sh

~/.local/bin/volclog --help
```

固定版本：

```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v0.0.2" \
bash scripts/install-binary.sh
```

Windows：

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

### 方式 B：本地编译安装

Go 1.22+ 环境可直接在仓库根目录安装：

```bash
bash scripts/install-local.sh
~/.local/bin/volclog --help
```

或手工构建：

```bash
go build -o ./volclog ./cmd/volclog
./volclog --help
```

### 方式 C：Docker

```bash
docker build -t volclog:local .
docker run --rm volclog:local --help
```

### 方式 D：源码验证

阅读或修改代码后，建议先执行：

```bash
go test ./...
```

## 配置

### 环境变量

环境变量优先级最高，适合 CI、容器、一次性任务：
- `VOLCENGINE_ACCESS_KEY_ID`
- `VOLCENGINE_ACCESS_KEY_SECRET`
- `VOLCENGINE_TOKEN`
- `VOLCENGINE_ENDPOINT`
- `VOLCENGINE_REGION`

若 endpoint 形如 `https://tls-<region>.volces.com`，region 可从 endpoint 自动推导。

### Profile

本地配置文件默认位于 `~/.volclog/config.json`：

```bash
volclog configure set --profile default --ak "$VOLCENGINE_ACCESS_KEY_ID" --sk "$VOLCENGINE_ACCESS_KEY_SECRET" --endpoint https://tls-cn-beijing.volces.com
volclog configure use default
volclog configure show --profile default
```

多账号 / 多地域推荐：
- 为不同业务环境维护多个 profile
- 用 `--profile <name>` 显式切换
- 复用同一套 AK/SK 时优先用 `--cred-ref`

### 项目级默认值

在工作目录放置 `./.volclog/cli.config.json` 可注入非敏感默认值，例如：

```json
{
  "region": "cn-beijing",
  "endpoint": "https://tls-cn-beijing.volces.com",
  "timeout_seconds": 60,
  "output": "json",
  "output_mode": "stdout",
  "trace_redact": "strict",
  "hints_file": "./capability-hints.json"
}
```

该文件禁止包含凭证字段。

## 快速上手

### 查看命令入口

```bash
volclog --help
volclog project -h
volclog topic -h
volclog metric-topic -h
volclog log -h
volclog api -h
```

### 资源管理

```bash
volclog project list
volclog project create --project-name demo --description test
volclog topic create --project-id <pid> --topic-name demo-topic --ttl 30 --shard-count 2 --auto-split --max-split-shard 10
volclog index create --topic-id <tid> --body file://./examples/index.json
```

### 日志检索与导出

```bash
volclog log search --topic-id <tid> --query "*" --from "2026-03-14 00:00:00" --to "2026-03-14 01:00:00"
volclog --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 10
volclog log export-analysis --topic-id <tid> --query "*|select count(*) as cnt group by __time__ limit 100" --from 1710374400000 --to 1710378000000
```

### 指标主题与 Prom API

```bash
volclog metric-topic list --project-id <pid>
volclog metric-topic prom query --topic-id <metric_tid> --query 'up' --time 1710374400000
volclog metric-topic prom query-range --topic-id <metric_tid> --query 'rate(up[5m])' --start 1710374400000 --end 1710378000000 --step 15
```

## 输入与输出

### file:// 与 stdin

支持把参数值从文件或 stdin 注入：

```bash
volclog index create --topic-id <tid> --body file://./examples/index.json
cat ./examples/create_topic.json | volclog topic create --request -
```

### 完整请求体覆盖

对复杂接口，优先使用 `--request`：

```bash
volclog topic create --request file://./examples/create_topic.json
volclog log search --request file://./examples/search_logs.json
```

### 输出格式

- `--output json`：默认，适合对象型结果
- `--output jsonl`：适合导出、流式处理、管道消费
- `--output-mode file`：stdout 只返回文件路径，实际内容写入 `./.volclog/output/`

### 错误结构

请求失败时 stderr 输出结构化 JSON，包含：
- `errorCode`
- `errorMessage`
- `requestId`
- `statusCode`
- `kind`
- `hint`

退出码约定：
- `0`：成功
- `1`：用法或参数错误
- `2`：请求 / 运行时失败
- `3`：输出 / 解码失败

## 常用命令

```bash
volclog configure -h
volclog capabilities -h
volclog commands -h
volclog api -h
volclog project -h
volclog topic -h
volclog metric-topic -h
volclog index -h
volclog log -h
volclog assistant -h
volclog doctor -h
volclog completion zsh
```

## 进一步阅读

- 中文完整手册：[README_CN.md](./README_CN.md)
- 示例文件目录：[examples](./examples/README.md)
- 版本变更记录：[CHANGELOG.md](./CHANGELOG.md)
