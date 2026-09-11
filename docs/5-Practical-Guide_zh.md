# 5. 实用指南

[← 上一篇：使用](4-Usage_zh.md) | [English](5-Practical-Guide.md) | [下一篇：高级 →](6-Advanced_zh.md)

本指南使用默认的 `volclog` 命令面（`tool`、`workflow`、`raw`）演练三个端到端场景。关于安装、认证、配置和命令架构，请参阅 [入门](1-Getting-Started_zh.md)、[认证](2-Authentication_zh.md)、[配置](3-Configuration_zh.md) 和 [使用](4-Usage_zh.md)。

`volclog-human` 快捷方式是可选的交互式路径，需显式以 `volclog-human` 调用；下面的自动化导向流程不使用它们。人工终端用法见 [Human Shortcuts](7-Human-Shortcuts_zh.md)。

## 1. 接入新服务并验证日志可检索

### 目标

创建项目和主题，发送一条示例日志，并确认该日志可被检索。

### 前提条件

- 一个已配置的、有权限创建项目和主题、写入日志（`PutLogs`）以及检索日志（`SearchLogs`）的配置档。
- 配置档的 region 和 endpoint 已设置（见 [配置](3-Configuration_zh.md)）。

### 占位符

| 占位符 | 含义 |
| --- | --- |
| `<profile>` | 已配置的配置档名称 |
| `<project-name>` | 新项目的名称 |
| `<region>` | 项目的 TLS 地域 |
| `<project-id>` | 由 `project.create` 返回 |
| `<topic-name>` | 新主题的名称 |
| `<shard-count>` | 根据当前服务要求选择的整数 |
| `<ttl-days>` | 根据当前保留要求和服务约束选择的整数 |
| `<topic-id>` | 由 `topic.create` 返回 |
| `<marker>` | 示例日志的唯一字符串后缀 |
| `<from-ms>` / `<to-ms>` | 覆盖写入时间的 Unix 毫秒范围 |

用 `tool describe topic.create` 确认 `<shard-count>` 和 `<ttl-days>` 的约束；不要假设默认值或有效范围。

### 写之前先发现

检查你将调用的工具和工作流的契约，使请求体与当前模式匹配：

```bash
volclog --profile '<profile>' tool describe project.create
volclog --profile '<profile>' tool describe topic.create
volclog --profile '<profile>' workflow describe log.ingest
volclog --profile '<profile>' tool describe log.search
```

### 创建项目和主题

先用 `--dry-run` 在本地校验请求形状，然后再发送。预执行是本地校验门；它不检查远程是否存在、权限或 TLS 服务行为。

`project.create` 在体中要求 `ProjectName` 和 `Region`：

```bash
volclog --profile '<profile>' tool exec project.create \
  --input '{"ProjectName":"<project-name>","Region":"<region>","Description":"example project"}' \
  --dry-run

volclog --profile '<profile>' tool exec project.create \
  --input '{"ProjectName":"<project-name>","Region":"<region>","Description":"example project"}'
```

记下返回的 `ProjectId`。然后创建主题。`topic.create` 在体中要求 `ProjectId`、`TopicName`、`ShardCount` 和 `Ttl`：

```bash
volclog --profile '<profile>' tool exec topic.create \
  --input '{"ProjectId":"<project-id>","TopicName":"<topic-name>","ShardCount":<shard-count>,"Ttl":<ttl-days>}' \
  --dry-run

volclog --profile '<profile>' tool exec topic.create \
  --input '{"ProjectId":"<project-id>","TopicName":"<topic-name>","ShardCount":<shard-count>,"Ttl":<ttl-days>}'
```

记下返回的 `TopicId`。

### 发送示例日志并检索

使用 `log.ingest` 工作流从本地文件发送示例日志，而不是手动编写 `LogGroups` 请求体。用唯一的标记占位符创建一个简短的本地文件：

```bash
printf 'tls-pipeline-verify-<marker>\n' > sample.log
```

先运行工作流预执行。工作流预执行仅为本地校验；它不检查远程状态。然后不带预执行运行同一工作流：

```bash
volclog --profile '<profile>' workflow exec log.ingest \
  --input '{"TopicId":"<topic-id>","Input":"file://sample.log","InputFormat":"lines"}' \
  --context '{"execution":{"dry_run":true}}'

volclog --profile '<profile>' workflow exec log.ingest \
  --input '{"TopicId":"<topic-id>","Input":"file://sample.log","InputFormat":"lines"}'
```

