# 4. 使用

[← 上一篇：配置](3-Configuration_zh.md) | [English](4-Usage.md) | [下一篇：实用指南 →](5-Practical-Guide_zh.md)

本指南介绍如何用 `volclog` 运行命令：命令面、如何发现工具和工作流、`tool exec`、`workflow exec` 和 `raw` 的工作方式、上下文和选择器如何交互，以及输出、过滤和追踪的行为。关于认证和配置的细节，请参阅 [认证](2-Authentication_zh.md) 和 [配置](3-Configuration_zh.md)。

## 1. 命令架构与版本边界

默认的 `volclog` 命令暴露以下组：`configure`、`tool`、`workflow`、`raw`、`doctor`、`skill`、`upgrade`、`version`、`login`、`logout` 和 `sso`。

`volclog-human` 在 `tool`/`workflow`/`raw` 之外添加了人类快捷方式层（`project`、`topic`、`metric-topic`、`index`、`log`、`host-group`、`collector`）。快捷方式复用共享的认证、传输、信封和追踪基础设施，但有自己的快捷方式参数解析和请求构建。契约校验、表格支持和文件/标准输出行为可能与 `tool`/`workflow`/`raw` 不同。用户和自动化必须遵循所选命令面的文档化契约。

选择合适的入口：

- 当你知道 API 动作并想要契约校验和结构化信封时，使用 `tool`。
- 当你想要 CLI 提供的、对一个或多个工具的高层编排时，使用 `workflow`。
- 仅当你已经知道确切的方法和路径并需要直接传输调用时，使用 `raw`。
- `volclog-human` 快捷方式仅用于交互式终端工作；Agent 和 CI 应默认使用 `tool`/`workflow`/`raw`。

### 版本与显式升级

`version` 用于输出机器可读的构建和 catalog 元信息。`--version` 保留文本兼容形式，适合只需要版本字符串的脚本：

```bash
volclog version
volclog --version
```

`volclog version` 返回的 JSON 包含 `schema_version`、`version`、`edition`、`commit`、`catalog_digest`、`operation_count`、`public_operation_count` 和 `workflow_count`。

升级检查和安装只会在显式调用时执行；`volclog` 不会在后台联网检查：

```bash
volclog upgrade --check
volclog upgrade --version 1.1.1
volclog upgrade --version 1.1.1 --yes
volclog upgrade --yes
```

`--check` 以及不带 `--yes` 的版本选择不会写文件。npm 安装会委托 npm；独立二进制需要校验发布 checksum，并使用原子替换。

### 升级到 1.1.0

请把保存的命令、脚本和 Agent 工具引用中的下载任务 ID 更新为：

- `log.create` → `log.create-download-task`
- `log.cancel` → `log.cancel-download-task`

旧短名已移除，不保留为别名。使用旧名会报错并提示替代名称；底层 API action、HTTP 方法和路径不变。创建下载任务仍是异步操作，不会直接下载文件到本地。

本版本还修正了请求契约：`log.consume` 必须提供 `Cursor`，不再声明或要求 Kafka 专用的 `Offset`；`tag.list` 必须显式提供 10～100 的 `MaxResults`。请通过 `volclog tool describe <tool-id> --view full` 刷新缓存的契约。契约中的默认值描述服务端行为，CLI 不会自动注入；dry-run 不能替代服务端的类型、范围、枚举或条件校验。

### Skill 生命周期

所有 Skill 生命周期命令都必须提供 `--dir`。重复使用 `--name` 可选择多个内置 Skill；省略则选择全部内置 Skill：

```bash
volclog skill list
volclog skill install --dir <agent-skills-dir> [--name <name>] [--force]
volclog skill status --dir <agent-skills-dir> [--name <name>]
volclog skill update --dir <agent-skills-dir> [--name <name>] [--force]
volclog skill uninstall --dir <agent-skills-dir> [--name <name>] [--force]
```

每个已安装 Skill 目录都包含 `.volclog-skill.json`，记录 schema 版本、Skill 名称、安装时 CLI 版本、source digest 和安装内容 digest。digest 对排序后的相对 POSIX 路径及按长度分帧的文件内容计算稳定 SHA-256，并排除该 sidecar；symlink 或特殊文件会被拒绝。`status` 完全离线，状态包括 `not_installed`、`current`、`outdated`、`modified`、`untracked` 和 `invalid_manifest`。版本不一致会单独报告，不会仅因版本变化就判定用户修改了 Skill。

