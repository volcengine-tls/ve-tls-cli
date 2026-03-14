# tlsctl（火山引擎 TLS 日志服务 CLI）用户指南

本指南面向使用命令行与日志服务的用户，尽量把“要做什么、怎么做、为什么这样做”讲清楚。你可以把 `tlsctl` 理解成：用命令行完成“创建日志项目/主题/索引、查询与导出日志、管理指标主题与 Prom 查询接口”的工具。

> 如果你打算把日志服务能力接入 skills/openclaw/agent：建议默认使用 `--output json`（或导出用 `--output jsonl`），并在失败时读取 stderr 的 JSON 错误结构。

***

## 目录

- <br />

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

如果你只配置了 `region`，`tlsctl` 会自动推导 endpoint：

- `cn-beijing` → `https://tls-cn-beijing.volces.com`

***

## 2. 安装

`tlsctl` 是 Go 工具。当前模块要求 Go 版本：**Go 1.22+**。

### 2.0 安装方式对比（推荐先看这里）

| 安装方式 | 适合谁 | 依赖 | 是否需要 Go | 是否需要网络下载 |
|---|---|---|---:|---:|
| 一键安装（本地编译） | 已拿到源码、机器上有 Go 的开发者 | `bash`、`go(>=1.22)` | 是 | 否（仅编译本地代码） |
| GitHub Release 预编译包（macOS/Linux） | CI/服务器/受限环境（不安装 Go） | `bash`、`curl`、`tar`、（可选）`shasum` 或 `sha256sum` | 否 | 是 |
| GitHub Release 预编译包（Windows） | Windows 环境（不安装 Go） | PowerShell 5+（或 PowerShell 7+） | 否 | 是 |
| Docker 运行 | 需要隔离运行环境/不安装 Go | `docker` | 否 | 是（拉镜像/构建） |
| 手动 go build/go install | Go 用户/二次开发 | `go(>=1.22)` | 是 | 否（仅编译本地代码） |

### 2.1 一键安装（直接可用，推荐）

适用于希望快速完成本地安装的场景：在仓库根目录执行即可，脚本会把二进制安装到 `~/.local/bin/tlsctl`。

依赖：
- `bash`
- `go`（Go 1.22+）

```bash
bash scripts/install-local.sh
~/.local/bin/tlsctl --help
```

如需安装到其他目录：

```bash
PREFIX=/usr/local bash scripts/install-local.sh
```

如果安装后提示 `command not found`，请将 `~/.local/bin` 加入 PATH（仅需配置一次）：

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
tlsctl --help
```

### 2.2 没有 Go 环境怎么办？

#### 2.2.1 方案 A：下载预编译二进制

前提：我们需要提供对应系统的 release 包（例如内部制品库或 GitHub Releases）。

macOS/Linux 安装脚本（需要 curl + tar）：

依赖：
- `bash`
- `curl`
- `tar`
- （可选）`shasum` 或 `sha256sum`（若 release 同目录提供 `.sha256` 文件则会校验）

```bash
TLSCTL_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download" bash scripts/install-binary.sh
~/.local/bin/tlsctl --help
```

说明：

- 脚本会自动识别 OS/Arch
- 下载文件名固定为 `tlsctl_${os}_${arch}.tar.gz`（通过 `TLSCTL_BASE_URL` 指向不同 release）
- 若同路径存在 `.sha256` 文件会自动校验（格式为：`<sha256>  <filename>`）
- 默认安装到 `~/.local/bin/tlsctl`（可用 `PREFIX=/usr/local` 改路径）

如需固定安装某个版本（建议用于生产/CI），可使用 tag 作为版本：
```bash
TLSCTL_BASE_URL="https://github.com/volcengine-tls/ve-tls-cli/releases/download/tlsctl-v0.0.1" bash scripts/install-binary.sh
```

Windows 安装脚本（PowerShell）：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

固定安装某个版本：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -BaseUrl "https://github.com/volcengine-tls/ve-tls-cli/releases/download/tlsctl-v0.0.1"
```

#### 2.2.2 方案 B：用 Docker 运行（无需本机 Go）

前提：运行环境已安装 Docker。

依赖：
- `docker`

```bash
docker build -t tlsctl:local .
docker run --rm tlsctl:local --help
```

