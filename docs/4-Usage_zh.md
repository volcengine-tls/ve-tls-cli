# 4. 使用

[← 上一篇：配置](3-Configuration_zh.md) | [English](4-Usage.md) | [下一篇：实用指南 →](5-Practical-Guide_zh.md)

本指南介绍如何用 `volclog` 运行命令：命令面、如何发现工具和工作流、`tool exec`、`workflow exec` 和 `raw` 的工作方式、上下文和选择器如何交互，以及输出、过滤和追踪的行为。关于认证和配置的细节，请参阅 [认证](2-Authentication_zh.md) 和 [配置](3-Configuration_zh.md)。

## 1. 命令架构与版本边界

默认的 `volclog` 命令暴露以下组：`configure`、`doctor`、`skill`、`tool`、`workflow`、`raw`、`login`、`logout` 和 `sso`。

`volclog-human` 在 `tool`/`workflow`/`raw` 之外添加了人类快捷方式层（`project`、`topic`、`metric-topic`、`index`、`log`、`host-group`、`collector`）。快捷方式复用共享的认证、传输、信封和追踪基础设施，但有自己的快捷方式参数解析和请求构建。契约校验、表格支持和文件/标准输出行为可能与 `tool`/`workflow`/`raw` 不同。用户和自动化必须遵循所选命令面的文档化契约。

选择合适的入口：

- 当你知道 API 动作并想要契约校验和结构化信封时，使用 `tool`。
- 当你想要 CLI 提供的、对一个或多个工具的高层编排时，使用 `workflow`。
- 仅当你已经知道确切的方法和路径并需要直接传输调用时，使用 `raw`。
- `volclog-human` 快捷方式仅用于交互式终端工作；Agent 和 CI 应默认使用 `tool`/`workflow`/`raw`。

## 2. 执行前先发现

在运行工具或工作流之前，先检查其契约，以便了解所需的输入和请求形状。

### 2.1 `tool list` 和 `tool describe`

```bash
volclog tool list
volclog tool list project
volclog tool list --verb create --format json
volclog tool describe project.describe-projects
volclog tool describe project.create --view full
```

`tool list` 将组作为位置参数（不是 `--group` 标志），并支持 `--verb` 按动词过滤和 `--format text|json`（默认 `text`）。`tool describe <group.action>` 显示工具的标识、输入模式、上下文模式、执行模式、行为、输出策略和契约摘要。`--view` 标志选择 `compact`（默认）或 `full`；显式 JSON 输出默认为 `full`。

### 2.2 `workflow list` 和 `workflow describe`

```bash
volclog workflow list
volclog workflow list log
volclog workflow list --format json
volclog workflow describe log.export
```

`workflow list` 将组作为位置参数，并支持 `--format text|json`。`workflow describe <group.command>` 显示工作流的类型、来源、输入模式、上下文模式、执行模式、推荐的全局标志和指导。

## 3. `tool exec`

`tool exec` 按 `<group.action>` 标识运行单个工具。

```bash
volclog --profile default tool exec project.describe-projects
```

### 3.1 输入与上下文

`--input` 提供请求体。它接受 `file://<path>`、`-`（标准输入）或内联 JSON 对象（必须以 `{` 开头）。`--context` 使用相同的来源提供运行时和执行控制。

```bash
volclog --profile default tool exec project.create \
  --input '{"ProjectName":"my-project","Region":"cn-beijing","Description":"example"}'

volclog --profile default tool exec project.create \
  --input file://req.json \
  --context '{"execution":{"dry_run":true}}'
```

`file://req.json` 文件必须包含必填的 `ProjectName` 和 `Region` 字段。

### 3.2 契约校验与预执行

在任何网络调用之前，`tool exec` 将扁平输入规范化为正确的 `query`/`path`/`header`/`body` 段（当契约定义了输入模式时），并校验必填字段。缺少必填字段会在发送请求前失败。

`--dry-run` 运行相同的契约校验，然后跳过网络调用并返回一个本地计划，该计划检查配置档、endpoint、region 和请求体 JSON 的有效性：

```bash
volclog --profile default tool exec project.create \
  --input '{"ProjectName":"my-project","Region":"cn-beijing"}' --dry-run
```

`--page-all` 是 `tool exec` 的直接标志，用于请求分页结果的所有页。

## 4. `workflow exec`

`workflow exec` 按 `<group.command>` 标识运行 CLI 提供的高层工作流。工作流用编排逻辑封装一个或多个工具；公共 API 工具面仍可通过 `tool exec` 使用。

```bash
volclog --profile default workflow exec log.export \
  --input file://export.json
```

