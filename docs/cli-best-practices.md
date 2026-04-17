# CLI 参数最佳实践与场景指南（volclog）

本篇聚焦 `volclog` 的全局参数、输出/runtime 语义、配置策略，以及 Agent/自动化优先的使用建议，适用于本地脚本、CI 与 `volclog-agent` / full 版共用的主链路。

说明：
- Agent 主路径默认是 `tool / workflow / raw`
- full 版的人类 shortcut 已下沉到独立文档：[cli-human-shortcuts.md](cli-human-shortcuts.md)
- 如果你想看链路化的 agent-first 实战指导，请先读：[cli-practical-guide.md](cli-practical-guide.md)

如果你不确定该怎么用，建议先按“新手最短路径”走一遍：

1) 先用 `doctor` 检查配置是否齐全  
2) Agent/自动化默认先用 `tool describe` / `workflow describe` 看契约，再执行  
3) 输出特别大就用 `--output-mode file --output-dir <writable-dir>`（stdout 只给固定提示与文件路径）  
4) 出错就加 `--trace-dir` 生成 trace 工件复盘  

---

## 1. 核心业务场景指南

`volclog` 涵盖了日志服务的绝大部分功能。以下示例优先展示 Agent/自动化更稳定的 `tool / workflow` 主路径；如果你是 full 版人工用户，需要更短的 shortcut 写法，请跳到 [cli-human-shortcuts.md](cli-human-shortcuts.md)。

### 1.1 资源管理 (Project & Topic)
**场景**：快速创建和查询日志项目及主题。

- **列出所有项目**：
  ```bash
  volclog tool exec project.describe-projects \
    --jmes-filter "data.Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
  ```
- **创建新项目**：
  ```bash
  volclog tool describe project.create
  volclog --dry-run tool exec project.create --input file://project_req.json
  volclog tool exec project.create --input file://project_req.json
  ```
- **列出某个项目下的所有日志主题**：
  ```bash
  volclog tool exec topic.describe-topics \
    --input '{"ProjectId":"<your-project-id>"}' \
    --jmes-filter "data.Topics[].{TopicId: TopicId, TopicName: TopicName}"
  ```
- **创建日志主题**：
  ```bash
  volclog tool describe topic.create
  volclog --dry-run tool exec topic.create --input file://topic_req.json
  volclog tool exec topic.create --input file://topic_req.json
  ```

### 1.2 索引管理 (Index)
**场景**：为日志主题开启或修改索引，支持全文索引与键值索引。

- **查看当前索引配置**：
  ```bash
  volclog tool describe index.describe
  volclog tool exec index.describe --input '{"TopicId":"<your-topic-id>"}'
  ```
- **配置索引（通过 JSON 文件传递复杂结构）**：
  *由于索引结构复杂（包含全文和键值规则），推荐先 `tool describe`，再用 JSON 文件执行。*
  ```bash
  volclog tool describe index.create --view full
  # 编辑 index_req.json 后执行
  volclog --dry-run tool exec index.create --input file://index_req.json
  volclog tool exec index.create --input file://index_req.json
  ```

### 1.3 日志检索与分析 (Log Search & Analysis)
**场景**：根据查询条件检索原始日志，或使用 SQL 进行聚合分析。

- **检索原始日志**：
  ```bash
  volclog tool describe log.search
  volclog tool exec log.search --input file://search_req.json
  ```
- **执行 SQL 聚合分析并提取分析结果**：
  ```bash
  volclog tool exec log.search \
    --input file://analysis_req.json \
    --jmes-filter "data.AnalysisResult.Data"
  ```

### 1.4 日志批量导出 (Log Export)
**场景**：将海量原始日志或分析结果导出至本地进行离线处理。

- **导出海量原始日志 (JSONL 格式流式落盘)**：
  ```bash
  volclog --output jsonl --output-mode file --output-dir ./out \
    workflow exec log.export --input file://export_req.json
  ```
- **导出 SQL 分析结果**：
  ```bash
  volclog --output jsonl --output-mode file --output-dir ./out \
    workflow exec log.export-analysis --input file://analysis_export_req.json
  ```

### 1.5 告警管理 (Alarm)
**场景**：配置告警策略组，实现日志监控和异常通知。

- **通过 tool 管理告警策略**：
  *由于告警配置高度定制化，建议先看 `tool describe` 约束，再执行。*
  ```bash
  # 探测契约
  volclog tool describe alarm.create-alarm
  # 预执行校验
  volclog --dry-run tool exec alarm.create-alarm --context file://ctx.json --input file://alarm_req.json
  # 正式执行
  volclog tool exec alarm.create-alarm --context file://ctx.json --input file://alarm_req.json
  ```

