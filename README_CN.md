# volclog（火山引擎 TLS 日志服务 CLI）用户指南

本指南面向使用命令行与日志服务的用户，尽量把“要做什么、怎么做、为什么这样做”讲清楚。你可以把 `volclog` 理解成：用命令行完成“创建日志项目/主题/索引、查询与导出日志、管理指标主题与 Prom 查询接口”的工具；它的当前实现同时面向 CI、批处理和 Agent 编排，默认输出结构化 JSON/JSONL。

> 如果你打算把日志服务能力接入 skills/openclaw/agent：建议默认使用 `--output json`（或导出用 `--output jsonl`），并在失败时读取 stderr 的 JSON 错误结构；详细对接流程见本文的 **3. Agent 对接详解**。

自动化/Agent 友好能力建议先看 **3. Agent 对接详解** 和 **6.6 自动化与诊断（Agent 推荐）**。

## 架构速览

当前仓库的 CLI 核心执行链路如下：

- 可执行入口在 `cmd/volclog/main.go`，它是对 `internal/cli.Run` 的薄封装
- 入口核心在 `internal/cli.Run`，先统一解析全局参数，再分发到各个 group
- 全局参数包括 `--profile`、`--output`、`--output-mode`、`--trace-dir`、`--secrets-file`、`--dry-run`
- 上下文层会统一加载环境变量、用户级 profile 与项目级默认值 `./.volclog/cli.config.json`
- 命令分组包括 `configure`、`capabilities`、`api`、`project`、`topic`、`metric-topic`、`index`、`log`、`assistant`、`doctor`、`completion`
- `api` 组支持 `--describe`、`--print-request-template`、`--dry-run` 等自解释能力，适合 Agent 动态拼装请求
- 输出链路同时支持 stdout、落盘工件、trace 工件与结构化 stderr，便于自动化系统稳定消费

***

## 目录

