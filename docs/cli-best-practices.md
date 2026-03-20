# CLI 参数最佳实践（volclog）

本篇聚焦 `volclog` 的全局参数、配置策略与自动化最佳实践，适用于本地交互、脚本批处理与 CI/Agent 场景。

如果你不确定该怎么用，建议先按“新手最短路径”走一遍：

1) 先用 `doctor` 检查配置是否齐全  
2) 资源管理用 `--output json`，日志导出用 `--output jsonl`  
3) 输出特别大就用 `--output-mode file`（stdout 只给路径）  
4) 出错就加 `--trace-dir` 生成 trace 工件复盘  

## 1. 参数作用域与语法边界

`volclog` 的全局参数只支持写在 group 之前：

```bash
volclog [global flags...] <group> <command> [args]
```

推荐：

```bash
volclog --output json api call --method GET --path /DescribeProjects --query PageSize=1
```

不推荐（不支持）：

```bash
volclog api --output json call --method GET --path /DescribeProjects
```

## 1.1 全局参数一图理解

全局参数控制“这次命令的执行方式”，不改变业务语义：
- 选用哪个身份/环境：`--profile`、`--secrets-file`
- 输出怎么打印/怎么落盘：`--output`、`--output-mode`、`--output-file`、`--jmes-filter`
- 出问题怎么排：`--trace-dir`、`--trace-redact`、`--debug`

业务语义参数属于具体命令，例如 `project list --page-size 10`、`log search --topic-id ...`。

## 2. 输出相关（--output / --output-mode / --output-file）

### 2.1 --output

- `--output json`：默认，适合资源管理类命令（project/topic/index/metric-topic）。
- `--output jsonl`：每行一条 JSON，适合日志导出/大结果集（便于流式处理）。

示例：

```bash
volclog project list --output json
volclog log export --output jsonl --topic-id <tid> --query "*" --from <ms> --to <ms>
```

### 2.1.1 新手怎么选 json/jsonl

- 不确定：先用 `json`
- 你要“导出很多行日志/指标点”：用 `jsonl`
- 你要“给脚本/系统读取一个结构化对象”：用 `json`

常见组合：

```bash
volclog --output json project list
volclog --output json topic list --project-id <pid>
volclog --output jsonl log export --topic-id <tid> --query "*" --from <ms> --to <ms>
```

### 2.1.2 SearchLogs：Logs 模式 vs Analysis 模式

`/SearchLogs` 响应里通常会包含两类结果：
- `Logs`: 原始日志列表（数组）
- `AnalysisResult`: SQL/聚合类查询的表格结果（包含 `Schema` 和 `Data`）

使用时先分清三点：
- `volclog log search`：返回的是“完整响应对象”（包含 `Context/ListOver/Count/Logs/AnalysisResult/...`），即使你用 `--output jsonl` 也只会变成“一行一个完整响应 JSON”。
- `volclog log export`：用于导出“仅检索”结果（`Logs`）；如果你跑的是 SQL/Analysis 类型查询（结果主要在 `AnalysisResult`），请使用 `log export-analysis`。
- `volclog log export-analysis`：用于导出 SQL/Analysis 结果，直接逐行输出 `AnalysisResult.Data`（每行一个对象），默认输出为 JSONL（每行一个对象）。SQL 的翻页通过 `offset/limit` 由用户在 `--query` 中控制。

注意（与官方 SearchLogs 语义一致）：
- 仅检索（Query 没有 `|`）时，`Limit/Context/Sort/Offset` 生效；可以用 `Context` 继续查询后续结果。
- 检索+分析（Query 包含 `|`）时，不支持 Context 分页；请求体里的 `Limit/Context/Sort` 不生效（limit/offset 由 SQL 语句控制）。

#### 2.1.2.1 备用/高级用法：用 jq 导出 JSONL

当你不想使用 `log export-analysis`（或在老版本里还没有该命令）时，可以用 `log search` + `jq` 直接把响应转换为 JSONL。

如果你要把 `log search` 的日志逐行输出（JSONL），推荐用 `jq`：

```bash
volclog --output json log search ... | jq -c '.Logs[]'
```

如果你跑的是 SQL/Analysis 查询（服务端 `AnalysisResult.Data` 为带列名对象数组），可直接逐行输出：

```bash
volclog --output json log search ... | jq -c '.AnalysisResult.Data[]'
```

### 2.2 --output-mode 与 --output-file

当输出很大或 CI/Agent 需要控制 stdout 体积时，推荐：