`current` 表示磁盘内容匹配 `installed_digest` 且内置 source 匹配 `source_digest`；`outdated` 表示内容未改动但内置 source digest 已变化；`modified` 表示磁盘内容不同于 `installed_digest`。缺少 sidecar 时为 `untracked`，sidecar 无法读取或内容不一致时为 `invalid_manifest`。

目标已存在时，`install` 默认报错，必须显式使用 `--force`。`update` 和 `uninstall` 默认跳过 `modified`、`untracked` 和 `invalid_manifest` Skill；使用 `--force` 才会替换或删除。安装和更新会先构造完整目录，再通过同文件系统 rename 完成替换；替换失败时恢复旧目录。

## 2. 执行前先发现

在运行工具或工作流之前，先检查其契约，以便了解所需的输入和请求形状。

### 2.1 `tool list` 和 `tool describe`

```bash
volclog tool list
volclog tool list project
volclog tool list app
volclog tool list log-app
volclog tool list trace
volclog tool list --verb create --format json
volclog tool describe project.describe-projects
volclog tool describe project.create --view full
```

`tool list` 将组作为位置参数（不是 `--group` 标志），并支持 `--verb` 按动词过滤和 `--format text|json`（默认 `text`）。`tool describe <group.action>` 显示工具的标识、输入模式、上下文模式、执行模式、行为、输出策略和契约摘要。`--view` 标志选择 `compact`（默认）或 `full`；显式 JSON 输出默认为 `full`。

`full` 视图还提供 `recovery_source`、`recovery_notes`、`doc_url`、`doc_url_source`、`input_policy` 和 `source`；没有核实到精确文档地址时 `doc_url` 为 `null`。`identity.api_version_default` 是客户端默认请求头版本（`0.3.0`），`identity.api_version_source` 说明其来源；它不是逐操作核实的 API 版本。`full` 中未知的 `identity.maintainer` 和 `identity.deprecated` 为 `null`，`compact` 会省略部分细节。

`recovery_source=reviewed-read-contract` 表示已核实该操作用于读取或非持久化用途，不证明服务端幂等，也不启用自动重试。对于 `unverified`，重试前先对账操作结果。

### 2.2 `workflow list` 和 `workflow describe`

```bash
volclog workflow list
volclog workflow list log
volclog workflow list app
volclog workflow list --format json
volclog workflow describe log.export
volclog workflow describe app.resolve-resources
```

`workflow list` 将组作为位置参数，并支持 `--format text|json`。`workflow describe <group.command>` 显示工作流的类型、来源、输入模式、上下文模式、执行模式、推荐的全局标志和指导。

## 3. `tool exec`

`tool exec` 按 `<group.action>` 标识运行单个工具。

```bash
volclog --profile default tool exec project.describe-projects
```

### 3.1 输入与上下文

`--input` 提供操作请求字段。它接受 `file://<path>`、`-`（标准输入）或内联 JSON 对象（必须以 `{` 开头）。`--context` 使用相同的来源提供运行时和执行控制。

上下文会在应用前校验未知字段、错误类型和冲突的别名。既有形式继续支持：`trace` 的布尔值、字符串或对象；`execution.artifact` 的布尔值、字符串或对象；`execution.output`/`output_mode`；字符串、对象或数组形式的 `execution.projection`；以及受支持的分页工具的 `execution.page.all`/`page_all`。

```bash
volclog --profile default --allow-write tool exec project.create \
  --input '{"ProjectName":"my-project","Region":"cn-beijing","Description":"example"}'

volclog --profile default tool exec project.create \
  --input file://req.json \
  --context '{"execution":{"dry_run":true}}'
```

`file://req.json` 文件必须包含必填的 `ProjectName` 和 `Region` 字段。

### 3.2 契约校验与预执行