等待几秒以便建立索引，然后用唯一的标记占位符检索。将 `<from-ms>` 和 `<to-ms>` 替换为覆盖发送时间的毫秒时间戳：

```bash
volclog --profile '<profile>' tool exec log.search \
  --input '{"TopicId":"<topic-id>","Query":"tls-pipeline-verify-<marker>","StartTime":<from-ms>,"EndTime":<to-ms>,"Limit":20}'
```

### 成功信号

- `project.create` 和 `topic.create` 返回 `status: success` 以及非空的 `ProjectId` / `TopicId`。
- `log.ingest` 返回 `status: success`。
- `log.search` 返回 `status: success`，且 `data` 中包含示例日志条目。
- 如果 `ResultStatus` 为 `incomplete`，服务只返回了部分扫描结果；请缩小时间范围后重新运行，再信任结果。

### 恢复检查点

- 如果 `project.create` 因权限错误失败，用 `volclog --profile '<profile>' doctor --online` 验证配置档的身份（这会检查配置档解析和最小的实时连通性；权限、配额和资源状态仍由服务端决定）。
- 如果 `log.search` 没有返回结果，确认 `TopicId`、时间范围以及是否已留出足够的索引时间；用 `tool describe log.search` 重新检查查询语法。
- 如果预执行失败，请先检查结构化错误。预执行不会发送业务 API 请求，但本地前置条件仍可能失败：
  - 契约形状或缺少字段错误：重新运行 `tool describe` / `workflow describe` 并对齐字段。
  - 配置档、凭证、endpoint 或 region 错误：查看 [配置](3-Configuration_zh.md) 并运行 `doctor`。
  - 本地输入或文件错误：检查路径是否存在且可读，以及输入格式是否与契约匹配。

## 2. 从告警转向导出证据以供分析

### 目标

获取告警中出现的查询，用有界检索验证后，将匹配的日志导出到文件以供离线分析。

### 前提条件

- 一个已配置的配置档。将 `<profile>`、`<topic-id>`、`<query>`、`<from-ms>`、`<to-ms>` 和 `<max-pages>` 替换为你的值。
- 一个可写的输出目录用于存放导出文件。

### 先用有界检索预览

运行 `tool describe log.search` 确认查询语法，然后运行有界的 `log.search` 并检查标准输出信封中的 `status`、`requestId` 和服务 `ResultStatus`：

```bash
volclog --profile '<profile>' tool describe log.search

volclog --profile '<profile>' tool exec log.search \
  --input '{"TopicId":"<topic-id>","Query":"<query>","StartTime":<from-ms>,"EndTime":<to-ms>,"Limit":20}'
```

如果 `ResultStatus` 为 `incomplete`，请在导出前缩小时间范围或查询；导出工作流不会在其输出中暴露 `ResultStatus`。

### 发现导出契约

当前仅有的工作流 ID 是 `log.export`、`log.export-analysis` 和 `log.ingest`。在创建请求 JSON 之前用 `workflow describe`，使字段与当前契约匹配：

```bash
volclog --profile '<profile>' workflow describe log.export
```

`log.export` 会自动翻页导出纯检索结果。当省略 `MaxPages` 时，它默认最多翻 100 页，且可能在所选/默认页数限制处停止而不暴露"仍有更多页"的信号。导出的数据工件只包含行，不会保留 `ResultStatus` 或最终的 `ListOver` 状态。因此，命令成功和文件存在只能证明那些行已被导出，不能证明证据集是完整的。对于证据级导出，请先用有界预览检查，当预览 `ResultStatus=incomplete` 时不要视为完整，缩小或拆分时间窗口或查询，并根据预期数据量显式选择 `MaxPages`。`MaxPages` 的选择由你负责。对于分析（SQL）查询，请改用 `log.export-analysis`；它不会自动翻页分析行，因此适用 SQL `limit`/`offset` 语义。

### 导出到文件

用显式的 `MaxPages` 强制 JSONL 导出到可写目录。`log.export` 将导出的行直接流式写入数据工件（对于 `--output jsonl` 为 JSONL 行）；该工件不是信封。标准输出只包含带有数据工件路径的固定文件通知：