如需传入环境变量（例如 AK/SK/Region/Endpoint）：

```bash
docker run --rm \
  -e VOLCENGINE_ACCESS_KEY_ID \
  -e VOLCENGINE_ACCESS_KEY_SECRET \
  -e VOLCENGINE_TOKEN \
  -e VOLCENGINE_REGION \
  -e VOLCENGINE_ENDPOINT \
  tlsctl:local project list
```

### 2.3 从源码编译（推荐）

```bash
cd ve-tls-cli
go build ./cmd/tlsctl
./tlsctl --help
```

### 2.4 安装到 GOPATH/bin（可选）

```bash
cd ve-tls-cli
go install ./cmd/tlsctl
tlsctl --help
```

***

## 3. 快速上手（示例流程）

下面给出一套示例流程：配置 → 创建项目 → 创建主题 → 创建索引 → 查询日志；并包含指标主题与 Prom 查询示例。

> 提示：可在任意命令后追加 `-h` 查看该组用法，例如：`tlsctl metric-topic -h`、`tlsctl metric-topic prom -h`。

### 3.1 方式 A：用环境变量（适合 CI / 容器）

```bash
export VOLCENGINE_ACCESS_KEY_ID="你的AK"
export VOLCENGINE_ACCESS_KEY_SECRET="你的SK"
export VOLCENGINE_REGION="cn-beijing"
export VOLCENGINE_ENDPOINT="https://tls-cn-beijing.volces.com"
# 可选：export VOLCENGINE_TOKEN="你的STS Token"

./tlsctl project list
```

### 3.2 方式 B：写入本地 Profile（适合个人电脑）

```bash
./tlsctl configure set --profile default --ak "你的AK" --sk "你的SK" --region cn-beijing
./tlsctl configure use default
./tlsctl configure show --profile default
```

### 3.3 创建一个日志项目（Project）

```bash
./tlsctl project create --project-name demo-project --description "demo"
```

成功后会返回 `ProjectId`。后续操作都推荐使用 ID（更稳定）。

### 3.4 创建一个日志主题（Topic）

```bash
./tlsctl topic create --project-id <ProjectId> --topic-name demo-topic --ttl 30 --shard-count 2 --auto-split --max-split-shard 10
```

### 3.5 创建/修改索引（Index）

索引决定你能否按字段检索/分析。通常需要先准备一个 JSON 文件，例如 `index.json`。

```bash
./tlsctl index create --topic-id <TopicId> --body file://./index.json
# 如果已存在索引，改用 modify
./tlsctl index modify --topic-id <TopicId> --body file://./index.json
```

### 3.6 查询日志（SearchLogs）

```bash
./tlsctl log search --topic-id <TopicId> --query "*" --from "2026-03-14 00:00:00" --to "2026-03-14 01:00:00"
```

### 3.7 导出日志（JSONL 流式）

```bash
./tlsctl --output jsonl log export --topic-id <TopicId> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 10
```

### 3.8 指标主题（MetricTopic）与 Prom 查询

```bash
./tlsctl metric-topic list --project-id <ProjectId>
./tlsctl metric-topic prom query --topic-id <MetricTopicId> --query 'up' --time 1710374400000
./tlsctl metric-topic prom query-range --topic-id <MetricTopicId> --query 'rate(up[5m])' --start 1710374400000 --end 1710378000000 --step 15
```

***

## 4. 配置说明（环境变量 / Profile）

### 4.1 环境变量（优先级最高）

当以下环境变量都具备时，`tlsctl` 会优先使用它们（覆盖 profile）：

- `VOLCENGINE_ACCESS_KEY_ID`
- `VOLCENGINE_ACCESS_KEY_SECRET`
- `VOLCENGINE_TOKEN`（可选）
- `VOLCENGINE_REGION`
- `VOLCENGINE_ENDPOINT`

适用于：

- CI/CD
- 容器运行
- skills/openclaw runtime 注入密钥

### 4.2 本地 Profile 配置文件

默认位置：

- `~/.tlsctl/config.json`

也可通过环境变量指定路径：

- `TLSCTL_CONFIG=/path/to/config.json`

常用命令：