在任何网络调用之前，`tool exec` 将无歧义的扁平输入路由到 `query`/`path`/`header`/`body`，并对扁平和分段输入应用相同的已声明 schema。分段输入会拒绝额外的顶层键；段内额外字段按 `additionalProperties` 声明拒绝、接受或按子 schema 校验（未声明时允许）。没有分段 schema 的操作保留透传行为，仍受输入和上下文形状规则约束。CLI 不注入 schema 默认值；human 快捷方式保留原有 legacy 校验，服务端仍会继续校验。

`--dry-run` 运行相同的契约校验，然后跳过网络调用并返回一个本地计划，该计划检查配置档、endpoint、region 和请求体 JSON 的有效性：

```bash
volclog --profile default tool exec project.create \
  --input '{"ProjectName":"my-project","Region":"cn-beijing"}' --dry-run
```

`--page-all` 是 `tool exec` 的直接标志，用于请求分页结果的所有页。

不启用 `--page-all` 时，已声明分页的列表仍只请求一页，并在成功信封中返回 `summary.pagination.mode="single_page"` 和 `summary.completeness`。还有后续页或无法证明结束时，`complete=false`，可安全续取时提供 `next.operationId` 和 `next.input`；单页成功仍返回 0。续取保留筛选条件及省略的服务端 PageSize 默认值，不会套用 page-all 的请求大小。只有从第一页取得完整列表时才标记 `complete=true`；从中间页开始，即使已经到末尾，也只是 `stopReason="page_slice"`，不会宣称包含此前页面。没有显式 PageSize 且无法确定末页时，可能需要续取一个空页来确认结束。

即使已经合并页面，也要检查 `summary.completeness`：`summary.pagination.merged=true` 只表示已累积页面，不代表结果完整。服务端的 `data.Total` 保持原值，`summary.itemCount` 表示已收集条数。完整性包含 `complete`、`stopReason`、`pageCount`、`itemCount`、`resumable`，以及可用时的 `next`。触及页数预算或后续请求失败时命令返回非零，并保留已收集数据。存在 `next` 时，复用相同运行时选择器，以其 `operationId` 和 `input` 发起普通 `tool exec` 单页调用，不传 `--page-all`，也不在上下文中启用 page-all；`next.pageAll` 为 `false`。

## 4. `workflow exec`

`workflow exec` 按 `<group.command>` 标识运行 CLI 提供的高层工作流。工作流用编排逻辑封装一个或多个工具；公共 API 工具面仍可通过 `tool exec` 使用。

```bash
volclog --profile default workflow exec log.export \
  --input file://export.json
```

### 4.1 输入与上下文

`--input` 和 `--context` 接受与 `tool exec` 相同的来源：`file://<path>`、`-`（标准输入）或内联 JSON 对象。工作流的必填参数在执行前会被校验。

### 4.2 执行控制

`workflow exec` 不接受 `--artifact`、`--projection` 或 `--page-all` 作为直接 CLI 标志。它从 `--context` JSON 的 `execution` 对象中消费的控制是：

- `execution.dry_run` — 跳过网络调用
- `execution.artifact` — 强制文件交付
- `execution.projection` — 在构建信封之前用 JMESPath 表达式投影原始结果
- `execution.non_interactive` — 禁止交互提示，不能关闭非 TTY 的自动模式
- `execution.allow_write` — 允许已授权的 CLI 写操作，不代替 IAM 授权
- `execution.timeout` — 以正 Go duration 设置整次命令时限

`workflow exec` 会显式拒绝上下文中的 `execution.page` 和 `execution.page_all`，即使值为 `false`。分页语义是工作流专属的；请遵循 `workflow describe` 中针对你所使用工作流的指导。

```bash
volclog --profile default workflow exec log.export \
  --input file://export.json \
  --context '{"execution":{"dry_run":true}}'
```

在运行工作流之前，请使用 `workflow list` 或 `workflow describe` 确认该工作流 ID 在当前安装中可用。

SearchLogs 的 `ListOver` 只适用于纯检索的结束判断；SQL/分析结果只有 `ResultStatus=complete` 才完整。`incomplete` 和 `time_out` 会报告为不完整，`error` 会报告为失败。不要为了续取不完整响应而凭空构造 SQL `OFFSET`；用户显式编写 SQL `LIMIT`/`OFFSET` 的每次调用都是独立查询，不保证同一快照。