### 1.6 数据加工与消费 (ETL & Consumer Group)
**场景**：清洗富化日志数据或开启消费组分发。

- **管理数据加工任务 (ETL)**：
  ```bash
  volclog tool describe etl.create-rule
  ```
- **创建并查看消费组 (Consumer Group)**：
  ```bash
  volclog tool describe consumer-group.create-consumer-group
  ```

---

## 2. CLI 独有能力与高级参数组合

除了常规资源命令，`volclog` 还为 CLI 和自动化设计了许多高级特性，这些是平台 SDK 或控制台所不具备的。熟练掌握这些参数组合，可以大幅提升开发与运维效率。

### 2.1 Agent/自动化友好的元数据探索

对于不熟悉的接口，不要靠猜，利用 CLI 提供的自解释能力：

- **先看 `tool describe` / `workflow describe`**
  在调用复杂命令前，先看机器契约。shortcut 的 `--describe` 只属于 full 版人类增强层；Agent 主路径请优先用 `tool describe` 或 `workflow describe`。
  ```bash
  volclog workflow describe log.ingest
  ```

- **full 版人工用户仍可用 `--print-request-template` 生成骨架**
  当命令需要复杂的 JSON 请求体时，full 版 shortcut 仍支持一键生成；Agent 主路径则直接信任 `tool/workflow describe` 的契约。
  ```bash
  # 快捷写入优先看 ingest 的输入约束
  volclog log ingest --describe
  
  # 如果你已经准备好了原始 PutLogs 请求体，再看低层模板
  volclog log put --print-request-template=required > put_req.json
  volclog log put --print-request-template=full > put_full_req.json
  ```

- **--dry-run 本地校验载荷**
  这是最安全的参数！通过 `tool exec` 的 `ctx.json` 中 `execution.dry_run` 先拦截真实网络请求，仅校验参数是否完备、JSON 是否合法。
  ```bash
  volclog tool exec log.put-logs --context file://ctx.json --input file://put_req.json
  ```

### 2.2 多种请求载荷输入方式（--input / --context）

针对 `tool exec` / `workflow exec` 的 JSON 载荷，CLI 提供了灵活的输入支持：

- **通过 Inline 字符串直接输入**：
  ```bash
  volclog tool exec project.create --input '{"ProjectName":"test","Region":"cn-beijing"}'
  ```
- **通过本地文件输入 (安全可靠)**：
  ```bash
  volclog tool exec project.create --input file://./create_req.json
  ```
- **通过标准输入流式传递 (适合管道拼接)**：
  ```bash
  cat create_req.json | volclog tool exec project.create --input -
  ```

### 2.3 异常排查组合 (--trace-dir + --trace-redact)

遇到非预期的 `400` 或 `500` 错误，想查看完整的请求响应包但又不想泄露敏感的 `AK/SK`：

```bash
volclog --trace-dir ./.volclog/traces --trace-redact strict project create --request file://create_req.json
```
执行后，目录下会生成详细的 trace 日志。`--trace-redact strict` 确保所有的 Authorization、Token 都被 `[REDACTED]` 替换，可以安全地发给支持团队。

---

## 3. 全局参数作用域与语法边界

`volclog` 的全局参数支持任意位置解析，但脚本和文档最好仍保持一致写法：

```bash
volclog [global flags...] <group> <command> [args]
```

推荐：
```bash
volclog --output json raw --method GET --path /DescribeProjects --query PageSize=1
```
建议：为了减少歧义，文档、脚本和 Agent 最好仍统一采用前置写法。

### 3.1 全局参数一图理解

全局参数控制“这次命令的执行方式”，不改变业务语义：
- 选用哪个身份/环境：`--profile`、`--secrets-file`
- 输出怎么打印/怎么落盘：`--output`、`--output-mode`、`--output-dir`、`--jmes-filter`
- 出问题怎么排障：`doctor`、`--trace-dir`、`--trace-redact`

业务语义参数属于具体命令，例如 `project list --page-size 10`、`log search --topic-id ...`。

---

## 4. 输出相关（--output / --output-mode / --output-dir）

### 4.1 --output

- `--output table`：人类友好格式，仅支持部分常用 list/get 及 search。
- `--output json`：默认，适合资源管理类命令（project/topic/index/metric-topic）。
- `--output jsonl`：每行一条 JSON，适合日志导出/大结果集（便于流式处理）。