```bash
tlsctl configure set --profile default --ak <ak> --sk <sk> --region cn-beijing [--endpoint https://tls-cn-beijing.volces.com] [--token <sts>] [--timeout-seconds 60]
tlsctl configure use default
tlsctl configure show --profile default
```

### 4.3 安全建议

- 建议通过环境变量或 CI Secret 注入 AK/SK，避免在命令行参数或脚本中明文写入。
- 如使用本地 Profile，请妥善保护 `~/.tlsctl/config.json`（包含敏感凭证），并避免将其纳入版本控制。
- 如需共享日志或提交 issue/工单，建议仅提供 `requestId/statusCode` 等排障信息，避免泄露密钥。

### 4.4 多租户 / 多账号 / 多 Region 组合配置（推荐用 Profile）

`tlsctl` 的 Profile 本质是一组“访问凭证 + region + endpoint +（可选）token + 超时”的组合。常见用法是将不同账号（不同 AK/SK）、不同租户、不同 region（或不同 endpoint 私网域名）分别保存为不同 profile，然后按需切换或按命令选择。

#### 4.4.1 为不同账号/租户创建多个 Profile

```bash
tlsctl configure set --profile tenant-a-cn --ak <ak_a> --sk <sk_a> --region cn-beijing
tlsctl configure set --profile tenant-a-sg --ak <ak_a> --sk <sk_a> --region ap-singapore-1
tlsctl configure set --profile tenant-b-cn --ak <ak_b> --sk <sk_b> --region cn-beijing
```

说明：
- 仅提供 `--region` 时会自动推导 endpoint：`https://tls-<region>.volces.com`
- 如需使用自定义域名（例如私网 endpoint），可显式指定 `--endpoint`：

```bash
tlsctl configure set --profile tenant-a-cn-private --ak <ak_a> --sk <sk_a> --region cn-beijing --endpoint https://tls-private.example.com
```

#### 4.4.2 选择 Profile 的三种方式

方式 A：切换默认 Profile（影响后续命令，适合交互式使用）
```bash
tlsctl configure use tenant-a-cn
tlsctl configure show
```

方式 B：单次命令选择 Profile（推荐，适合脚本/多环境并行）
```bash
tlsctl --profile tenant-a-cn project list
tlsctl --profile tenant-b-cn topic list --project-id <pid>
```

方式 C：用环境变量覆盖（优先级最高，适合 CI/容器）
当同时提供 `VOLCENGINE_ACCESS_KEY_ID` 与 `VOLCENGINE_ACCESS_KEY_SECRET` 时，会直接使用环境变量（覆盖 `--profile`/当前 profile）。常见做法是在不同 Job/不同容器注入不同 AK/SK + region/endpoint。

#### 4.4.3 多配置文件隔离（TLSCTL_CONFIG）

当你希望把“生产/测试”“不同团队/不同租户”彻底隔离在不同配置文件时，可以使用：
- `TLSCTL_CONFIG=/path/to/config.json`

示例：
```bash
TLSCTL_CONFIG="$HOME/.tlsctl/config-prod.json" tlsctl configure set --profile prod-cn --ak <ak> --sk <sk> --region cn-beijing
TLSCTL_CONFIG="$HOME/.tlsctl/config-test.json" tlsctl configure set --profile test-cn --ak <ak> --sk <sk> --region cn-beijing
```

#### 4.4.4 STS / 临时凭证（token）

若使用 STS 临时凭证，可在 profile 里保存 `--token`，或使用环境变量 `VOLCENGINE_TOKEN`：
```bash
tlsctl configure set --profile tenant-a-sts --ak <ak> --sk <sk> --token <sts_token> --region cn-beijing
```

#### 4.4.5 Profile 列表与删除（多租户批量管理）

列出所有 profile：
```bash
tlsctl configure list
```

按前缀筛选（常用做法：用租户名作为 profile 前缀）：
```bash
tlsctl configure list --prefix tenant-a
```

删除单个 profile：
```bash
tlsctl configure delete tenant-a-cn
```

按前缀批量删除（危险操作，需要显式确认）：
```bash
tlsctl configure delete --prefix tenant-a --yes
```

***

## 5. 输入与输出规则（非常重要）

### 5.1 输入：普通参数 vs file:// 文件输入