### 4.3 App 资源解析工作流

`app`、`log-app` 和 `trace` 组提供单次公开 API 的原子能力；先用 `tool list <group>` 和 `tool describe <group.action> --view full` 获取当前契约。只有在需要跨 `DescribeApp`、`DescribeLogApp` 和 `DescribeTraceInstance` 展开关系时才使用下面的 workflow：

```bash
volclog workflow exec app.resolve-resources \
  --input '{"AppId":"YOUR_APP_ID"}'

volclog workflow exec app.resolve-topic-ids \
  --input '{"AppId":"YOUR_APP_ID"}'
```

- `app.resolve-resources` 返回规范化的 `App`、`Nodes`、`Edges`、`LogAppIds`、`TraceInstanceIds` 和 `TopicIds`。非 LogApp 应用保留为通用 `AppResource` 节点，不猜测资源语义。
- `app.resolve-topic-ids` 仅支持 `AppType=LogApp`，返回按首次出现顺序去重的 `TopicIds`；Trace 资源同时包含 Trace Topic 和 Dependency Topic。
- 展开过程不自动跨 Region 路由；任一依赖调用失败时不会返回部分结果。先用 `--dry-run` 可查看依赖步骤，但资源 ID 和 Region 校验仍需实际响应。

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

非空 `--profile` 或 `context.profile` 必须指向已存在的配置档。配置档的静态 AK/SK 和 token 不会被环境凭证替换或补齐；静态 AK/SK 不完整会在请求前失败。动态身份仍由提供者获取。未显式选择配置档时保留原有环境优先级。

身份选择器是：全局 `--profile`、全局 `--secrets-file`、`context.profile` 和 `context.secrets_file`。冲突规则如下：

- 任何配置档选择器（全局 `--profile` 或 `context.profile`）与任何密钥文件选择器（全局 `--secrets-file` 或 `context.secrets_file`）组合都会冲突。
- 全局 `--secrets-file` 与 `context.secrets_file` 组合会冲突。
- 全局 `--profile` 与 `context.profile` 仅在配置档名称不同时才冲突。在两处重复相同的配置档名称是可接受的，但属于冗余；为清晰起见应避免这样做。

冲突会失败并报错 `conflicting runtime selectors`（当提供两个不同的配置档名称时为 `conflicting profile selectors`）。

全局 `--region`、`--endpoint` 和 `context.region`、`context.endpoint` 都是单次命令的运行时覆盖，优先于适用的配置档、环境和项目默认值，且不会落盘。同一字段的全局标志与上下文值均非空但不同时，校验失败。完整的 region/endpoint/timeout 优先级见 [配置](3-Configuration_zh.md)。

需要独立值的全局标志不会吞掉后续 `--flag`：例如 `--output-dir --dry-run` 会报告缺少值。以 `--` 开头的相对路径可加 `./` 前缀。

### 6.2 `--dry-run` 与上下文预执行

`--dry-run` 是一个全局标志，适用于 `raw`、`tool exec` 和 `workflow exec`。对于 `tool exec` 和 `workflow exec`，`--context` JSON 中的 `execution.dry_run` 具有相同效果。`--dry-run` 对其他组是被拒绝的。

预执行计划包含 `request_preview` 时，会递归掩码 password、secret、token、access key、credential 等已知敏感键的值；对于 `Contents`/`LogTags` 的 `Key`/`Value`，敏感 `Key` 对应的 `Value` 也会被掩码。无法解析的请求体不会原样显示，可选的 `redacted_fields` 会列出被掩码的路径。这只改变预览，不改变实际发送的请求或 `body_sha256`。普通请求体字符串中的秘密不保证被识别。

无法解析的类 JSON 查询值会替换为 `[unparseable query omitted]`；显示的 endpoint 会移除用户信息、查询参数和片段。

### 6.3 写操作安全

TLS 写请求和操作身份未知的 raw 请求默认被拒绝，返回 `error.kind=permission`、`error.code=WritePermissionRequired`；未知 raw GET 和 `volclog-human` 写快捷方式也受此限制。仅对用户已授权的写操作使用 `--allow-write`，或为 `tool exec` / `workflow exec` 设置 `context.execution.allow_write=true`。这是 CLI 动作闸门，不授予 IAM 权限。已复核的只读操作（包括 POST `log.search`）不需要写入许可；不要给所有调用都加该标志。全局 `--allow-write` 与上下文显式 `allow_write=false` 冲突。