示例：
```bash
volclog project list --output json
volclog log export --output jsonl --topic-id <tid> --query "*" --from <ms> --to <ms>
```

### 4.1.1 新手怎么选 json/jsonl
- 不确定：先用 `json`
- 你要“导出很多行日志/指标点”：用 `jsonl`
- 你要“给脚本/系统读取一个结构化对象”：用 `json`

### 4.1.2 SearchLogs：Logs 模式 vs Analysis 模式

`/SearchLogs` 响应里通常会包含两类结果：
- `Logs`: 原始日志列表（数组）
- `AnalysisResult`: SQL/聚合类查询的表格结果（包含 `Schema` 和 `Data`）

使用时先分清三点：
- `volclog log search`：返回的是“完整响应对象”。
- `volclog log export`：用于导出“仅检索”结果（`Logs`）。
- `volclog log export-analysis`：用于导出 SQL/Analysis 结果，直接逐行输出 `AnalysisResult.Data`。

### 4.2 --output-mode 与 --output-dir

当输出很大或 CI/Agent 需要控制 stdout 体积时，推荐：
```bash
volclog --output-mode file --output-dir ./out \
  workflow exec log.export --input file://export_req.json
```
此时 stdout 只输出固定提示与文件路径；完整 envelope 会被写到 `output_dir` 下由 CLI 自动生成的文件中。

提供可写目录：
```bash
volclog --output-mode file --output-dir ./out tool exec project.describe-projects
```

说明：
- 不再要求 Agent 自己决定文件名，CLI 会在 `output_dir` 下自动生成文件
- `--jmes-filter` 与 file 交付不能同时使用；要么筛选 stdout，要么直接落完整结果文件

---

## 5. 过滤与提取（--jmes-filter）

用于脚本中提取字段（作用于完整 CLI envelope）：

```bash
volclog tool exec project.describe-projects --jmes-filter "data.Projects[0].ProjectId"
volclog tool exec project.describe-projects --jmes-filter "summary.deliveryMode"
volclog tool exec project.describe-projects --jmes-filter "error"
```

如果你需要复杂筛选/排序，建议直接输出 JSON/JSONL 后用 `jq` 处理。

补充说明：
- `--jmes-filter` 命中存在但值为 `null` 的字段时，stdout 直接输出 `null`，不视为失败。
- 真正的失败仍会返回失败 envelope；此时优先看 `error.kind`、`error.code`、`error.message`、`error.details`。
- 对服务端错误，`error.code` 直接是业务错误码，例如 `ProjectAlreadyExist`；如果服务端在错误文案里又嵌了一层 JSON，CLI 会尽量提炼到 `error.details`。

### 5.1 失败结果怎么读

失败时统一读取单层 `error` 对象：

- `error.source`：`cli` 或 `upstream`
- `error.kind`：`validation / usage / server / decode / ...`
- `error.code`：CLI 错误码或上游业务错误码
- `error.message`：主错误文案
- `error.requestId` / `error.statusCode`：排障时最有用
- `error.details`：只有确实存在额外结构时才出现

典型用法：

```bash
volclog --jmes-filter "error.code" tool exec project.create --input '{"ProjectName":"demo","Region":"cn-beijing"}'
volclog --jmes-filter "error.details" raw --method POST --path /CreateTopic --data '{}'
```

---

## 6. 诊断与复盘（--trace-dir / --trace-redact / doctor）

排障推荐流程：

```bash
volclog doctor
volclog --trace-dir ./.volclog/traces log search --topic-id <tid> --query "*" --from <ms> --to <ms>
```

- `--trace-dir`：生成脱敏 trace 工件（JSONL），输出中附带 `meta.trace.path`。
- `--trace-redact strict|default`：默认 `strict`；`default` 信息更丰富但仍会脱敏。

---

## 7. 密钥注入与多账号隔离

### 7.1 环境变量隔离 (--secrets-file)

`--secrets-file` 会从 dotenv 文件加载环境变量，适合 CI/CD 和多账号隔离执行：
```bash
volclog --secrets-file ./.env tool exec project.describe-projects
```

dotenv 示例：
```bash
VOLCENGINE_ACCESS_KEY_ID=xxx
VOLCENGINE_ACCESS_KEY_SECRET=yyy
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
```

### 7.2 Profile + cred-ref 模式

当同一 AK/SK 需要跨多个 region/endpoint 复用时：

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