```bash
volclog --output-mode file log export --topic-id <tid> --query "*" --from <ms> --to <ms>
```

此时 stdout 只输出文件路径，便于下一步读取。

固定输出路径：

```bash
volclog --output-mode file --output-file ./out.json project list
```

### 2.2.1 什么时候一定要用 --output-mode file

- 你在 CI/Agent 里跑命令，stdout 太大容易被截断/刷屏
- 你在导出日志（`log export`）或查询返回可能很大
- 你希望“固定输出路径”给下一步任务读取

示例：

```bash
volclog --output jsonl --output-mode file --output-file ./export.jsonl \
  log export --topic-id <tid> --query "*" --from <ms> --to <ms>
```

## 3. 过滤与提取（--jmes-filter）

用于脚本中提取字段（当前为轻量路径选择，不是完整 JMESPath）：

```bash
volclog project list --jmes-filter "Projects[0].ProjectId"
```

### 3.1 新手常用提取写法

```bash
volclog project list --jmes-filter "Projects[0].ProjectId"
volclog topic list --project-id <pid> --jmes-filter "Topics[0].TopicId"
```

如果你需要复杂筛选/排序，建议直接输出 JSON/JSONL 后用 `jq` 处理。

## 4. 诊断与复盘（--trace-dir / --trace-redact / doctor）

排障推荐流程：

```bash
volclog doctor
volclog --trace-dir ./.volclog/traces log search --topic-id <tid> --query "*" --from <ms> --to <ms>
```

- `--trace-dir`：生成脱敏 trace 工件（JSONL），输出中附带 `meta.trace.path`。
- `--trace-redact strict|default`：默认 `strict`；`default` 信息更丰富但仍会脱敏。

### 4.1 新手排障套路（建议收藏）

1) 先检查当前配置是否齐全：

```bash
volclog doctor
```

2) 失败后加 trace：

```bash
volclog --trace-dir ./.volclog/traces <group> <command> ...
```

3) 如果你要把 trace 发给别人看，默认 `strict` 更安全：

```bash
volclog --trace-dir ./.volclog/traces --trace-redact strict <group> <command> ...
```

## 5. 密钥注入与隔离（--secrets-file）

`--secrets-file` 会从 dotenv 文件加载环境变量（不会覆盖已存在 env），适合：
- CI/CD
- 多账号隔离执行
- 不希望落盘 `~/.volclog/config.json` 的场景

示例：

```bash
volclog --secrets-file ./.env project list
```

dotenv 示例（按需）：

```bash
VOLCENGINE_ACCESS_KEY_ID=xxx
VOLCENGINE_ACCESS_KEY_SECRET=yyy
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
# 可选：VOLCENGINE_REGION=cn-beijing
# 可选：VOLCENGINE_TOKEN=...
```

注意：`--secrets-file` 是全局参数，不是 `configure` 子命令参数；对 `configure set/use/show/list/delete` 通常没有意义（这些命令主要读写本地配置文件）。

### 5.1 新手应该选 env / profile / secrets-file 哪个

- 本地长期使用：`configure set/use`（profile）
- CI/容器：环境变量或 `--secrets-file`
- 多账号隔离且不想改全局 env：用 `--secrets-file` 分开不同 `.env.*`

示例（同一机器跑两套账号）：

```bash
volclog --secrets-file ./.env.tenant-a project list
volclog --secrets-file ./.env.tenant-b project list
```

注意：如果当前 shell 已经设置了同名环境变量，dotenv 中的同名值不会覆盖它。

## 7. Debug（--debug）

`--debug` 用于打印更多调试信息（适合本地排障），但不建议在共享日志环境长期开启。

## 6. 多账号/多租户配置主路径（Profile + cred-ref）

### 6.1 为什么推荐 cred-ref

当同一 AK/SK 需要跨多个 region/endpoint 复用时：
- AK/SK 只写一次到用户级 `creds`
- 多个 profile 引用同一 `cred_ref`

首次创建并绑定：

```bash
volclog configure set --profile demo-bj --cred-ref demo-root --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
```

复用创建第二个 profile：

```bash
volclog configure set --profile demo-sg --cred-ref demo-root --endpoint https://tls-ap-singapore-1.volces.com
```

切换默认 profile：

```bash
volclog configure use demo-bj
```

后续不带 `--profile` 的命令会使用当前 profile（但若同时存在 `VOLCENGINE_ACCESS_KEY_ID/SECRET`，环境变量优先）。