发送写请求前可用 `--dry-run` 本地校验。预执行免于写入闸门且不执行业务请求，但不能证明服务端会接受请求。

### 6.4 非交互与整次命令超时

非 TTY 时自动启用非交互模式。`--non-interactive` 或 `context.execution.non_interactive=true` 可显式启用；上下文 `false` 不能关闭非 TTY 自动模式，且与显式全局 `true` 冲突。

`--timeout 30s` 或 `context.execution.timeout="30s"` 可用正 Go duration 设置可选的整次命令预算。预算从命令调用开始计时，不因翻页或分批重置，且没有整体默认值；它独立于配置档的单次请求 `timeout_seconds`。全局与上下文提供等值 duration 时可重复，不等则冲突。SIGINT、SIGTERM 和 `RunContext` 取消会向工作链传递：发送前为 `not_sent`，发送后若结果不确定仍为 `unknown`；已完成的部分结果按 C3 完整性合同保留。

关于各认证方式的登录、刷新和登出行为，请参阅 [认证](2-Authentication_zh.md)。关于配置档和运行时优先级，请参阅 [配置](3-Configuration_zh.md)。

## 7. 输出与交付

### 7.1 输出值

`--output` 在默认的 `volclog` agent 路径上接受 `json`（默认）和 `jsonl`。`table` 在默认路径上会被拒绝；它仅由 `volclog-human` 支持，且仅用于特定的快捷方式入口（`project`/`topic`/`metric-topic` 的 list 和 get、`index get`、`log search`）。`text` 和 `raw` 不被接受。

`--output-mode` 接受 `stdout`（默认）和 `file`。任何其他值都会被拒绝。

未显式筛选标准输出时，默认 `volclog` 的 `raw`、`tool exec` 和 `workflow exec` 成功与错误结果均输出 JSON 信封。`raw` 返回的 JSON 标量或字符串放在 `data` 中；`volclog-human` 保留原有展示。显式的标准输出 `--jmes-filter` 可返回数组、标量或 `null`。

### 7.2 标准输出与文件交付

默认情况下，结果写入标准输出。使用 `--output-mode file`（强制文件模式）时，输出契约取决于所调用的命令面：

- 对于 `raw`、`tool exec` 和 `workflow exec`，成功结果会写入 `--output-dir` 下的工件文件：通常是完整信封；设置 `--jmes-filter` 时则是选中的值。默认 `volclog` 的标准输出为 JSON 交付信封，包含 `status=success`、`data=null`，文件路径位于 `artifacts[].path`；`volclog-human` 保留中文文件通知。
- 对于非信封命令组，原始结果会写入所选文件，标准输出只输出裸文件路径。
- 对于通用人类快捷方式信封命令（例如 `project list`、`topic create`），信封工件会写入文件，同时完整的信封也会输出到标准输出。
- 对于不设置 `--jmes-filter` 的人类 `volclog-human log export` / `log export-analysis`，导出的数据行会流式写入文件工件（对于 `--output json` 为 JSON 数组，对于 `--output jsonl` 为 JSONL 行），标准输出会输出包含工件元数据的完整信封。工件本身不是信封。

默认版文件交付成功时，`summary.deliveryMode` 为 `file_forced` 或 `file_auto`，`summary.itemCount` 保留原业务结果数量，`summary.totalBytes` 是标准输出交付信封的字节数，`artifacts[].sizeBytes` 是实际文件字节数。这是兼容性变化：原先解析中文通知的脚本须改为读取 `artifacts[].path`。human 日志快捷方式保留自己的输出契约。

例外：不设置 `--jmes-filter` 时，默认的 `workflow exec log.export` 和 `log.export-analysis` 将导出的行直接流式写入数据工件（对于 `--output json` 为 JSON 数组，对于 `--output jsonl` 为 JSONL 行），而不是写入信封。数据工件不是信封，也不包含信封元数据。成功时标准输出包含带有数据工件路径的 JSON 交付信封。设置 `--jmes-filter` 后，这些导出会在内存中累积结果，再过滤并写入文件。大导出应优先使用无过滤的流式导出，或在支持时使用服务端字段选择；客户端过滤不会减少服务端扫描或网络流量。详见 [高级](6-Advanced_zh.md) 第 2 节。