大多数参数都是 `--key value`。当 value 很长或是复杂 JSON 时，推荐用 `file://`：

#### 5.1.1 file:// 的基本规则

- 以 `file://` 开头的参数值会被当作文件路径读取
- 读取到的内容会作为参数值（或 JSON）参与请求

例如：

```bash
tlsctl api call --method POST --path /SearchLogs --body file://./search.json
```

#### 5.1.2 --request file://...（覆盖完整 JSON 请求体）

很多命令支持 `--request file://...`，直接传入完整 JSON 请求体（字段以服务端 swagger 为准），适合“参数太多/要 100% 对齐服务端”的场景。

示例：

```bash
tlsctl topic create --request file://./create_topic.json
tlsctl topic modify --topic-id <tid> --request file://./modify_topic.json
tlsctl metric-topic create --request file://./create_metric_topic.json
tlsctl log search --request file://./search_logs.json
```

#### 5.1.3 Prom 命令的 file://

Prom 命令大量参数都支持 `file://`（例如 `--query/--time/--start/--end/--match/--label-name`）。

`--match file://...` 支持两种文件格式：

- JSON 数组：`["up","rate(up[5m])"]`
- 按行文本：每行一个 match（空行会忽略）

### 5.2 时间格式（from/to/start/end/time）

对外建议统一使用毫秒级 Unix 时间戳（最终会以毫秒精度参与检索计算）：

- Unix 时间戳（毫秒）：`1710374400000`
- RFC3339：`2026-03-14T00:00:00Z`
- 本地时间：`2026-03-14 00:00:00`
- 日期：`2026-03-14`

说明：实现上也兼容秒级 Unix 时间戳输入，但文档与示例不对外展示该格式。

### 5.3 输出：JSON / JSONL

全局 `--output`：

- `--output json`（默认）：适合“资源查询/管理”
- `--output jsonl`：适合“日志导出/流式处理”（每行一条 JSON）

示例：

```bash
tlsctl project list --output json
tlsctl --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000
```

### 5.4 输出过滤：--jmes-filter（轻量路径选择）

当前 `--jmes-filter` 支持轻量路径选择（例如 `a.b[0].c`），用于从 JSON 结果中选取局部字段。

示例：

```bash
tlsctl project list --jmes-filter "Projects[0].ProjectId"
```

### 5.5 错误结构与退出码（给自动化集成用）

当命令失败时：

- stdout 通常为空
- stderr 输出 JSON 错误结构（包含 `errorCode/errorMessage/requestId/statusCode`）
- 退出码非 0

当用户输入缺少子命令或 `-h/--help` 时：

- 输出为可读的用法文本（非 JSON）
- 退出码：help 为 0；缺参为 1

***

## 6. 命令参考（按功能分组）

你可以先记住一条规律：

- `tlsctl <group> -h`：看这个组有什么子命令和例子
- `tlsctl <group> <command> ...`：执行具体操作

### 6.1 全局用法

```bash
tlsctl [--profile <name>] [--output json|jsonl] [--jmes-filter <expr>] [--debug] <group> <command> [args]
```

全局选项：

- `--profile <name>`：指定 profile 名
- `--output json|jsonl`：输出格式
- `--jmes-filter <expr>`：输出过滤
- `--debug`：打印调试信息（如请求/响应细节）
- `--help/-h`：帮助
- `--version`：版本

### 6.2 configure（本地配置）

```bash
tlsctl configure -h
```

### 6.3 api（通用 OpenAPI 调用兜底）

```bash
tlsctl api -h
```

### 6.4 project（日志项目）

```bash
tlsctl project -h
```

### 6.5 topic（日志主题）

```bash
tlsctl topic -h
```

### 6.6 metric-topic（指标主题 + Prom 查询）

```bash
tlsctl metric-topic -h
tlsctl metric-topic prom -h
```

### 6.7 index（索引）

```bash
tlsctl index -h
```

### 6.8 log（日志检索/导出）

```bash
tlsctl log -h
```

### 6.9 ai（AI packs）

```bash
tlsctl ai -h
```

***

## 7. 逐命令参数手册（参数说明 + body 示例）

本章以“逐命令”的方式把每个参数讲清楚，并把 `file://` 与请求体（JSON）示例文件完整列出，方便你直接复制使用。