```bash
volclog --profile '<profile>' --output jsonl --output-mode file --output-dir ./evidence \
  workflow exec log.export \
  --input '{"TopicId":"<topic-id>","Query":"<query>","StartTime":<from-ms>,"EndTime":<to-ms>,"MaxPages":<max-pages>}'
```

### 成功信号

- 标准输出打印 `结果已写入文件。\n文件: <path>\n`（固定的文件通知）。
- `<path>` 处的文件包含导出的日志行（JSONL），而不是信封。
- 导出工件不包含 `status`、`summary.deliveryMode`、`requestId` 或 `ResultStatus`。
- 成功只能证明导出的行已被写入；不能证明证据集是完整的。

### 恢复检查点

- 如果命令报错 `result too large for stdout`，说明输出未被强制为文件；请用 `--output-mode file --output-dir '<writable-dir>'` 重新运行。
- 如果 `workflow exec` 报告缺少必填字段，请重新运行 `workflow describe log.export` 并对齐请求体。
- 如果需要多页导出的传输请求 ID，请启用受限的追踪目录（`--trace-dir '<dir>'`）并在运行后检查追踪。追踪文件是敏感的：结构化字段保留头/查询键和体哈希，但传输 `error_message` 字段可能包含 URL 和查询值。永远不要将凭证放在查询参数中。

## 3. 诊断日志未到达的主机组/采集器流水线

### 目标

判断主机组及其采集器规则是否配置正确，以及主机是否在上报。

### 前提条件

- 一个已配置的配置档。将 `<profile>`、`<project-id>`、`<host-group-name>` 和 `<rule-name>` 替换为你的值。

### 发现契约

```bash
volclog --profile '<profile>' tool describe host-group.describe-host-groups-v2
volclog --profile '<profile>' tool describe host-group.describe-hosts
volclog --profile '<profile>' tool describe collector.describe-rules
```

### 列出并过滤主机组

`host-group.describe-host-groups-v2` 的 `supports_all=false`，且不接受 `ProjectId`。用 `HostGroupName` 过滤并请求显式的第一页。不要在此动作上使用 `--page-all`：

```bash
volclog --profile '<profile>' tool exec host-group.describe-host-groups-v2 \
  --input '{"HostGroupName":"<host-group-name>","PageNumber":1,"PageSize":20}'
```

`host-group.describe-host-groups-v2` 不接受 `ProjectId`，因此按名称过滤可能返回来自不同项目的同名组。在复制 `<host-group-id>` 之前，请检查每个返回项的项目和名称元数据，确认它属于预期的项目。如果存在歧义，请细化可用的过滤器或检查候选项；不要猜测 ID。检查返回的数据和当前的 `tool describe` 契约以获取确切的字段名。

### 检查主机

`host-group.describe-hosts` 要求 `HostGroupId`，且 `supports_all=true`。用 `--page-all` 获取所有页：

```bash
volclog --profile '<profile>' tool exec host-group.describe-hosts \
  --input '{"HostGroupId":"<host-group-id>"}' --page-all
```

### 检查采集器规则

`collector.describe-rules` 接受 `ProjectId` 和 `RuleName`，且 `supports_all=true`：

```bash
volclog --profile '<profile>' tool exec collector.describe-rules \
  --input '{"ProjectId":"<project-id>","RuleName":"<rule-name>"}' --page-all
```

### 成功信号

- `host-group.describe-host-groups-v2` 返回匹配的主机组；检查响应中的主机组标识符。
- `host-group.describe-hosts` 返回组中的主机；检查返回的数据和 `tool describe` 契约以获取确切的状态字段名。
- `collector.describe-rules` 返回匹配项目和规则名称的规则。

### 恢复检查点

- 如果未找到主机组，确认 `HostGroupName` 过滤器以及该组是否在预期项目中创建。
- 如果主机未上报，检查采集器规则的路径/主题绑定以及主机上采集器代理是否正在运行；用 `doctor --online` 确认配置档能到达 endpoint（这会检查连通性，不检查权限或资源状态）。
- 如果 `--page-all` 被拒绝，说明该动作的工具契约不支持它；用 `tool describe` 检查 `supports_all`，必要时手动翻页。

---

[← 上一篇：使用](4-Usage_zh.md) | [English](5-Practical-Guide.md) | [下一篇：高级 →](6-Advanced_zh.md)