流式导出触及页数预算或后续失败时返回非零；有效写入的行可保留时，会存入唯一的部分文件，不会替换完整目标文件。错误信封中的工件描述包含 `completeness` 和 `metadataPath`，后者指向部分文件的 `.meta.json` 元数据；若元数据交付本身失败，则报告保留的数据路径和交付错误。

`--output-file` 对 `tool`、`workflow` 和 `raw` 是被拒绝的；对这些组使用 `--output-dir`。其他组可以使用 `--output-file` 或 `--output-dir`。

### 7.3 自动溢出（`file_auto`）

对于 `tool exec` 和 `workflow exec`，自动溢出要求输出格式为 JSON（`--output json`，无论是默认值还是显式设置）；`--output jsonl` 不会触发自动溢出。当估计的 JSON 信封大小超过 16 KiB 且输出模式为 `stdout`（未显式设置）、没有 `--jmes-filter`、且没有 `execution.artifact`/`execution.projection` 时，完整的信封会自动写入 `--output-dir` 下的文件。存储在文件中的信封的 `summary.deliveryMode` 为 `file_auto`。默认版标准输出为 `data=null` 的交付信封，路径位于 `artifacts[].path`，不包含业务结果预览；human 标准输出保留中文通知。如果 `--output-dir` 缺失或不可写，命令会报错 `result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery`。

### 7.4 确定性文件交付

```bash
volclog --profile default --output json --output-mode file --output-dir ./out \
  tool exec project.describe-projects
```

### 7.5 交互式认证命令的冻结输出

`login`、`logout`、`sso` 和 `configure sso` 始终将其精确的 JSON 结果形状写入标准输出。`--output jsonl|table` 可能会被解析，但不会改变冻结的 JSON 形状（JSON 被强制）；不要依赖它来改变输出。以下选项在任何认证副作用运行之前被拒绝：非 `stdout` 的 `--output-mode`、`--output-file`、`--jmes-filter`、`--trace-dir` 和 `--secrets-file`。单独的 `--output-dir` 和不带 `--trace-dir` 的合法 `--trace-redact` 不会转移冻结的结果。

非交互模式下，`login` 使用 device code 且不打开浏览器。若替换会话需要确认，立即返回 `interaction_required`；请使用其他 `--profile` 或交互式终端，没有 `--force` 覆盖。SSO 账号或角色只有一个候选时自动选择，多个候选且未指定所需选择时立即失败；失败时事务性 token 回滚、配置更改不提交，注册缓存可能已写入。

## 8. 过滤、投影与信封

### 8.1 `--jmes-filter` 作用于完整信封

CLI 会在发送任何请求前编译并校验 `--jmes-filter`。表达式无效时会在本地失败，不会发起网络调用。收到响应后，对于 `raw`、`tool exec` 和 `workflow exec`，过滤器作用于完整的信封（包括 `status`、`summary`、`data`、`error`）；对于其他组，它作用于原始结果值。

### 8.2 空值、缺失路径与无效表达式

- 如果过滤器解析为现有的 `null` 值，命令成功，并将字面量 `null` 输出到标准输出或写入所请求的文件。
- 如果过滤器路径不存在（缺失键、越界索引），命令失败并报 `filter matched no value` 错误，退出码为 `3`；文件交付不会新建或覆盖结果文件。
- 如果过滤器表达式无效，命令失败并报 `invalid jmes-filter expression`，退出码为 `3`。

### 8.3 筛选后写入文件

对于 `raw`、`tool exec` 和 `workflow exec`，可将 `--jmes-filter` 与 `--output-mode file` 组合，把完整成功信封中选中的值写成单个 JSON 文档。`tool exec` 和 `workflow exec` 也支持通过上下文中的 `execution.artifact` 请求文件交付。即使设置 `--output jsonl`，文件仍是 JSON 而不是 JSONL；内容可以是数组、标量或 `null`，不一定是信封。过滤只改变文件内容：默认版标准输出仍是 JSON 交付信封，human 标准输出保留中文通知。不设置过滤器时，普通文件输出保留完整信封；上述无过滤流式导出例外保持不变。