示例文件目录：
- [examples](./examples/README.md)

> 说明：示例 JSON 中的 `00000000-0000-0000-0000-000000000000` 只是占位符，你需要替换成真实的 `ProjectId/TopicId`。

### 7.1 全局参数（所有命令都可用）

命令通用格式：

```bash
tlsctl [--profile <name>] [--output json|jsonl] [--jmes-filter <expr>] [--debug] <group> <command> [args]
```

| 参数                     | 必填 | 说明                                                                | 示例                                      |
| ---------------------- | -: | ----------------------------------------------------------------- | --------------------------------------- |
| `--profile <name>`     |  否 | 使用指定 profile（未提供则使用 current profile 或 default；若环境变量 AK/SK 存在则被覆盖） | `--profile prod`                        |
| `--output json\|jsonl` |  否 | 输出格式。资源管理建议 `json`；日志导出建议 `jsonl`                                 | `--output jsonl`                        |
| `--jmes-filter <expr>` |  否 | 输出过滤（当前为轻量路径选择，如 `a.b[0].c`）                                      | `--jmes-filter "Projects[0].ProjectId"` |
| `--debug`              |  否 | 输出调试信息（用于排障）                                                      | `--debug`                               |
| `-h/--help`            |  否 | 帮助（对 group 也有效）                                                   | `tlsctl metric-topic -h`                |

### 7.2 configure（本地配置）

#### 7.2.1 configure set

用途：新增/更新一个 profile。

```bash
tlsctl configure set --profile <name> --ak <ak> --sk <sk> --region <region> [--endpoint <endpoint>] [--token <sts>] [--timeout-seconds <n>]
```

| 参数                  | 必填 | 说明                                                      | 示例                                             |
| ------------------- | -: | ------------------------------------------------------- | ---------------------------------------------- |
| `--profile`         |  是 | profile 名称                                              | `--profile default`                            |
| `--ak`              |  是 | AccessKeyID                                             | `--ak $VOLCENGINE_ACCESS_KEY_ID`               |
| `--sk`              |  是 | SecretAccessKey                                         | `--sk $VOLCENGINE_ACCESS_KEY_SECRET`           |
| `--token`           |  否 | STS Token                                               | `--token $VOLCENGINE_TOKEN`                    |
| `--region`          |  是 | 地域                                                      | `--region cn-beijing`                          |
| `--endpoint`        |  否 | 服务地址。省略时会由 region 推导为 `https://tls-<region>.volces.com` | `--endpoint https://tls-cn-beijing.volces.com` |
| `--timeout-seconds` |  否 | HTTP 超时秒数（默认 60）                                        | `--timeout-seconds 60`                         |

#### 7.2.2 configure use

用途：设置默认 profile（后续不写 `--profile` 时使用）。

```bash
tlsctl configure use <name>
```

#### 7.2.3 configure show

用途：查看 profile（AK 会脱敏显示）。

```bash
tlsctl configure show --profile <name>
```

### 7.3 api（通用 OpenAPI 调用兜底）

用途：当某个接口 CLI 还未封装时，用 `api call` 直接调用服务端 OpenAPI。

```bash
tlsctl api call --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...>]
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
tlsctl api call --method GET --path /DescribeProject --query ProjectId=<pid>
tlsctl api call --method POST --path /SearchLogs --body file://./search_logs.json
```

### 7.4 project（日志项目）

#### 7.4.1 project list