### 4.1 输入与上下文

`--input` 和 `--context` 的行为与 `tool exec` 完全相同：`file://<path>`、`-`（标准输入）或内联 JSON 对象。工作流的必填参数在执行前会被校验。

### 4.2 执行控制

`workflow exec` 不接受 `--artifact`、`--projection` 或 `--page-all` 作为直接 CLI 标志。它从 `--context` JSON 的 `execution` 对象中消费的控制是：

- `execution.dry_run` — 跳过网络调用
- `execution.artifact` — 强制文件交付
- `execution.projection` — 在构建信封之前用 JMESPath 表达式投影原始结果

虽然上下文解析器能识别 `execution.page.all` / `execution.page_all`，但当前工作流执行不会消费它们。分页语义是工作流专属的；请遵循 `workflow describe` 中针对你所使用工作流的指导。不要依赖工作流的 page-all 上下文控制。

```bash
volclog --profile default workflow exec log.export \
  --input file://export.json \
  --context '{"execution":{"dry_run":true}}'
```

在运行工作流之前，请使用 `workflow list` 或 `workflow describe` 确认该工作流 ID 在当前安装中可用。

## 5. `raw`

仅当你已经知道确切的方法和路径时才使用 `raw`。它进行直接传输调用，不进行契约校验。

```bash
volclog --profile default raw --method GET --path /DescribeProjects
```

### 5.1 方法、路径、查询、头、体

| 标志 | 行为 |
| --- | --- |
| `--method` | HTTP 方法；默认 `GET`，转为大写 |
| `--path` | 必填；如果缺少 `/` 则自动补上 |
| `--query k=v` | 可重复的查询参数 |
| `--header k=v` | 可重复的头 |
| `--body` / `--input` | 请求体；`--input` 是 `--body` 的兼容别名 |
| `--request-format` | 请求体格式；默认 `json` |

`--body` 和 `--input` 互斥；同时使用两者会失败并报错 `conflicting body selectors`。请求体值接受内联 JSON、`-`（标准输入）、`file://<path>`、裸文件路径或纯字符串。空请求体变为 `{}`。

### 5.2 预执行

`raw` 的 `--dry-run` 仅校验传输和本地形状（配置档解析、endpoint、region、请求体 JSON 有效性）。它**不**校验工具或工作流 API 的必填字段，因为 `raw` 没有契约。不会进行网络调用。

```bash
volclog --profile default raw --method POST --path /CreateProject \
  --body '{"ProjectName":"my-project"}' --dry-run
```

## 6. 上下文、选择器与预执行

### 6.1 全局与上下文选择器

显式身份选择器是可选的。如果没有选择器，则应用正常的配置档选择或静态环境解析。

身份选择器是：全局 `--profile`、全局 `--secrets-file`、`context.profile` 和 `context.secrets_file`。冲突规则如下：

- 任何配置档选择器（全局 `--profile` 或 `context.profile`）与任何密钥文件选择器（全局 `--secrets-file` 或 `context.secrets_file`）组合都会冲突。
- 全局 `--secrets-file` 与 `context.secrets_file` 组合会冲突。
- 全局 `--profile` 与 `context.profile` 仅在配置档名称不同时才冲突。在两处重复相同的配置档名称是可接受的，但属于冗余；为清晰起见应避免这样做。

冲突会失败并报错 `conflicting runtime selectors`（当提供两个不同的配置档名称时为 `conflicting profile selectors`）。

没有全局 CLI `--region` 或 `--endpoint` 标志。`context.region` 和 `context.endpoint` 是仅可通过 `tool`/`workflow` 上下文使用的每次执行回退默认值（不是身份选择器）；它们优先于项目默认值，但不会覆盖非空的所选配置档值或动态环境值。完整的 region/endpoint/timeout 优先级在 [配置](3-Configuration_zh.md) 中有详细说明。

### 6.2 `--dry-run` 与上下文预执行

`--dry-run` 是一个全局标志，适用于 `raw`、`tool exec` 和 `workflow exec`。对于 `tool exec` 和 `workflow exec`，`--context` JSON 中的 `execution.dry_run` 具有相同效果。`--dry-run` 对其他组是被拒绝的。

### 6.3 写操作安全

对于写操作，先用 `--dry-run` 运行以在本地校验请求，然后再发送到 TLS。预执行计划会报告本地检查（配置档、endpoint、region、请求体 JSON）是否通过。

关于各认证方式的登录、刷新和登出行为，请参阅 [认证](2-Authentication_zh.md)。关于配置档和运行时优先级，请参阅 [配置](3-Configuration_zh.md)。

