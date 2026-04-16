# CLI 参数最佳实践与场景指南（volclog）

本篇聚焦 `volclog` 的全局参数、配置策略、自动化最佳实践，以及各类核心业务场景的使用指南，适用于本地交互、脚本批处理与 CI/Agent 场景。

如果你不确定该怎么用，建议先按“新手最短路径”走一遍：

1) 先用 `doctor` 检查配置是否齐全  
2) 资源管理用 `--output json` 或 `--output table`（人类友好），日志导出用 `--output jsonl`  
3) 输出特别大就用 `--output-mode file`（stdout 只给路径）  
4) 出错就加 `--trace-dir` 生成 trace 工件复盘  

---

## 1. 核心业务场景指南

`volclog` 涵盖了日志服务的绝大部分功能，以下列出各核心场景下的使用建议和常用命令。

### 1.1 资源管理 (Project & Topic)
**场景**：快速创建和查询日志项目及主题。

- **列出所有项目**：
  ```bash
  volclog project list --output table
  ```
- **创建新项目**：
  ```bash
  volclog project create --project-name "my-new-project" --description "for testing"
  ```
- **列出某个项目下的所有日志主题**：
  ```bash
  volclog topic list --project-id <your-project-id> --output table
  ```
- **创建日志主题**：
  ```bash
  volclog topic create --project-id <your-project-id> --topic-name "nginx-logs" --ttl 30
  ```

### 1.2 索引管理 (Index)
**场景**：为日志主题开启或修改索引，支持全文索引与键值索引。

- **查看当前索引配置**：
  ```bash
  volclog index get --topic-id <your-topic-id>
  ```
- **配置索引（通过 JSON 文件传递复杂结构）**：
  *由于索引结构复杂（包含全文和键值规则），推荐使用 JSON 文件。*
  ```bash
  volclog index create --print-request-template=full > index_req.json
  # 编辑 index_req.json 后执行
  volclog index create --request file://index_req.json
  ```

### 1.3 日志检索与分析 (Log Search & Analysis)
**场景**：根据查询条件检索原始日志，或使用 SQL 进行聚合分析。

- **检索原始日志**：
  ```bash
  volclog log search --topic-id <your-topic-id> --query "error" --from <start-time-ms> --to <end-time-ms> --limit 100
  ```
- **执行 SQL 聚合分析并提取分析结果**：
  ```bash
  volclog log search --topic-id <your-topic-id> --query "* | select count(*) as total, status group by status" --from <start-time-ms> --to <end-time-ms> --jmes-filter "AnalysisResult.Data"
  ```

### 1.4 日志批量导出 (Log Export)
**场景**：将海量原始日志或分析结果导出至本地进行离线处理。

- **导出海量原始日志 (JSONL 格式流式落盘)**：
  ```bash
  volclog --output-mode file --output-file ./export.jsonl log export --topic-id <your-topic-id> --query "*" --from <start-time-ms> --to <end-time-ms>
  ```
- **导出 SQL 分析结果**：
  ```bash
  volclog --output-mode file --output-file ./analysis.jsonl log export-analysis --topic-id <your-topic-id> --query "* | select status, count(*) group by status" --from <start-time-ms> --to <end-time-ms>
  ```

### 1.5 告警管理 (Alarm)
**场景**：配置告警策略组，实现日志监控和异常通知。

- **通过 tool/shortcut 管理告警策略**：
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

- **--describe 探测命令约束**
  在调用复杂命令前，先看 `--describe`。shortcut 的 `--describe` 输出当前命令约束；公开机器契约请改用 `tool describe` 或 `workflow describe`。
  ```bash
  volclog log ingest --describe
  ```

- **--print-request-template 生成请求骨架**
  当命令需要复杂的 JSON 请求体时，CLI 支持一键生成。
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

### 2.2 多种请求载荷输入方式 (--request)

针对包含 JSON Body 的请求，CLI 提供了极度灵活的输入支持：

- **通过 Inline 字符串直接输入**：
  ```bash
  volclog project create --request '{"ProjectName":"test", "Region": "cn-beijing"}'
  ```
- **通过本地文件输入 (安全可靠)**：
  ```bash
  volclog project create --request file://./create_req.json
  ```
- **通过标准输入流式传递 (适合管道拼接)**：
  ```bash
  cat create_req.json | volclog project create --request -
  ```

### 2.3 异常排查组合 (--trace-dir + --trace-redact)

遇到非预期的 `400` 或 `500` 错误，想查看完整的请求响应包但又不想泄露敏感的 `AK/SK`：

```bash
volclog --trace-dir ./.volclog/traces --trace-redact strict project create --request file://create_req.json
```
执行后，目录下会生成详细的 trace 日志。`--trace-redact strict` 确保所有的 Authorization、Token 都被 `[REDACTED]` 替换，可以安全地发给支持团队。

---

## 3. 全局参数作用域与语法边界

`volclog` 的全局参数默认仍建议写在 group 之前：

```bash
volclog [global flags...] <group> <command> [args]
```

推荐：
```bash
volclog --output json raw --method GET --path /DescribeProjects --query PageSize=1
```

也支持的后置写法（仅限 `raw` 和部分 shortcut 的输出类全局参数）：
```bash
volclog raw --output json --method GET --path /DescribeProjects
```

建议：为了减少歧义，文档、脚本和 Agent 默认仍统一采用前置写法。

### 3.1 全局参数一图理解

全局参数控制“这次命令的执行方式”，不改变业务语义：
- 选用哪个身份/环境：`--profile`、`--secrets-file`
- 输出怎么打印/怎么落盘：`--output`、`--output-mode`、`--output-file`、`--jmes-filter`
- 出问题怎么排障：`doctor`、`--trace-dir`、`--trace-redact`

业务语义参数属于具体命令，例如 `project list --page-size 10`、`log search --topic-id ...`。

---

## 4. 输出相关（--output / --output-mode / --output-file）

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

### 4.2 --output-mode 与 --output-file

当输出很大或 CI/Agent 需要控制 stdout 体积时，推荐：
```bash
volclog --output-mode file log export --topic-id <tid> --query "*" --from <ms> --to <ms>
```
此时 stdout 只输出文件路径，便于下一步读取。

固定输出路径：
```bash
volclog --output-mode file --output-file ./out.json project list
```

---

## 5. 过滤与提取（--jmes-filter）

用于脚本中提取字段（作用于原始响应结构）：

```bash
volclog project list --jmes-filter "Projects[0].ProjectId"
volclog topic list --project-id <pid> --jmes-filter "Topics[0].TopicId"
```

如果你需要复杂筛选/排序，建议直接输出 JSON/JSONL 后用 `jq` 处理。

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
volclog --secrets-file ./.env project list
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