```bash
tlsctl project list [--page-number <n>] [--page-size <n>] [--project-name <s>] [--project-id <s>] [--fuzzy-search-key <s>] [--description <s>] [--is-full-name|--no-is-full-name] [--iam-project-name <s>] [--tags <s|file://...>] [--favourite|--no-favourite] [--topic-types <s>]
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

#### 7.4.2 project get

```bash
tlsctl project get --project-id <pid> [--topic-types <s>]
```

#### 7.4.3 project create

方式 A：用参数快速创建（Region 默认来自 profile/环境变量）

```bash
tlsctl project create --project-name <name> [--description <s>] [--iam-project-name <s>] [--region <region>] [--tags <json-array|file://...>] [--request <file://...>]
```

方式 B：用 `--request` 传入完整 JSON（推荐更“对齐 swagger”）

```bash
tlsctl project create --request file://./examples/create_project.json
```

`create_project.json` 示例（完整文件见 [create_project.json](./examples/create_project.json)）：

```json
{
  "ProjectName": "demo-project",
  "Description": "demo project created by tlsctl",
  "Region": "cn-beijing",
  "IamProjectName": "default",
  "Tags": [
    { "Key": "env", "Value": "dev" },
    { "Key": "owner", "Value": "alice" }
  ]
}
```

#### 7.4.4 project modify

```bash
tlsctl project modify --project-id <pid> [--project-name <name>] [--description <s>] [--favourite|--no-favourite] [--request <file://...>]
```

`modify_project.json` 示例（完整文件见 [modify_project.json](./examples/modify_project.json)）：

```json
{
  "ProjectId": "00000000-0000-0000-0000-000000000000",
  "Description": "updated description",
  "Favourite": true
}
```

#### 7.4.5 project delete

```bash
tlsctl project delete --project-id <pid>
```

### 7.5 topic（日志主题）

#### 7.5.1 topic list

```bash
tlsctl topic list [--page-number <n>] [--page-size <n>] [--cursor <s>] [--region <s>] [--project-id <pid>] [--project-name <s>] [--topic-name <s>] [--topic-id <s>] [--fuzzy-search-key <s>] [--description <s>] [--tags <s|file://...>] [--favourite|--no-favourite] [--order-by-project|--no-order-by-project]
```

重要限制：

- `--topic-name` 与 `--topic-id` 不能同时提供（服务端约束）

#### 7.5.2 topic get

```bash
tlsctl topic get --topic-id <tid>
```

#### 7.5.3 topic create

方式 A：命令行参数（适合只填核心字段）

```bash
tlsctl topic create --project-id <pid> --topic-name <name> --ttl <days> --shard-count <n> [--description <s>] [--auto-split] [--max-split-shard <n>] [--enable-tracking|--disable-tracking] [--metering-mode <s>] [--log-public-ip|--no-log-public-ip] [--enable-hot-ttl|--disable-hot-ttl] [--hot-ttl <days>] [--cold-ttl <days>] [--archive-ttl <days>] [--time-key <s>] [--time-format <s>] [--encrypt-conf <json|file://...>] [--tags <json-array|file://...>] [--request <file://...>]
```

方式 B：用 `--request`（强烈推荐）

```bash
tlsctl topic create --request file://./examples/create_topic.json
```

`create_topic.json` 示例（完整文件见 [create_topic.json](./examples/create_topic.json)）：

```json
{
  "ProjectId": "00000000-0000-0000-0000-000000000000",
  "TopicName": "demo-topic",
  "Description": "demo topic created by tlsctl",
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

#### 7.5.4 topic modify

```bash
tlsctl topic modify --topic-id <tid> [--topic-name <name>] [--description <s>] [--ttl <days>] [--auto-split|--no-auto-split] [--max-split-shard <n>] [--enable-tracking|--disable-tracking] [--favourite|--no-favourite] [--metering-mode <s>] [--log-public-ip|--no-log-public-ip] [--enable-hot-ttl|--disable-hot-ttl] [--hot-ttl <days>] [--cold-ttl <days>] [--archive-ttl <days>] [--time-key <s>] [--time-format <s>] [--encrypt-conf <json|file://...>] [--request <file://...>]
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

#### 7.5.5 topic delete

```bash
tlsctl topic delete --topic-id <tid>
```

### 7.6 metric-topic（指标主题 + 指标查询 + Prom 协议）

#### 7.6.1 metric-topic list/get/create/modify/delete

和 topic 基本一致，但资源为 MetricTopic（独立 API：CreateMetricTopic/ModifyMetricTopic/...）。

创建示例：

```bash
tlsctl metric-topic create --request file://./examples/create_metric_topic.json
```

`create_metric_topic.json` 示例（完整文件见 [create_metric_topic.json](./examples/create_metric_topic.json)）：

```json
{
  "ProjectId": "00000000-0000-0000-0000-000000000000",
  "TopicName": "demo-metric-topic",
  "Description": "metric topic created by tlsctl",
  "Ttl": 30,
  "ShardCount": 2,
  "AutoSplit": true,
  "MaxSplitShard": 10,
  "Tags": [
    { "Key": "env", "Value": "dev" }
  ]
}
```

#### 7.6.2 metric-topic search（用 SearchLogs 查询指标数据）

该命令本质调用 `/SearchLogs`，你可以把 `--query` 写成 SQL、PromQL、PromQL+SQL（取决于服务端能力与语法）。

```bash
tlsctl metric-topic search --topic-id <metric_tid> --query <expr> --from <t> --to <t> [--limit <n>] [--sort <asc|desc>] [--context <s>] [--highlight] [--accurate-query|--no-accurate-query] [--must-complete|--no-must-complete] [--offset <n>] [--request <file://...>]
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

#### 7.6.3 metric-topic prom（Prometheus HTTP API 兼容）

对应接口（GET/POST 均支持）：

- `/topic/{topic_id}/api/v1/query`
- `/topic/{topic_id}/api/v1/query_range`
- `/topic/{topic_id}/api/v1/series`
- `/topic/{topic_id}/api/v1/labels`
- `/topic/{topic_id}/api/v1/label/{label_name}/values`

```bash
tlsctl metric-topic prom <query|query-range|series|labels|label-values> [--method GET|POST] ...
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

### 7.7 index（索引）

#### 7.7.1 index get

```bash
tlsctl index get --topic-id <tid>
```

#### 7.7.2 index create / modify

```bash
tlsctl index create --topic-id <tid> --body file://./index.json
tlsctl index modify --topic-id <tid> --body file://./index.json
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

### 7.8 log（日志检索与导出）

#### 7.8.1 log search

```bash
tlsctl log search --topic-id <tid> --query <expr> --from <t> --to <t> [--limit <n>] [--context <s>] [--sort <asc|desc>] [--highlight] [--accurate-query|--no-accurate-query] [--must-complete|--no-must-complete] [--offset <n>] [--request <file://...>]
```

#### 7.8.2 log export

用途：自动翻页导出（内部基于 SearchLogs + Context）。

```bash
tlsctl --output jsonl log export --topic-id <tid> --query <expr> --from <t> --to <t> [--limit <n>] [--max-pages <n>] [--request <file://...>]
```

### 7.9 ai（AI packs）

#### 7.9.1 ai list-packs

```bash
tlsctl ai list-packs
```

#### 7.9.2 ai bootstrap

```bash
tlsctl ai bootstrap --pack <name> --project-id <pid> [--topic-name <s>]
```

#### 7.9.3 ai export

```bash
tlsctl --output jsonl ai export --pack <name> --project-id <pid> --from <t> --to <t> [--limit <n>]
```

***

## 8. 常见场景与示例

### 8.1 获取 ProjectId/TopicId

建议先执行列表查询：

```bash
tlsctl project list
tlsctl topic list --project-id <ProjectId>
tlsctl metric-topic list --project-id <ProjectId>
```

### 8.2 使用文件输入请求体，避免参数遗漏

```bash
tlsctl topic create --request file://./create_topic.json
tlsctl log search --request file://./search_logs.json
```

### 8.3 使用文件输入 PromQL（避免命令行过长）

```bash
tlsctl metric-topic prom query --topic-id <tid> --query file://./promql.txt --time 1710374400000
```

### 8.4 批量导出日志（供下游处理）

```bash
tlsctl --output jsonl log export --topic-id <tid> --query "*" --from 1710374400000 --to 1710378000000 --max-pages 100
```

***

## 9. 故障排查与 FAQ

### 9.1 “profile not found / missing region / missing endpoint”

- 先确认你是否配置了环境变量（会覆盖 profile）
- 或运行：

```bash
tlsctl configure show --profile default
```

### 9.2 “签名/鉴权失败（403/401）”

- 检查 AK/SK 是否正确
- 检查 region/endpoint 是否匹配（例如 cn-beijing 对应 tls-cn-beijing）
- 如果使用临时凭证，确认 `VOLCENGINE_TOKEN`（或 profile token）已配置

### 9.3 “服务端返回错误但我不知道怎么排查”

关注 stderr JSON 中的：

- `requestId`：提供给服务端排查非常关键
- `statusCode`：快速判断是鉴权/参数/限流/服务端错误

***