## 7. 输出与交付

### 7.1 输出值

`--output` 在默认的 `volclog` agent 路径上接受 `json`（默认）和 `jsonl`。`table` 在默认路径上会被拒绝；它仅由 `volclog-human` 支持，且仅用于特定的快捷方式入口（`project`/`topic`/`metric-topic` 的 list 和 get、`index get`、`log search`）。`text` 和 `raw` 不被接受。

`--output-mode` 接受 `stdout`（默认）和 `file`。任何其他值都会被拒绝。

### 7.2 标准输出与文件交付

默认情况下，结果写入标准输出。使用 `--output-mode file`（强制文件模式）时，输出契约取决于所调用的命令面：

- 对于 `raw`、`tool exec` 和 `workflow exec`，完整的信封会写入 `--output-dir` 下的工件文件。标准输出只包含固定的中文文件通知（`结果已写入文件。\n文件: <path>\n`），而不是信封。自动化必须从通知中读取工件路径并读取文件。
- 对于非信封命令组，原始结果会写入所选文件，标准输出只输出裸文件路径。
- 对于通用人类快捷方式信封命令（例如 `project list`、`topic create`），信封工件会写入文件，同时完整的信封也会输出到标准输出。
- 对于人类 `volclog-human log export` / `log export-analysis`，导出的数据行会流式写入文件工件（对于 `--output json` 为 JSON 数组，对于 `--output jsonl` 为 JSONL 行），标准输出会输出包含工件元数据的完整信封。工件本身不是信封。

自动化必须遵循其所调用命令面的输出契约；固定的中文通知规则专门适用于 `raw`、`tool exec` 和 `workflow exec`。人类日志导出不使用固定通知。

例外：默认的 `workflow exec log.export` 和 `log.export-analysis` 将导出的行直接流式写入数据工件（对于 `--output json` 为 JSON 数组，对于 `--output jsonl` 为 JSONL 行），而不是写入信封。数据工件不是信封，也不包含信封元数据。标准输出仍然只包含带有数据工件路径的固定文件通知。详见 [高级](6-Advanced_zh.md) 第 2 节。

`--output-file` 对 `tool`、`workflow` 和 `raw` 是被拒绝的；对这些组使用 `--output-dir`。其他组可以使用 `--output-file` 或 `--output-dir`。

### 7.3 自动溢出（`file_auto`）

对于 `tool exec` 和 `workflow exec`，自动溢出要求输出格式为 JSON（`--output json`，无论是默认值还是显式设置）；`--output jsonl` 不会触发自动溢出。当估计的 JSON 信封大小超过 16 KiB 且输出模式为 `stdout`（未显式设置）、没有 `--jmes-filter`、且没有 `execution.artifact`/`execution.projection` 时，完整的信封会自动写入 `--output-dir` 下的文件。存储在文件中的信封的 `summary.deliveryMode` 为 `file_auto`。标准输出只输出固定的文件通知（`结果过大，已写入文件。\n文件: <path>\n`）；内部预览不会打印到标准输出。自动化必须从通知中读取工件路径并读取文件。如果 `--output-dir` 缺失或不可写，命令会报错 `result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery`。

### 7.4 确定性文件交付

```bash
volclog --profile default --output json --output-mode file --output-dir ./out \
  tool exec project.describe-projects
```

### 7.5 交互式认证命令的冻结输出

`login`、`logout`、`sso` 和 `configure sso` 始终将其精确的 JSON 结果形状写入标准输出。`--output jsonl|table` 可能会被解析，但不会改变冻结的 JSON 形状（JSON 被强制）；不要依赖它来改变输出。以下选项在任何认证副作用运行之前被拒绝：非 `stdout` 的 `--output-mode`、`--output-file`、`--jmes-filter`、`--trace-dir` 和 `--secrets-file`。单独的 `--output-dir` 和不带 `--trace-dir` 的 `--trace-redact` 不会转移冻结的结果。

## 8. 过滤、投影与信封

### 8.1 `--jmes-filter` 作用于完整信封

对于 `raw`、`tool exec` 和 `workflow exec`，`--jmes-filter` 作用于完整的信封（包括 `status`、`summary`、`data`、`error`）。对于其他组，它作用于原始结果值。

### 8.2 空值、缺失路径与无效表达式

- 如果过滤器解析为现有的 `null` 值，命令打印字面量 `null` 并成功。
- 如果过滤器路径不存在（缺失键、越界索引），命令失败并报 `filter matched no value` 错误，退出码为 `3`。
- 如果过滤器表达式无效，命令失败并报 `invalid jmes-filter expression`，退出码为 `3`。