- [架构速览](#架构速览)
- [1. 你需要准备什么](#1-你需要准备什么)
  - [1.1 访问凭证（AK/SK）](#11-访问凭证aksk)
  - [1.2 Region（地域）](#12-region地域)
  - [1.3 Endpoint（服务地址）](#13-endpoint服务地址)
- [2. 安装](#2-安装)
- [3. Agent 对接详解](#3-agent-对接详解)
- [4. 快速上手（示例流程）](#4-快速上手示例流程)
- [5. 配置说明（环境变量 / Profile）](#5-配置说明环境变量--profile)
- [6. 输入与输出规则（非常重要）](#6-输入与输出规则非常重要)
  - [6.1 输入：普通参数 vs file:// 文件输入](#61-输入普通参数-vs-file-文件输入)
  - [6.2 时间格式（from/to/start/end/time）](#62-时间格式fromtostartendtime)
  - [6.3 输出：JSON / JSONL](#63-输出json--jsonl)
  - [6.4 输出过滤：--jmes-filter（JMESPath）](#64-输出过滤--jmes-filterjmespath)
  - [6.5 错误结构与退出码（给自动化集成用）](#65-错误结构与退出码给自动化集成用)
  - [6.6 自动化与诊断（Agent 推荐）](#66-自动化与诊断agent-推荐)
- [7. 命令参考（按功能分组）](#7-命令参考按功能分组)
- [8. 逐命令参数手册（参数说明 + body 示例）](#8-逐命令参数手册参数说明--body-示例)
- [9. 常见场景与示例](#9-常见场景与示例)
- [10. 故障排查与 FAQ](#10-故障排查与-faq)

***

## 1. 你需要准备什么

### 1.1 访问凭证（AK/SK）

你需要火山引擎访问密钥：

- Access Key ID（AK）
- Secret Access Key（SK）
- 可选：Security Token（临时凭证 / STS 场景）

### 1.2 Region（地域）

Region 是你要访问的地域，例如：

- `cn-beijing`
- `cn-shanghai`

### 1.3 Endpoint（服务地址）

Endpoint 是 TLS 日志服务域名，通常形如：

- `https://tls-cn-beijing.volces.com`

如果你只配置了 `endpoint`（并且 endpoint 命名符合 `https://tls-<region>.volces.com`），`volclog` 会尝试从 endpoint 推导 region：

- `https://tls-cn-beijing.volces.com` → `cn-beijing`

***

## 2. 安装

`volclog` 的当前代码实现基于 Go。仓库内测试与源码阅读要求 Go 版本：**Go 1.22+**。

### 2.0 安装方式对比（推荐先看这里）

| 方式 | 适合谁 | 依赖 | 是否需要 Go | 备注 |
|---|---|---|---:|---|
| GitHub Release 预编译包（macOS/Linux） | 直接使用 CLI 的用户、CI、服务器 | `bash`、`curl`、`tar`、（可选）`shasum` 或 `sha256sum` | 否 | 最稳妥，推荐 |
| GitHub Release 预编译包（Windows） | Windows 环境 | PowerShell 5+（或 PowerShell 7+） | 否 | 最稳妥，推荐 |
| 本地编译安装 | 已拿到源码、机器上有 Go 的开发者 | `bash`、`go(>=1.22)` | 是 | 可直接用脚本或 `go build ./cmd/volclog` |
| Docker 运行 | 需要隔离运行环境 / 不安装 Go | `docker` | 否 | 适合临时运行与验证 |
| 源码验证 | 仓库维护者、二次开发者 | `go(>=1.22)` | 是 | 推荐配合 `go test ./...` |

### 2.1 直接安装可执行文件（推荐）

#### 2.1.1 方案 A：下载预编译二进制

前提：我们需要提供对应系统的 release 包（例如内部制品库或 GitHub Releases）。

macOS/Linux 安装脚本（需要 curl + tar）：

依赖：
- `bash`
- `curl`
- `tar`
- （可选）`shasum` 或 `sha256sum`（若 release 同目录提供 `.sha256` 文件则会校验）

```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download" bash scripts/install-binary.sh
~/.local/bin/volclog --help
```

说明：

- 脚本会自动识别 OS/Arch
- 下载文件名固定为 `volclog_${os}_${arch}.tar.gz`（通过 `VOLCLOG_BASE_URL` 指向不同 release）
- 若同路径存在 `.sha256` 文件会自动校验（格式为：`<sha256>  <filename>`）
- 默认安装到 `~/.local/bin/volclog`（可用 `PREFIX=/usr/local` 改路径）

如需固定安装某个版本（建议用于生产/CI），可使用 tag 作为版本：
```bash
VOLCLOG_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v0.0.2" bash scripts/install-binary.sh
```

Windows 安装脚本（PowerShell）：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

固定安装某个版本：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -BaseUrl "https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v0.0.2"
```

### 2.2 本地编译安装

如果你已经拿到源码并具备 Go 1.22+ 环境，可直接安装到 `~/.local/bin/volclog`：

```bash
bash scripts/install-local.sh
~/.local/bin/volclog --help
```

如果希望直接安装到 GOPATH/GOBIN，推荐使用：

```bash
go install github.com/volcengine-tls/ve-tls-cli/cmd/volclog@latest
volclog --help
```

也可以手工构建：

```bash
go build -o ./volclog ./cmd/volclog
./volclog --help
```

### 2.3 Docker 运行

```bash
docker build -t volclog:local .
docker run --rm volclog:local --help
```

如需传入环境变量（例如 AK/SK/Region/Endpoint）：

```bash
docker run --rm \
  -e VOLCENGINE_ACCESS_KEY_ID \
  -e VOLCENGINE_ACCESS_KEY_SECRET \
  -e VOLCENGINE_TOKEN \
  -e VOLCENGINE_REGION \
  -e VOLCENGINE_ENDPOINT \
  volclog:local project list
```

### 2.4 源码验证

阅读、修改或二次集成代码时，建议先跑：

```bash
go test ./...
```

如果你的目标是“最稳定地直接使用 CLI”，仍推荐优先安装 release 包；如果你的目标是阅读实现或做二次集成，则可以把 `cmd/volclog` 视为薄入口、把 `internal/cli` 视为核心实现。

***

## 3. Agent 对接详解

本章按“发现能力 → 生成约束 → 本地校验 → 执行落盘 → 失败诊断”的顺序说明，尽量贴近当前代码实现。

### 3.1 先发现，不要硬编码

先用 `capabilities` 探测当前 CLI 暴露了哪些能力：

```bash
volclog capabilities --view text
volclog capabilities --view text --group log
volclog capabilities --group log --action SearchLogs
volclog capabilities --group log --action SearchLogs --view full
```

建议：

- `capabilities` 适合给 Agent 提供机器可消费的契约
- `capabilities --view text` 适合快速查看人类可读的命令清单
- `capabilities --view full` 会带回更完整的参数约束、请求体说明与提示信息

### 3.2 用 api 自解释能力生成请求约束

对于服务端 OpenAPI，优先使用 `api <group> <action>` 这一层，而不是直接手写 `api call`：

```bash
volclog api log SearchLogs --describe
volclog api log SearchLogs --print-request-template=required
volclog api log SearchLogs --print-request-template=full
```

这三类输出分别适合：

- `--describe`：读取 method/path/参数约束/请求体摘要
- `--print-request-template=required`：生成最小可执行模板
- `--print-request-template=full`：生成完整字段模板，便于补全复杂 body

### 3.3 Dry Run 只在 api 组可用

当前 `--dry-run` 仅支持 `api` group，用于本地校验 profile、region、endpoint 与 body JSON 是否有效：

```bash
volclog --dry-run api call --method GET --path /DescribeProjects
volclog --dry-run api log SearchLogs --request file://./examples/search_logs.json
```

返回结果是稳定的 envelope 风格结构，适合 Agent 直接解析：

- `status`：成功与否
- `summary.dryRun`：是否为 dry-run
- `summary.tracePath`：启用 trace 后的工件路径
- `artifacts[]`：在 `--output-mode file` 下的输出文件信息

### 3.4 大结果集优先走工件

对日志导出、指标查询、SearchLogs 大结果等场景，建议默认使用：

```bash
volclog --output-mode file log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000
volclog --output-mode file metric-topic search --topic-id <tid> --query 'up' --from 1710374400000 --to 1710378000000
```

实践建议：

- 普通 group：stdout 返回文件路径，后续步骤再读取该文件
- `api` group：读取 envelope 内的 `artifacts[].path`
- 需要稳定提取字段时，可先用 `--jmes-filter` 缩小输出

### 3.5 失败时读取 stderr JSON，而不是只看文本

失败时应优先解析 stderr JSON，常用字段包括：

- `errorCode`
- `errorMessage`
- `requestId`
- `statusCode`
- `kind`
- `hint`

退出码建议：

- `1`：参数错误或输入不合法，可直接修正命令
- `2`：请求或运行时失败，结合 `requestId`、`doctor`、`trace` 继续排查
- `3`：输出过滤、解码或写文件失败，优先检查 `--jmes-filter` 与目标文件路径

### 3.6 诊断与复盘建议组合

推荐的最短闭环：

```bash
volclog doctor
volclog doctor --online
volclog --trace-dir ./.volclog/traces --output-mode file project list
```

解释：

- `doctor`：先检查 profile / endpoint / region / 凭证是否齐备
- `doctor --online`：把网络、TLS、最小 API 探测拆成多个检查项
- `--trace-dir`：保留脱敏工件，便于复盘请求元信息与响应元信息

### 3.7 assistant 命令的当前实现边界

当前 `assistant` 组已落地的命令只有一个：

```bash
volclog assistant describe-session-answer --topic-id <tid> --question 'What happened?'
```

它会按如下流程工作：

- 优先读取 `TLS_AI_ASSISTANT_INSTANCE_ID`
- 若未提供实例，则用 `--account-id` 或 `LOG_SERVICE_ACCOUNT_ID` 查找 / 创建实例
- 创建会话后，通过 SSE 流式消费 `DescribeSessionAnswer`
- 最终返回聚合后的 `Answer` 文本，以及 `InstanceId`、`SessionId`、`TopicId`

如果你要做 Agent 编排，建议把 `assistant` 视为“面向 TLS Assistant 服务的一个专用命令组”，而把通用自动化入口放在 `capabilities` / `api` / `doctor` 这一套能力上。

***

## 4. 快速上手（示例流程）

下面给出一套示例流程：配置 → 创建项目 → 创建主题 → 创建索引 → 查询日志；并包含指标主题与 Prom 查询示例。以下示例默认你已经安装好了 `volclog` 可执行文件。

> 提示：可在任意命令后追加 `-h` 查看该组用法，例如：`volclog metric-topic -h`、`volclog metric-topic prom -h`。

### 4.1 方式 A：用环境变量（适合 CI / 容器）

```bash
export VOLCENGINE_ACCESS_KEY_ID="你的AK"
export VOLCENGINE_ACCESS_KEY_SECRET="你的SK"
export VOLCENGINE_ENDPOINT="https://tls-cn-beijing.volces.com"
# 可选：export VOLCENGINE_REGION="cn-beijing"
# 可选：export VOLCENGINE_TOKEN="你的STS Token"

volclog project list
```

### 4.2 方式 B：写入本地 Profile（适合个人电脑）

主路径（推荐）：

```bash
volclog configure set --profile demo-bj --cred-ref demo-root --ak "你的AK" --sk "你的SK" --endpoint https://tls-cn-beijing.volces.com
volclog configure set --profile demo-sg --cred-ref demo-root --endpoint https://tls-ap-singapore-1.volces.com
volclog configure use demo-bj
```

```bash
volclog configure set --profile default --ak "你的AK" --sk "你的SK" --endpoint https://tls-cn-beijing.volces.com
volclog configure use default
volclog configure show --profile default
```

### 4.3 创建一个日志项目（Project）

```bash
volclog project create --project-name demo-project --description "demo"
```

成功后会返回 `ProjectId`。后续操作都推荐使用 ID（更稳定）。

### 4.4 创建一个日志主题（Topic）

```bash
volclog topic create --project-id <ProjectId> --topic-name demo-topic --ttl 30 --shard-count 2 --auto-split --max-split-shard 10
```

### 4.5 创建/修改索引（Index）

索引决定你能否按字段检索/分析。通常需要先准备一个 JSON 文件，例如 `index.json`。

```bash
volclog index create --topic-id <TopicId> --body file://./index.json
# 如果已存在索引，改用 modify
volclog index modify --topic-id <TopicId> --body file://./index.json
```

### 4.6 查询日志（SearchLogs）

```bash
volclog log search --topic-id <TopicId> --query "*" --from "2026-03-14 00:00:00" --to "2026-03-14 01:00:00"
```

说明：
- `log search` 返回的是完整响应对象（包含 `Logs/Context/ListOver/Count/AnalysisResult` 等）；如果你只是想把日志逐行输出（JSONL），可用：

```bash
volclog --output json log search ... | jq -c '.Logs[]'
```

- 如果你跑的是 SQL/聚合类查询，结果可能主要在 `AnalysisResult` 而不是 `Logs`，可用：

```bash
volclog --output json log search ... | jq -c '.AnalysisResult.Data[]'
```

### 4.7 导出日志（JSONL：每条日志一行）

```bash
volclog --output jsonl log export --topic-id <TopicId> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 10
```

### 4.7.1 导出 SQL/分析结果（JSONL：每行一个带列名对象）

```bash
volclog log export-analysis --topic-id <TopicId> --query "*|select count(*) as cnt group by __time__ limit 100" --from 1710374400000 --to 1710378000000
```

### 4.8 指标主题（MetricTopic）与 Prom 查询

```bash
volclog metric-topic list --project-id <ProjectId>
volclog metric-topic prom query --topic-id <MetricTopicId> --query 'up' --time 1710374400000
volclog metric-topic prom query-range --topic-id <MetricTopicId> --query 'rate(up[5m])' --start 1710374400000 --end 1710378000000 --step 15
```

***

## 5. 配置说明（环境变量 / Profile）

### 5.1 环境变量（优先级最高）

当以下环境变量都具备时，`volclog` 会优先使用它们（覆盖 profile）：

- `VOLCENGINE_ACCESS_KEY_ID`
- `VOLCENGINE_ACCESS_KEY_SECRET`
- `VOLCENGINE_TOKEN`（可选）
- `VOLCENGINE_REGION`
- `VOLCENGINE_ENDPOINT`

说明：
- 推荐显式提供 `VOLCENGINE_ENDPOINT`，并可选提供 `VOLCENGINE_REGION`
- 当 `VOLCENGINE_ENDPOINT` 形如 `https://tls-<region>.volces.com` 且未提供 `VOLCENGINE_REGION` 时，会尝试从 endpoint 推导 region；推导失败则仍要求显式提供 region。

适用于：

- CI/CD
- 容器运行
- skills/openclaw runtime 注入密钥

### 5.2 本地 Profile 配置文件

默认位置：

- `~/.volclog/config.json`

也可通过环境变量指定路径：

- `VOLCLOG_CONFIG=/path/to/config.json`

主路径（推荐）：
- `--endpoint` 必填；`--region` 可选（标准 endpoint 可推导 region，私网 endpoint 需显式提供 region）
- 多 region/多 endpoint 复用同一 AK/SK 时，推荐 `--cred-ref` 存一次 AK/SK，多个 profile 引用同一份凭证

常用命令：

```bash
volclog configure set --profile default --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com [--token <sts>] [--timeout-seconds 60]
volclog configure use default
volclog configure show --profile default
```

### 5.3 安全建议

- 建议通过环境变量或 CI Secret 注入 AK/SK，避免在命令行参数或脚本中明文写入。
- 如使用本地 Profile，请妥善保护 `~/.volclog/config.json`（包含敏感凭证），并避免将其纳入版本控制。
- 如需共享日志或提交 issue/工单，建议仅提供 `requestId/statusCode` 等排障信息，避免泄露密钥。

### 5.4 多租户 / 多账号 / 多 Region 组合配置（推荐用 Profile）

`volclog` 的 Profile 本质是一组“访问凭证 + region + endpoint +（可选）token + 超时”的组合。常见用法是将不同账号（不同 AK/SK）、不同租户、不同 region（或不同 endpoint 私网域名）分别保存为不同 profile，然后按需切换或按命令选择。

#### 5.4.1 为不同账号/租户创建多个 Profile

```bash
volclog configure set --profile tenant-a-cn --ak <ak_a> --sk <sk_a> --endpoint https://tls-cn-beijing.volces.com
volclog configure set --profile tenant-a-sg --ak <ak_a> --sk <sk_a> --endpoint https://tls-ap-singapore-1.volces.com
volclog configure set --profile tenant-b-cn --ak <ak_b> --sk <sk_b> --endpoint https://tls-cn-beijing.volces.com
```

说明：
- 仅提供 `--endpoint` 且 endpoint 命名符合 `tls-<region>.volces.com` 时，会尝试从 endpoint 推导 region；推导失败则仍要求显式提供 `--region`
- 如需使用自定义域名（例如私网 endpoint），可显式指定 `--endpoint`：

```bash
volclog configure set --profile tenant-a-cn-private --ak <ak_a> --sk <sk_a> --region cn-beijing --endpoint https://tls-private.example.com
```

##### 5.4.1.1 用 --cred-ref 复用 AK/SK（推荐）

当一个 AK/SK 需要跨多个 region/endpoint 共用时，推荐用 `--cred-ref` 将 AK/SK 存为“用户级凭证”，多个 profile 只引用同一份 AK/SK（可在配置中集中管理默认凭证以避免重复书写密钥）。

首次创建凭证并绑定一个 profile：

```bash
volclog configure set --profile abc-bj --cred-ref ma-abc-root --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
```

复用同一 AK/SK 创建另一个 region 的 profile（无需再传 --ak/--sk）：

```bash
volclog configure set --profile abc-sg --cred-ref ma-abc-root --endpoint https://tls-ap-singapore-1.volces.com
```

查看时会展示 `cred_ref` 与 `credential_present`（是否能找到对应凭证）：

```bash
volclog configure show --profile abc-bj
```

#### 5.4.2 选择 Profile 的三种方式

方式 A：切换默认 Profile（影响后续命令，适合交互式使用）
```bash
volclog configure use tenant-a-cn
volclog configure show
```

方式 B：单次命令选择 Profile（推荐，适合脚本/多环境并行）
```bash
volclog --profile tenant-a-cn project list
volclog --profile tenant-b-cn topic list --project-id <pid>
```

方式 C：用环境变量覆盖（优先级最高，适合 CI/容器）
当同时提供 `VOLCENGINE_ACCESS_KEY_ID` 与 `VOLCENGINE_ACCESS_KEY_SECRET` 时，会直接使用环境变量（覆盖 `--profile`/当前 profile）。常见做法是在不同 Job/不同容器注入不同 AK/SK + region/endpoint。

#### 5.4.3 多配置文件隔离（VOLCLOG_CONFIG）

当你希望把“生产/测试”“不同团队/不同租户”彻底隔离在不同配置文件时，可以使用：
- `VOLCLOG_CONFIG=/path/to/config.json`

示例：
```bash
VOLCLOG_CONFIG="$HOME/.volclog/config-prod.json" volclog configure set --profile prod-cn --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
VOLCLOG_CONFIG="$HOME/.volclog/config-test.json" volclog configure set --profile test-cn --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
```

#### 5.4.4 STS / 临时凭证（token）

若使用 STS 临时凭证，可在 profile 里保存 `--token`，或使用环境变量 `VOLCENGINE_TOKEN`：
```bash
volclog configure set --profile tenant-a-sts --ak <ak> --sk <sk> --token <sts_token> --endpoint https://tls-cn-beijing.volces.com
```

#### 5.4.5 Profile 列表与删除（多租户批量管理）

列出所有 profile：
```bash
volclog configure list
```

按前缀筛选（常用做法：用租户名作为 profile 前缀）：
```bash
volclog configure list --prefix tenant-a
```

删除单个 profile：
```bash
volclog configure delete tenant-a-cn
```

按前缀批量删除（危险操作，需要显式确认）：
```bash
volclog configure delete --prefix tenant-a --yes
```

***

## 6. 输入与输出规则（非常重要）

### 6.1 输入：普通参数 vs file:// 文件输入

大多数参数都是 `--key value`。当 value 很长或是复杂 JSON 时，推荐用 `file://`：

#### 6.1.1 file:// 的基本规则

- 以 `file://` 开头的参数值会被当作文件路径读取
- 读取到的内容会作为参数值（或 JSON）参与请求

例如：

```bash
volclog api call --method POST --path /SearchLogs --body file://./search.json
```

也支持用 `-` 从 stdin 读取（便于与管道组合）：

```bash
cat ./create_topic.json | volclog topic create --request -
```

#### 6.1.2 --request file://...（覆盖完整 JSON 请求体）

很多命令支持 `--request file://...`，直接传入完整 JSON 请求体（字段以服务端 swagger 为准），适合“参数太多/要 100% 对齐服务端”的场景。

示例：

```bash
volclog topic create --request file://./create_topic.json
volclog topic modify --topic-id <tid> --request file://./modify_topic.json
volclog metric-topic create --request file://./create_metric_topic.json
volclog log search --request file://./search_logs.json
```

#### 6.1.3 Prom 命令的 file://

Prom 命令大量参数都支持 `file://`（例如 `--query/--time/--start/--end/--match/--label-name`）。

`--match file://...` 支持两种文件格式：

- JSON 数组：`["up","rate(up[5m])"]`
- 按行文本：每行一个 match（空行会忽略）

### 6.2 时间格式（from/to/start/end/time）

对外建议统一使用毫秒级 Unix 时间戳（最终会以毫秒精度参与检索计算）：

- Unix 时间戳（毫秒）：`1710374400000`
- RFC3339：`2026-03-14T00:00:00Z`
- 本地时间：`2026-03-14 00:00:00`
- 日期：`2026-03-14`

说明：实现上也兼容秒级 Unix 时间戳输入，但文档与示例不对外展示该格式。

### 6.3 输出：JSON / JSONL

全局 `--output`：

- `--output json`（默认）：适合“资源查询/管理”
- `--output jsonl`：适合“日志导出/流式处理”（每行一条 JSON）

示例：

```bash
volclog project list --output json
volclog --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000
```

### 6.4 输出过滤：--jmes-filter（JMESPath）

当前 `--jmes-filter` 支持真实 JMESPath 表达式，可用于字段提取、数组投影、对象重组等场景。

示例：

```bash
volclog project list --jmes-filter "Projects[0].ProjectId"
volclog project list --jmes-filter "Projects[].ProjectName"
volclog project list --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
```

常见模式：

- `Projects[0].ProjectId`：取第一条记录的单个字段
- `Projects[].ProjectName`：投影出所有项目名
- `Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}`：重组成更适合脚本消费的对象数组

### 6.5 错误结构与退出码（给自动化集成用）

当命令失败时：

- stdout 通常为空
- stderr 输出 JSON 错误结构（包含 `errorCode/errorMessage/requestId/statusCode/kind/hint`）
- 退出码非 0

当用户输入缺少子命令或 `-h/--help` 时：

- 输出为可读的用法文本（非 JSON）
- 退出码：help 为 0；缺参为 1

退出码约定（用于自动化分支）：

- `1`：用法/参数错误（缺参、flag 值非法、stdin 为空或 JSON 非法等）
- `2`：请求或运行时失败（鉴权/网络/服务端错误等）
- `3`：输出/过滤/解码失败（例如 `--jmes-filter` 的 JMESPath 表达式错误、输出写入失败等）

### 6.6 自动化与诊断（Agent 推荐）

本节聚合说明当前实现中最适合自动化与 Agent 使用的能力：doctor、trace、输出落盘、项目级配置与 secrets file、completion。

#### 6.6.1 最短诊断路径：doctor

离线诊断（推荐先跑）：

```bash
volclog doctor
```

最小在线探测（验证鉴权/网络/endpoint，失败也会给出结构化输出）：

```bash
volclog doctor --online
```

当启用 `--online` 时，会尽量把失败拆成更可定位的检查项（不同环境可能因代理而跳过“直连检查”）：

- `online_endpoint_parse`：endpoint 是否可解析
- `online_proxy_detected`：是否检测到 HTTP(S) 代理（检测到时会跳过直连 DNS/TCP/TLS）
- `online_dns_resolve` / `online_tcp_connect` / `online_tls_handshake`：直连链路检查（可跳过）
- `online_describe_projects`：最小 API 探测（鉴权/服务可用性）
- `online_time_skew`：基于响应 `Date` 头估算时钟偏移（可选）

#### 6.6.2 工件化复盘：trace

对任意命令生成脱敏 trace 工件（JSONL）：

```bash
volclog --trace-dir ./.volclog/traces project list
```

返回位置说明：

- 非 `api` 组通常会在输出对象中附带 `meta.trace.path`
- `api` 组在 envelope 模式下会把 trace 路径放在 `summary.tracePath`

#### 6.6.3 大输出降噪：输出落盘（--output-mode file）

将命令输出写入文件，适合 CI/Agent 减少 stdout 体积：

```bash
volclog --output-mode file log search --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000
```

指定输出文件：

```bash
volclog --output-mode file --output-file ./out.json project list
```

指定默认输出目录（当未提供 --output-file 时生效）：

```bash
export VOLCLOG_OUTPUT_DIR="$HOME/.volclog/output"
volclog --output-mode file project list
```

补充说明：

- 普通 group 在 `--output-mode file` 下，stdout 通常只返回路径
- `api` 组成功时会返回 envelope，真实工件路径在 `artifacts[].path`

#### 6.6.4 项目级默认配置（.volclog/cli.config.json）

在项目根目录放置 `./.volclog/cli.config.json`，用于提供非敏感默认值（例如 region/endpoint/timeout/output/output_mode/output_dir/trace_redact/hints_file）：

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

该文件禁止写入任何凭证字段（access_key_id/secret_access_key/security_token 等），否则会报错。

#### 6.6.5 secrets file（dotenv）

从 dotenv 文件加载环境变量（不会覆盖已存在 env），适合 CI/本地隔离配置：

```bash
volclog --secrets-file ./.env project list
```

#### 6.6.6 shell completion

`completion` 用于生成 shell 自动补全脚本，让你在终端里输入 `volclog ...` 时可用 Tab 补全 group/子命令/常用参数（减少输错和查文档成本）。

补全边界（重要）：
- 全局参数只支持写在 group 之前（例如 `volclog --output json api call ...`）；把全局参数写在 group 后（例如 `volclog api --output json call ...`）属于不支持用法，补全也不会覆盖这种写法。
- zsh 补全支持“先全局参数，再 group/command”场景；bash/fish 也支持，但能力相对更轻量；PowerShell 侧也支持该用法，但补全能力略弱于 zsh。

能力差异（按 shell）：
- zsh：group/子命令/常用参数取值（如 `api call --method`）补全最完整
- bash：支持 group/部分子命令与 `api call` 常用参数取值补全
- fish：支持 group/`api call` 子命令与常用参数取值补全
- PowerShell：支持 group/主要二级子命令与 `api call` 常用参数取值补全（基础版）

生成补全脚本：

```bash
volclog completion <bash|zsh|fish|powershell>
```

##### zsh（oh-my-zsh，推荐）

```bash
mkdir -p ~/.oh-my-zsh/custom/completions
volclog completion zsh > ~/.oh-my-zsh/custom/completions/_volclog
rm -f ~/.zcompdump*
autoload -Uz compinit && compinit -i
```

##### zsh（非 oh-my-zsh）

```bash
mkdir -p ~/.zsh/completions
volclog completion zsh > ~/.zsh/completions/_volclog
```

在 `~/.zshrc` 中加入（如已有可跳过重复项）：

```zsh
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit
compinit
```

然后执行：

```bash
rm -f ~/.zcompdump*
source ~/.zshrc
```

##### bash

单次会话生效：

```bash
source <(volclog completion bash)
```

长期生效（追加到 `~/.bashrc`）：

```bash
echo 'source <(volclog completion bash)' >> ~/.bashrc
```

##### fish

```bash
mkdir -p ~/.config/fish/completions
volclog completion fish > ~/.config/fish/completions/volclog.fish
```

新开一个 fish 会话即可生效。

##### PowerShell

把补全脚本追加到你的 PowerShell profile（只需一次）：

```powershell
if (!(Test-Path $PROFILE)) { New-Item -ItemType File -Force -Path $PROFILE | Out-Null }
volclog completion powershell | Add-Content -Path $PROFILE
```

重新打开 PowerShell 即可生效。

## 7. 命令参考（按功能分组）

你可以先记住一条规律：

- `volclog <group> -h`：看这个组有什么子命令和例子
- `volclog <group> <command> ...`：执行具体操作

### 7.1 全局用法

```bash
volclog [--profile <name>] [--output json|jsonl] [--output-mode stdout|file] [--output-file <path>] [--jmes-filter <expr>] [--trace-dir <path>] [--trace-redact strict|default] [--secrets-file <path>] [--dry-run] <group> <command> [args]
```

全局选项：

- `--profile <name>`：指定 profile 名
- `--output json|jsonl`：输出格式
- `--output-mode stdout|file`：输出到 stdout 或落盘（file 模式 stdout 输出文件路径）
- `--output-file <path>`：file 模式的输出文件路径（未提供则写入 `./.volclog/output/`）
- `--jmes-filter <expr>`：JMESPath 输出过滤
- `--trace-dir <path>`：生成 trace 工件（脱敏 JSONL）
- `--trace-redact strict|default`：trace 脱敏级别（默认 strict）
- `--secrets-file <path>`：从 dotenv 文件加载环境变量（不覆盖已存在 env）
- `--dry-run`：当前仅支持 `api` 组，返回本地校验计划而不真正发请求
- `--help/-h`：帮助
- `--version`：版本

### 7.2 configure（本地配置）

```bash
volclog configure -h
```

### 7.3 api（通用 OpenAPI 调用兜底）

```bash
volclog api -h
```

### 7.4 capabilities（机器可读能力契约）

```bash
volclog capabilities -h
```

### 7.5 capabilities --view text（人类可读命令清单）

```bash
volclog capabilities -h
volclog capabilities --view text
```

### 7.6 project（日志项目）

```bash
volclog project -h
```

### 7.7 topic（日志主题）

```bash
volclog topic -h
```

### 7.8 metric-topic（指标主题 + Prom 查询）

```bash
volclog metric-topic -h
volclog metric-topic prom -h
```

### 7.9 index（索引）

```bash
volclog index -h
```

### 7.10 log（日志检索/导出）

```bash
volclog log -h
```

### 7.11 assistant（TLS Assistant）

```bash
volclog assistant -h
```

### 7.12 doctor（诊断）

```bash
volclog doctor
```

### 7.13 completion（补全脚本）

```bash
volclog completion zsh
```

***

## 8. 逐命令参数手册（参数说明 + body 示例）

本章以“逐命令”的方式把每个参数讲清楚，并把 `file://` 与请求体（JSON）示例文件完整列出，方便你直接复制使用。

示例文件目录：
- [examples](./examples/README.md)

> 说明：示例 JSON 中的 `00000000-0000-0000-0000-000000000000` 只是占位符，你需要替换成真实的 `ProjectId/TopicId`。

### 8.1 全局参数（所有命令都可用）

命令通用格式：

```bash
volclog [--profile <name>] [--output json|jsonl] [--output-mode stdout|file] [--output-file <path>] [--jmes-filter <expr>] [--trace-dir <path>] [--trace-redact strict|default] [--secrets-file <path>] [--dry-run] <group> <command> [args]
```

注意：
- 全局参数只支持写在 group 之前（例如 `volclog --output json api call ...`），不要写成 `volclog api --output json call ...`

| 参数                     | 必填 | 说明                                                                | 示例                                      |
| ---------------------- | -: | ----------------------------------------------------------------- | --------------------------------------- |
| `--profile <name>`     |  否 | 使用指定 profile（未提供则使用 current profile 或 default；若环境变量 AK/SK 存在则被覆盖） | `--profile prod`                        |
| `--output json\|jsonl` |  否 | 输出格式。资源管理建议 `json`；日志导出建议 `jsonl`                                 | `--output jsonl`                        |
| `--output-mode`        |  否 | 输出到 stdout 或落盘（file 模式 stdout 输出文件路径）                             | `--output-mode file`                    |
| `--output-file`        |  否 | file 模式的输出文件路径（未提供则写入 `./.volclog/output/`）                          | `--output-file ./out.json`             |
| `--jmes-filter <expr>` |  否 | JMESPath 输出过滤，可做字段提取、投影与对象重组                                      | `--jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"` |
| `--trace-dir`          |  否 | 生成 trace 工件（脱敏 JSONL），输出中带 `meta.trace.path`                         | `--trace-dir ./.volclog/traces`         |
| `--trace-redact`       |  否 | trace 脱敏级别（默认 strict）                                            | `--trace-redact strict`                 |
| `--secrets-file`       |  否 | 从 dotenv 文件加载环境变量（不覆盖已存在 env）                                   | `--secrets-file ./.env`                 |
| `--dry-run`            |  否 | 当前仅支持 `api` 组，返回本地校验计划而不真正发请求                              | `--dry-run`                             |
| `-h/--help`            |  否 | 帮助（对 group 也有效）                                                   | `volclog metric-topic -h`                |

#### 8.1.1 全局参数最佳实践

- 补充阅读：[docs/cli-best-practices.md](./docs/cli-best-practices.md)。
- `--profile <name>`：推荐用于本地/脚本多环境切换；搭配 `configure use` 可设置默认 profile。CI/容器中如已注入 `VOLCENGINE_ACCESS_KEY_ID/SECRET`，则环境变量优先于 profile。
- `--output json|jsonl`：资源管理（project/topic/index/metric-topic）优先用 `json`；日志/指标导出或大结果集优先用 `jsonl`，便于管道处理（`jq -c`、`awk`、`grep` 等）。
- `--output-mode file`：当输出很大（日志导出、SearchLogs/Prom 查询返回大量数据）或在 Agent/CI 中需要控制 stdout 体积时使用；普通 group 的 stdout 通常只返回路径，`api` 组则返回带 `artifacts[].path` 的 envelope。
- `--output-file`：配合 `--output-mode file` 使用，固定输出路径；未指定时会写入 `./.volclog/output/`。
- `--jmes-filter <expr>`：用于在 CLI 侧先做字段提取、数组投影和对象重组；推荐在脚本里稳定提取 `ProjectId/TopicId` 或裁剪大对象输出。
- `--trace-dir <path>`：排障推荐开启；会生成脱敏 trace 工件（JSONL）；非 `api` 组通常在 `meta.trace.path` 返回路径，`api` 组在 envelope 的 `summary.tracePath` 返回路径。
- `--trace-redact strict|default`：默认 `strict`；排障中若需要更多上下文字段可切换为 `default`（仍会做脱敏，但信息更丰富）。
- `--secrets-file <path>`：推荐用于 CI/本地隔离密钥场景（dotenv）；它会加载 env 但不会覆盖已存在 env。注意它不是 `configure` 的参数，通常只对实际发请求的命令有意义（project/topic/index/log/metric-topic/api/assistant 等）。
- `--dry-run`：当前只对 `api` 组生效，适合 Agent 在真正发请求前做 profile / endpoint / body JSON 的本地校验。

### 8.2 configure（本地配置）

#### 8.2.1 configure set

用途：新增/更新一个 profile。

```bash
volclog configure set --profile <name> [--cred-ref <cred>] [--ak <ak> --sk <sk>] [--token <sts>] [--region <region>] --endpoint <endpoint> [--timeout-seconds <n>]
```

| 参数                  | 必填 | 说明                                                                 | 示例                                             |
| ------------------- | -: | ------------------------------------------------------------------ | ---------------------------------------------- |
| `--profile`         |  是 | profile 名称                                                         | `--profile default`                            |
| `--cred-ref`        |  否 | 引用用户级凭证（AK/SK），可用于多个 profile 复用同一 AK/SK；与 `--ak/--sk` 二选一 | `--cred-ref ma-abc-root`                       |
| `--ak`              |  否 | AccessKeyID（与 `--sk` 成对；与 `--cred-ref` 二选一）                   | `--ak $VOLCENGINE_ACCESS_KEY_ID`               |
| `--sk`              |  否 | SecretAccessKey（与 `--ak` 成对；与 `--cred-ref` 二选一）               | `--sk $VOLCENGINE_ACCESS_KEY_SECRET`           |
| `--token`           |  否 | STS Token（可选）                                                    | `--token $VOLCENGINE_TOKEN`                    |
| `--region`          |  否 | 地域。省略且 endpoint 命名符合 `tls-<region>.volces.com` 时会从 endpoint 推导 | `--region cn-beijing`                          |
| `--endpoint`        |  是 | 服务地址（推荐显式指定）                                                 | `--endpoint https://tls-cn-beijing.volces.com` |
| `--timeout-seconds` |  否 | HTTP 超时秒数（默认 60）                                                   | `--timeout-seconds 60`                         |

说明：
- 需要提供一组可用凭证：`--ak/--sk` 或 `--cred-ref`（指向已存在的用户级凭证）
- `--endpoint` 为必填；仅提供 `--endpoint` 时会尝试推导 region（推导失败则仍要求显式 `--region`）

#### 8.2.2 configure use

用途：设置默认 profile（后续不写 `--profile` 时使用）。

```bash
volclog configure use <name>
```

#### 8.2.3 configure show

用途：查看 profile（AK 会脱敏显示）。

```bash
volclog configure show --profile <name>
```

### 8.3 api（通用 OpenAPI 调用兜底）

用途：当某个接口 CLI 还未封装时，用 `api call` 直接调用服务端 OpenAPI。

```bash
volclog api call --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...>]
```

| 参数         | 必填 | 说明                       | 示例                                 | 支持 file:// |
| ---------- | -: | ------------------------ | ---------------------------------- | ---------: |
| `--method` |  是 | HTTP 方法                  | `--method GET`                     |          否 |
| `--path`   |  是 | API 路径（以 `/` 开头）         | `--path /DescribeProjects`         |          否 |
| `--query`  |  否 | Query 参数，支持多次出现          | `--query ProjectId=<pid>`          |          否 |
| `--header` |  否 | Header，支持多次出现            | `--header x-tls-apiversion=0.3.0`  |          否 |
| `--body`   |  否 | 请求体（JSON 字符串或 `file://`） | `--body file://./search_logs.json` |          是 |

示例：

```bash
volclog api call --method GET --path /DescribeProject --query ProjectId=<pid>
volclog api call --method POST --path /SearchLogs --body file://./search_logs.json
```

### 8.4 project（日志项目）

#### 8.4.1 project list

```bash
volclog project list [--page-number <n>] [--page-size <n>] [--project-name <s>] [--project-id <s>] [--fuzzy-search-key <s>] [--description <s>] [--is-full-name|--no-is-full-name] [--iam-project-name <s>] [--tags <s|file://...>] [--favourite|--no-favourite] [--topic-types <s>]
```

| 参数                                 | 必填 | 说明                                      | 示例                                               | 支持 file:// |
| ---------------------------------- | -: | --------------------------------------- | ------------------------------------------------ | ---------: |
| `--page-number`                    |  否 | 页码                                      | `--page-number 1`                                |          否 |
| `--page-size`                      |  否 | 页大小                                     | `--page-size 20`                                 |          否 |
| `--project-name`                   |  否 | 项目名模糊查询                                 | `--project-name test`                            |          否 |
| `--project-id`                     |  否 | 项目 ID 模糊查询                              | `--project-id 1e2fd27e`                          |          否 |
| `--fuzzy-search-key`               |  否 | 模糊搜索 key（可覆盖 id/name/topicId/topicName） | `--fuzzy-search-key demo`                        |          否 |
| `--description`                    |  否 | 描述模糊匹配                                  | `--description desc`                             |          否 |
| `--is-full-name/--no-is-full-name` |  否 | 是否完整匹配项目名                               | `--is-full-name`                                 |          否 |
| `--iam-project-name`               |  否 | IAM 项目名过滤                               | `--iam-project-name default`                     |          否 |
| `--tags`                           |  否 | tags 过滤（服务端要求为字符串；通常是 JSON 字符串）         | `--tags '[{\"Key\":\"env\",\"Value\":\"dev\"}]'` |          是 |
| `--favourite/--no-favourite`       |  否 | 是否收藏过滤                                  | `--favourite`                                    |          否 |
| `--topic-types`                    |  否 | TopicTypes                              | `--topic-types xxx`                              |          否 |

#### 8.4.2 project get

```bash
volclog project get --project-id <pid> [--topic-types <s>]
```

#### 8.4.3 project create

方式 A：用参数快速创建（Region 默认来自 profile/环境变量）

```bash
volclog project create --project-name <name> [--description <s>] [--iam-project-name <s>] [--region <region>] [--tags <json-array|file://...>] [--request <file://...>]
```

方式 B：用 `--request` 传入完整 JSON（推荐更“对齐 swagger”）

```bash
volclog project create --request file://./examples/create_project.json
```

`create_project.json` 示例（完整文件见 [create_project.json](./examples/create_project.json)）：

```json
{
  "ProjectName": "demo-project",
  "Description": "demo project created by volclog",
  "Region": "cn-beijing",
  "IamProjectName": "default",
  "Tags": [
    { "Key": "env", "Value": "dev" },
    { "Key": "owner", "Value": "alice" }
  ]
}
```

#### 8.4.4 project modify

```bash
volclog project modify --project-id <pid> [--project-name <name>] [--description <s>] [--favourite|--no-favourite] [--request <file://...>]
```

`modify_project.json` 示例（完整文件见 [modify_project.json](./examples/modify_project.json)）：

```json
{
  "ProjectId": "00000000-0000-0000-0000-000000000000",
  "Description": "updated description",
  "Favourite": true
}
```

#### 8.4.5 project delete

```bash
volclog project delete --project-id <pid>
```

### 8.5 topic（日志主题）

#### 8.5.1 topic list

```bash
volclog topic list [--page-number <n>] [--page-size <n>] [--cursor <s>] [--region <s>] [--project-id <pid>] [--project-name <s>] [--topic-name <s>] [--topic-id <s>] [--fuzzy-search-key <s>] [--description <s>] [--tags <s|file://...>] [--favourite|--no-favourite] [--order-by-project|--no-order-by-project]
```

重要限制：

- `--topic-name` 与 `--topic-id` 不能同时提供（服务端约束）

#### 8.5.2 topic get

```bash
volclog topic get --topic-id <tid>
```

#### 8.5.3 topic create

方式 A：命令行参数（适合只填核心字段）

```bash
volclog topic create --project-id <pid> --topic-name <name> --ttl <days> --shard-count <n> [--description <s>] [--auto-split] [--max-split-shard <n>] [--enable-tracking|--disable-tracking] [--metering-mode <s>] [--log-public-ip|--no-log-public-ip] [--enable-hot-ttl|--disable-hot-ttl] [--hot-ttl <days>] [--cold-ttl <days>] [--archive-ttl <days>] [--time-key <s>] [--time-format <s>] [--encrypt-conf <json|file://...>] [--tags <json-array|file://...>] [--request <file://...>]
```

方式 B：用 `--request`（强烈推荐）

```bash
volclog topic create --request file://./examples/create_topic.json
```

`create_topic.json` 示例（完整文件见 [create_topic.json](./examples/create_topic.json)）：

```json
{
  "ProjectId": "00000000-0000-0000-0000-000000000000",
  "TopicName": "demo-topic",
  "Description": "demo topic created by volclog",
  "Ttl": 30,
  "ShardCount": 2,
  "AutoSplit": true,
  "MaxSplitShard": 10,
  "EnableTracking": false,
  "MeteringMode": "ChargeByFunction",
  "LogPublicIP": false,
  "TimeKey": "requestTime",
  "TimeFormat": "%F %T",
  "EncryptConf": {
    "enable": false,
    "encrypt_type": "",
    "user_cmk_info": {
      "from_tls": false,
      "region_id": "",
      "trn": "",
      "user_cmk_id": ""
    }
  },
  "Tags": [
    { "Key": "env", "Value": "dev" }
  ]
}
```

#### 8.5.4 topic modify

```bash
volclog topic modify --topic-id <tid> [--topic-name <name>] [--description <s>] [--ttl <days>] [--auto-split|--no-auto-split] [--max-split-shard <n>] [--enable-tracking|--disable-tracking] [--favourite|--no-favourite] [--metering-mode <s>] [--log-public-ip|--no-log-public-ip] [--enable-hot-ttl|--disable-hot-ttl] [--hot-ttl <days>] [--cold-ttl <days>] [--archive-ttl <days>] [--time-key <s>] [--time-format <s>] [--encrypt-conf <json|file://...>] [--request <file://...>]
```

`modify_topic.json` 示例（完整文件见 [modify_topic.json](./examples/modify_topic.json)）：

```json
{
  "TopicId": "00000000-0000-0000-0000-000000000000",
  "Description": "updated topic description",
  "Ttl": 60,
  "AutoSplit": true,
  "MaxSplitShard": 10,
  "EnableTracking": false,
  "Favourite": true,
  "LogPublicIP": false,
  "TimeKey": "requestTime",
  "TimeFormat": "%F %T"
}
```

#### 8.5.5 topic delete

```bash
volclog topic delete --topic-id <tid>
```

### 8.6 metric-topic（指标主题 + 指标查询 + Prom 协议）

#### 8.6.1 metric-topic list/get/create/modify/delete

和 topic 基本一致，但资源为 MetricTopic（独立 API：CreateMetricTopic/ModifyMetricTopic/...）。

创建示例：

```bash
volclog metric-topic create --request file://./examples/create_metric_topic.json
```

`create_metric_topic.json` 示例（完整文件见 [create_metric_topic.json](./examples/create_metric_topic.json)）：

```json
{
  "ProjectId": "00000000-0000-0000-0000-000000000000",
  "TopicName": "demo-metric-topic",
  "Description": "metric topic created by volclog",
  "Ttl": 30,
  "ShardCount": 2,
  "AutoSplit": true,
  "MaxSplitShard": 10,
  "Tags": [
    { "Key": "env", "Value": "dev" }
  ]
}
```

#### 8.6.2 metric-topic search（用 SearchLogs 查询指标数据）

该命令本质调用 `/SearchLogs`，你可以把 `--query` 写成 SQL、PromQL、PromQL+SQL（取决于服务端能力与语法）。

```bash
volclog metric-topic search --topic-id <metric_tid> --query <expr> --from <t> --to <t> [--limit <n>] [--sort <asc|desc>] [--context <s>] [--highlight] [--accurate-query|--no-accurate-query] [--must-complete|--no-must-complete] [--offset <n>] [--request <file://...>]
```

SearchLogs 请求体示例（完整文件见 [search_logs.json](./examples/search_logs.json)）：

```json
{
  "TopicId": "00000000-0000-0000-0000-000000000000",
  "Query": "* | limit 10",
  "StartTime": 1710374400000,
  "EndTime": 1710378000000,
  "Limit": 100,
  "Sort": "desc",
  "HighLight": false,
  "AccurateQuery": false,
  "MustComplete": false,
  "Offset": 0
}
```

#### 8.6.3 metric-topic prom（Prometheus HTTP API 兼容）

对应接口（GET/POST 均支持）：

- `/topic/{topic_id}/api/v1/query`
- `/topic/{topic_id}/api/v1/query_range`
- `/topic/{topic_id}/api/v1/series`
- `/topic/{topic_id}/api/v1/labels`
- `/topic/{topic_id}/api/v1/label/{label_name}/values`

```bash
volclog metric-topic prom <query|query-range|series|labels|label-values> [--method GET|POST] ...
```

常用参数说明（各子命令共同点）：

| 参数                     | 说明                              | 支持 file:// |
| ---------------------- | ------------------------------- | ---------: |
| `--topic-id`           | 指标主题 ID                         |          否 |
| `--method`             | GET 或 POST（默认 GET）              |          否 |
| `--start/--end/--time` | Prom 时间（支持 RFC3339/秒/毫秒）        |          是 |
| `--query`              | PromQL 表达式                      |          是 |
| `--match`              | match\[]，可多次出现；也支持 file:// 读取多条 |          是 |
| `--label-name`         | label 名称（label-values 子命令）      |          是 |

PromQL 示例文件：

- [promql.txt](./examples/promql.txt)
- [time.txt](./examples/time.txt)
- [match.json](./examples/match.json)
- [match.txt](./examples/match.txt)

### 8.7 index（索引）

#### 8.7.1 index get

```bash
volclog index get --topic-id <tid>
```

#### 8.7.2 index create / modify

```bash
volclog index create --topic-id <tid> --body file://./index.json
volclog index modify --topic-id <tid> --body file://./index.json
```

说明：

- `--body` 必须是 JSON 对象
- `TopicId` 会由命令行 `--topic-id` 自动注入到 body 中（你可以在 index.json 里不写 TopicId）

索引 body 示例（完整文件见 [index.json](./examples/index.json)）：

```json
{
  "EnableAutoIndex": true,
  "FullText": {
    "CaseSensitive": false,
    "Delimiter": " \\t\\n",
    "IncludeChinese": true
  },
  "KeyValue": [
    {
      "Key": "level",
      "Value": {
        "ValueType": "text",
        "CaseSensitive": false,
        "SqlFlag": true
      }
    }
  ],
  "MaxTextLen": 2048
}
```

### 8.8 log（日志检索与导出）

#### 8.8.1 log search

```bash
volclog log search --topic-id <tid> --query <expr> --from <t> --to <t> [--limit <n>] [--context <s>] [--sort <asc|desc>] [--highlight] [--accurate-query|--no-accurate-query] [--must-complete|--no-must-complete] [--offset <n>] [--request <file://...>]
```

#### 8.8.2 log export

用途：自动翻页导出（内部基于 SearchLogs + Context）。

```bash
volclog --output jsonl log export --topic-id <tid> --query <expr> --from <t> --to <t> [--limit <n>] [--max-pages <n>] [--request <file://...>]
```

#### 8.8.3 log export-analysis

用途：导出 SQL/Analysis 查询结果（逐行输出 `AnalysisResult.Data`，每行一个带列名对象）。不使用 Context 分页；翻页由你在 `--query` 的 SQL 中写 `offset/limit` 控制。

```bash
volclog log export-analysis --topic-id <tid> --query "*|select count(*) as cnt group by __time__ limit 100" --from <t> --to <t> [--request <file://...>]
```

***

## 9. 常见场景与示例

### 9.1 获取 ProjectId/TopicId

建议先执行列表查询：

```bash
volclog project list
volclog topic list --project-id <ProjectId>
volclog metric-topic list --project-id <ProjectId>
```

### 9.2 使用文件输入请求体，避免参数遗漏

```bash
volclog topic create --request file://./create_topic.json
volclog log search --request file://./search_logs.json
```

### 9.3 使用文件输入 PromQL（避免命令行过长）

```bash
volclog metric-topic prom query --topic-id <tid> --query file://./promql.txt --time 1710374400000
```

### 9.4 批量导出日志（供下游处理）

```bash
volclog --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 100
```

***

## 10. 故障排查与 FAQ

### 10.1 “profile not found / missing region / missing endpoint”

- 先确认你是否配置了环境变量（会覆盖 profile）
- 或运行：

```bash
volclog configure show --profile default
```

### 10.2 “签名/鉴权失败（403/401）”

- 检查 AK/SK 是否正确
- 检查 region/endpoint 是否匹配（例如 cn-beijing 对应 tls-cn-beijing）
- 如果使用临时凭证，确认 `VOLCENGINE_TOKEN`（或 profile token）已配置

### 10.3 “服务端返回错误但我不知道怎么排查”

关注 stderr JSON 中的：

- `requestId`：提供给服务端排查非常关键
- `statusCode`：快速判断是鉴权/参数/限流/服务端错误

***