过滤器看到的 `summary.deliveryMode` 是 `file_forced`；该输入中的 `summary.totalBytes` 表示过滤前完整信封的大小。默认版标准输出的交付信封有自己的 `summary.totalBytes`，而 `artifacts[].sizeBytes` 表示筛选后文件的大小。全局过滤器会禁用自动溢出。如果上游操作在文件模式失败，命令输出完整错误并返回非零退出码，不会把错误过滤到文件中。

例如，从信封中选择项目名称并写入 `./out`：

```bash
volclog --profile default --output jsonl --output-mode file --output-dir ./out \
  --jmes-filter 'data.Projects[].ProjectName' \
  tool exec project.describe-projects --input '{"PageSize":20}'
```

也可以先投影原始结果，再选择信封的 `data`，并通过上下文请求文件交付：

```bash
volclog --profile default --output-dir ./out --jmes-filter 'data' \
  tool exec project.describe-projects --input '{"PageSize":20}' \
  --context '{"execution":{"projection":"Projects[].ProjectName","artifact":true}}'
```

第一个示例将项目名称数组写为 JSON。第二个示例保留既有投影封装：数组位于 `data.items`，并带有服务端返回的 `Total` 等元数据；选择 `data` 会将这个对象写入文件。从默认版标准输出信封的 `artifacts[].path` 读取路径后即可打开文件。

### 8.4 CLI 信封过滤与 `execution.projection`

`--jmes-filter` 会在请求前编译，然后在收到响应后作为 CLI 级过滤器应用于最终信封（或非信封组的原始结果）。

`execution.projection`（在 `tool exec` 和 `workflow exec` 的 `--context` JSON 中）会在请求前编译，并在收到响应后、CLI 构建信封之前作用于原始结果。它不是服务器端的。它使用与 `--jmes-filter` 相同的 JMESPath 引擎，但作用于原始结果而不是最终信封。上面的示例在信封过滤中使用 `data.Projects[].ProjectName`，在更早的原始结果投影中使用 `Projects[].ProjectName`；两者同时使用时，过滤器在 `data` 下看到的是投影后的结果。

动态 JSON 在请求体、服务响应、信封、JSONL 和 JMESPath 求值的全链路保留 `json.Number`，不会先转换为 `float64` 而损失精度。因此大整数和长小数可以保持原始精度。

### 8.5 信封字段与错误输出

成功信封：`status`、`action`、`requestId`、`summary`（`outputMode`、`deliveryMode`、`remoteOutcome`、`deliveryOutcome`、`dryRun`、`itemCount`、`totalBytes`、可选的 `pagination`/`completeness`/`async`/`asyncTasks`/`tracePath`）、`artifacts`、`data`、`error`（`null`）。

错误信封：`status`（`failed`）、`action`、`requestId`、`summary`、`artifacts`、`data`、`error`（`source`、`code`、`message`、`requestId`、`statusCode`、`kind`、`hint`、可选的 `details`）。`data` 通常为 `null`，也可能包含保留的部分结果或批量写入进度；`artifacts` 可能描述保留的部分文件。

默认 `volclog` 的早期用法错误也在标准输出给出失败 JSON 信封并返回非零，包括无参数、未知选项或命令组、选项缺值。显式帮助和 `--version` 仍为文本；机器读取版本请使用 `version` 命令。`tool`/`workflow` 的文本发现输出，以及 `login`、`logout`、`sso`、`configure sso` 的冻结 JSON 形状和错误契约保持不变。human 快捷方式信封组保留既有错误契约。标准输出损坏时，CLI 返回退出码 3，并尽力向标准错误输出结构化报告。

`summary.remoteOutcome` 表示业务操作状态：`not_sent`（未发送）、`unknown`（结果未知）、`accepted`（已接受）、`success`（成功）、`failed`（失败）或 `partial`（部分完成）。`accepted` 表示有明确契约的异步提交已被接受，不代表任务已完成。`summary.deliveryOutcome`（`success` 或 `failed`）独立表示结果交付状态。遇到 `output_failed` 应修复交付问题，不要重跑原写操作；遇到 `result_unknown` 应先对账，再决定是否写入。本次 CLI 改动不新增重试；既有 `http.Client`/`Transport` 策略及自定义 transport 的行为仍然适用。