### 8.3 与文件交付不兼容

`--jmes-filter` 不能与 `--output-mode file` 组合用于 `raw`、`tool exec` 和 `workflow exec`。对于其他组，该组合是允许的。

### 8.4 CLI 信封过滤与 `execution.projection`

`--jmes-filter` 是应用于最终信封（或非信封组的原始结果）的 CLI 级过滤器，在收到响应后应用。

`execution.projection`（在 `tool exec` 和 `workflow exec` 的 `--context` JSON 中）是 CLI 本地的 JMESPath 投影，在收到响应后、CLI 构建信封之前应用于原始结果。它不是服务器端的。它使用与 `--jmes-filter` 相同的 JMESPath 引擎，但作用于原始结果而不是最终信封。

### 8.5 信封字段与错误输出

成功信封：`status`、`action`、`requestId`、`summary`（`outputMode`、`deliveryMode`、`dryRun`、`itemCount`、`totalBytes`、可选的 `pagination`/`tracePath`）、`artifacts`、`data`、`error`（`null`）。

错误信封：`status`（`failed`）、`action`、`requestId`、`summary`、`artifacts`（空）、`data`（`null`）、`error`（`source`、`code`、`message`、`requestId`、`statusCode`、`kind`、`hint`、可选的 `details`）。

结构化错误信封由 `raw`、所有 `tool` 命令（包括 `list`/`describe`）、所有 `workflow` 命令（包括 `list`/`describe`）以及人类快捷方式信封组（`project`、`topic`、`metric-topic`、`index`、`log`）使用。其他非信封组以扁平 JSON 写入标准错误，字段顺序如下：`errorCode`、`errorMessage`、`requestId`、`statusCode`、`kind`、`hint`。

## 9. 追踪与诊断

### 9.1 追踪目录与脱敏

`--trace-dir <dir>` 启用追踪。当 CLI 创建新的追踪路径时，它为目录请求 `0700`、为名为 `trace-<UTC 时间戳>.jsonl` 的 JSONL 文件请求 `0600`。已有的目录和文件可能保留其当前权限。追踪事件包括 `http_request`、`http_response` 和 `plan`。

每个追踪事件记录事件类型，以及在适用时：HTTP 方法和路径、响应状态、Request ID 和经过的毫秒数。正常的结构化请求/响应追踪字段只包含头键（从不包含值）、查询键（从不包含值）以及请求和响应体的 SHA-256 哈希（从不包含原始字节）。`Authorization` 和 `X-Security-Token` 始终包含在脱敏的头键列表中。

`error_message` 字段直接存储传输错误字符串。传输错误可能包含完整的请求 URL 和查询值。永远不要将凭证或其他密钥放在查询参数中。将追踪文件视为敏感的诊断工件：保持其权限受限，仅在检查后再分享。

`--trace-redact` 接受 `on`（默认）和 `off`，以及别名（`true`/`1`/`yes`/`enabled`/`strict`/`default` 映射为 `on`；`false`/`0`/`no`/`disabled` 映射为 `off`）。无法识别的值默认为 `on`。`--trace-redact off` 会被接受和规范化，但不会禁用强制的结构化字段脱敏，也不会输出原始的头、查询或体值。

### 9.2 Doctor 与 Request ID

`doctor` 在本地检查配置和凭证；`doctor --online` 对 TLS endpoint 执行最小的实时连通性检查。

```bash
volclog --profile default doctor
volclog --profile default doctor --online
```

信封中的 `requestId`（或 `x-tls-requestid` 响应头）标识请求，用于支持和排障。

## 10. 人类快捷方式与下一步

`volclog-human` 快捷方式为常见的交互式任务提供更短的路径。它们复用共享的认证、传输、信封和追踪基础设施，但其参数、契约校验、表格支持和文件/标准输出行为可能与 `tool`/`workflow`/`raw` 不同。有关输出行为，请参阅第 7 节。

```bash
volclog-human project list --output table
volclog-human topic create --describe
volclog-human index create --print-request-template=required > index_req.json
volclog-human index create --topic-id <topic-id> --request file://index_req.json
```

Human shortcut 的完整上手方式见 [Human Shortcuts](7-Human-Shortcuts_zh.md)。对于更长的自动化工作流，请参阅 [实用指南](5-Practical-Guide_zh.md)；对于高级主题，请参阅 [进阶](6-Advanced_zh.md)。

---

[← 上一篇：配置](3-Configuration_zh.md) | [English](4-Usage.md) | [下一篇：实用指南 →](5-Practical-Guide_zh.md)