创建下载任务返回 `accepted` 和 `summary.async`，包含任务 ID；有主题 ID 时还包含一次调用 `log.describe-download-tasks` 的 `next`。CLI 不自动等待。`TaskName` 为模糊筛选，应跨页匹配 `Tasks[].TaskId`，再查看 `summary.asyncTasks` 中对应条目。`wait`/`creating` 是非终态；`success` 表示文件已生成；`created_cut` 是部分结果，仅部分任务可下载；`fail` 表示失败，未知状态仍为未知。成功条目可提供 `log.describe-download-url` 的 `next`，截断条目只提供有条件的 `possibleNext`。下载链接有效期为 5 分钟。文件生成不保证导出整个查询窗口，请同时考虑请求条数、精度选项和服务端限制。

批量写入出现 `partial_failure` 时，检查 `summary.progress` 或 `data.progress` 中的 `confirmedBatches`、`confirmedLogs`、`confirmedLogOffset`（从零开始的排他偏移）、`currentBatch`（有当前批次时从 1 开始）、`currentBatchOutcome` 和 `lastConfirmedRequestId`。只有收到确认成功的响应才增加计数；HTTP 200 后读取失败或响应不符合协议时结果为 `unknown`，不能把未知批次当作可自动续传的安全断点。当前 `requestId` 和 `statusCode` 只对应当前尝试，不沿用先前确认批次的值。

标准 catalog JSON 响应路径要求 HTTP 200，非空响应体必须是有效 JSON；空响应以及特殊 codec、`raw`、protobuf 路径保留既有 2xx/text/empty/PB 兼容合同。错误可使用 `not_found`、`conflict`、`throttled`、`protocol` 和类型化 `validation` 等 kind。退出码 1、2、3 保持既有含义；暂时性错误本身不代表重试安全。标准输出交付失败时，CLI 返回非零并尽力向标准错误输出结构化报告；损坏的标准输出无法保证包含 JSON。

## 9. 追踪与诊断

### 9.1 追踪目录与脱敏

`--trace-dir <dir>` 启用追踪。当 CLI 创建新的追踪路径时，它为目录请求 `0700`、为名为 `trace-<UTC 时间戳>.jsonl` 的 JSONL 文件请求 `0600`。已有的目录和文件可能保留其当前权限。追踪事件包括 `http_request`、`http_response` 和 `plan`。

每个追踪事件记录事件类型，以及在适用时：HTTP 方法和路径、响应状态、Request ID 和经过的毫秒数。正常的结构化请求/响应追踪字段只包含头键（从不包含值）、查询键（从不包含值）以及请求和响应体的 SHA-256 哈希（从不包含原始字节）。`Authorization` 和 `X-Security-Token` 始终包含在脱敏的头键列表中。

`error_message` 字段直接存储传输错误字符串。传输错误可能包含完整的请求 URL 和查询值。永远不要将凭证或其他密钥放在查询参数中。将追踪文件视为敏感的诊断工件：保持其权限受限，仅在检查后再分享。

`--trace-redact` 接受 `on`（默认）、`true`、`1`、`yes`、`enabled`、`strict` 和 `default`，均启用脱敏；`off`、`false`、`0`、`no`、`disabled` 和未知值会校验失败。`context.trace=false` 或 `context.trace.enabled=false` 会关闭全局配置的追踪目录。

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
volclog-human --allow-write index create --topic-id 'YOUR_TOPIC_ID' --request file://index_req.json
```

Human shortcut 的完整上手方式见 [Human Shortcuts](7-Human-Shortcuts_zh.md)。对于更长的自动化工作流，请参阅 [实用指南](5-Practical-Guide_zh.md)；对于高级主题，请参阅 [进阶](6-Advanced_zh.md)。

---

[← 上一篇：配置](3-Configuration_zh.md) | [English](4-Usage.md) | [下一篇：实用指南 →](5-Practical-Guide_zh.md)
