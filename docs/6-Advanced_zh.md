# 6. 高级

[← 上一篇：实用指南](5-Practical-Guide_zh.md) | [English](6-Advanced.md) | [下一篇：Human Shortcuts →](7-Human-Shortcuts_zh.md)

本指南涵盖高级运维决策、边界情况和模式。关于基础契约，请参阅 [使用](4-Usage_zh.md)、[配置](3-Configuration_zh.md) 和 [认证](2-Authentication_zh.md)。

## 1. 过滤与投影

`--jmes-filter` 会在发送任何请求前编译并校验，然后在收到响应后应用于最终信封（对于 `raw`、`tool exec`、`workflow exec`）或原始结果（对于其他组）。

`execution.projection`（在 `tool exec` 和 `workflow exec` 的 `--context` JSON 中）会在请求前编译，并在收到响应后、CLI 构建信封之前作用于原始结果。它不是服务器端的。它使用与 `--jmes-filter` 相同的 JMESPath 引擎，但作用于原始结果而不是最终信封：例如选择项目名称时，信封过滤使用 `data.Projects[].ProjectName`，原始结果投影使用 `Projects[].ProjectName`。两者同时使用时，过滤器在 `data` 下看到投影后的结果。

动态 JSON 在请求解码、服务响应、信封、JSONL 输出和 JMESPath 求值的全链路保留 `json.Number`。大整数和长小数不会经过 `float64` 转换，因此能够保持精度。无效的 filter 或 projection 会在本地预执行阶段失败，不会发送请求。

行为：

- 如果过滤器解析为现有的 `null` 值，命令成功，并将字面量 `null` 输出到标准输出或写入所请求的文件。
- 如果过滤器路径不存在（缺失键、越界索引），命令失败并报 `filter matched no value`，退出码为 `3`；文件交付不会新建或覆盖结果文件。
- 如果过滤器表达式无效，命令失败并报 `invalid jmes-filter expression`，退出码为 `3`。

对于 `raw`、`tool exec` 和 `workflow exec`，可将 `--jmes-filter` 与 `--output-mode file` 组合；`tool exec` 和 `workflow exec` 也可以通过上下文中的 `execution.artifact` 请求文件交付。完整成功信封中选中的值会写为单个 JSON 文档，即使设置 `--output jsonl` 也是如此。文件可以包含数组、标量或 `null`，不一定是信封。标准输出只输出带路径的固定中文文件通知。不设置过滤器时，普通文件交付保留完整信封；无过滤的流式导出仍是例外。

文件模式的过滤视图中，`summary.deliveryMode=file_forced`，`summary.totalBytes` 表示过滤前信封的大小，而不是筛选文件的大小。全局过滤器会禁用自动溢出。上游操作在文件模式失败时，命令输出完整错误并返回非零退出码，不会把错误过滤到文件中。

```bash
volclog --profile default --output jsonl --output-mode file --output-dir ./out \
  --jmes-filter 'data.Projects[].ProjectName' \
  tool exec project.describe-projects --input '{"PageSize":20}'
```

从标准输出通知中读取文件路径；文件内容是选中的项目名称 JSON 数组。`execution.artifact` 和投影的组合示例见[使用](4-Usage_zh.md)第 8.3 节。

## 2. 大结果交付

强制文件模式（`--output-mode file`）的行为取决于命令面：

- `raw`、`tool exec`、`workflow exec`：完整成功信封或 `--jmes-filter` 选中的值写入 `--output-dir` 下的工件；标准输出只包含固定的中文文件通知。
- 非信封组：原始结果写入所选文件，标准输出只输出裸文件路径。
- 通用人类快捷方式信封命令（例如 `project list`、`topic create`）：信封工件写入文件，同时完整的信封也输出到标准输出。
- 不设置 `--jmes-filter` 的人类 `volclog-human log export` / `log export-analysis`：导出的数据行流式写入文件工件（对于 `--output json` 为 JSON 数组，对于 `--output jsonl` 为 JSONL 行），标准输出输出包含工件元数据的完整信封。工件本身不是信封。

固定的中文通知仅限于 `raw`、`tool exec` 和 `workflow exec`；人类日志导出不使用它。自动化必须遵循其所调用命令面的输出契约。

自动溢出（`file_auto`）适用于 `tool exec` 和 `workflow exec`，需同时满足：输出格式为 JSON（`--output json`，默认或显式）、输出模式为 `stdout` 且未显式设置、没有 `--jmes-filter`、且没有 `execution.artifact`/`execution.projection`。当估计的信封大小超过 16 KiB 时，完整的信封会写入 `--output-dir` 下的文件；标准输出只输出固定的文件通知。如果 `--output-dir` 缺失或不可写，命令会报错 `result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery`。

下面的默认流式导出行为适用于未设置 `--jmes-filter` 的情况。设置过滤器后，流式写入器会被禁用，结果在内存中累积后再过滤并写入文件。大导出应优先使用无过滤的流式导出，或在支持时使用服务端字段选择；客户端过滤不会减少服务端扫描或网络流量。

默认的 `workflow exec log.export` 和 `log.export-analysis` 将导出的行直接流式写入数据工件（JSON 数组或 JSONL），而不是写入信封。成功时标准输出包含固定文件通知；行文件本身不包含 `status`、`summary`、`requestId`、`ResultStatus` 或 `ListOver`。纯检索 `log.export` 省略 `MaxPages` 时默认最多 100 页。尚未观察到结束信号就触及预算，或后续失败时，命令返回非零，并报告 `summary.completeness` 和保留的行。流式部分结果使用唯一数据路径及 `.meta.json` 元数据，工件描述通过 `completeness` 和 `metadataPath` 报告；既有完整目标文件保持不变。`log.export-analysis` 不自动翻页分析行，只有 `ResultStatus=complete` 才标记完整。

## 3. 分页

`tool exec --page-all` 请求分页结果的所有页，但仅在工具契约支持时可用。用 `tool describe <group.action>` 检查 `execution.supports_all`（或 describe 输出中的 `supports_all`）。如果 `supports_all` 为 `false`，该动作不支持 `--page-all`，必须手动翻页。

`summary.pagination.merged=true` 只表示已累积页面，不代表完整。服务端 `data.Total` 保持原值，`summary.itemCount` 表示已收集条数。检查 `summary.completeness` 中的 `complete`、`stopReason`、`pageCount`、`itemCount`、`resumable` 和可选的 `next`。存在 `next` 时，复用相同运行时选择器，以其 `operationId` 和 `input` 发起普通 `tool exec` 单页请求并关闭 page-all（`next.pageAll=false`）。

`workflow exec` 不消费 `execution.page.all` / `execution.page_all` 上下文控制。分页语义是工作流专属的；请遵循 `workflow describe` 中针对你所使用工作流的指导。

## 4. 不完整结果

SearchLogs 的 `ResultStatus=incomplete` 和 `time_out` 表示数据不完整，`error` 表示失败。SQL/分析结果只有 `ResultStatus=complete` 才完整；`ListOver` 仅用于纯检索的结束判断。不要将部分结果呈现为完整结果；检查完整性，并在合适时缩小时间范围或查询后再信任计数、行或未命中的判断。

## 5. 追踪与诊断工件

当 CLI 创建新的追踪路径时，它为目录请求 `0700`、为 JSONL 追踪文件请求 `0600`。已有的目录和文件可能保留其当前权限。

正常的结构化请求/响应追踪字段只包含头键（从不包含值）、查询键（从不包含值）以及请求和响应体的 SHA-256 哈希（从不包含原始字节）。`--trace-redact off` 会被接受和规范化，但不会禁用此强制的结构化字段脱敏，也不会输出原始的头、查询或体值。

`error_message` 字段直接存储传输错误字符串，其中可能包含完整的请求 URL 和查询值。永远不要将凭证或其他密钥放在查询参数中。将追踪文件视为敏感的诊断工件：保持其权限受限，仅在检查后再分享。

## 6. 错误恢复

`raw`、所有 `tool` 命令（包括 `list`/`describe`）、所有 `workflow` 命令（包括 `list`/`describe`）以及人类快捷方式信封组使用结构化错误信封（`status: failed`、`error` 对象）。其他非信封组以扁平 JSON 写入标准错误，字段顺序如下：`errorCode`、`errorMessage`、`requestId`、`statusCode`、`kind`、`hint`。

信封中的 `requestId`（或 `x-tls-requestid` 响应头）标识请求，可供支持和排障使用。报告问题时请捕获它。

错误大致分为几类：本地校验（契约形状、缺少必填字段）、认证（凭证解析、登录）、传输（网络、endpoint、DNS）和 TLS 服务响应（状态码、服务错误消息）。用 `--dry-run` 在网络调用前隔离本地校验错误，用 `--trace-dir` 捕获传输或服务错误的请求/响应交互。

## 7. 瘦客户端边界

CLI 校验本地契约形状（输入模式、必填字段）并将请求传输到 TLS API。服务器对权限、资源状态、查询语义和服务限制保持权威。成功的预执行或本地校验不保证请求会被服务接受。

## 8. 稳定的检索、直方图和分析规则

`log.search` 同时支持普通检索语法和 SQL/分析语法（`* | select ...`）。`HitCount` 只是当前响应窗口中返回的计数，不是整个窗口的总数。对于纯检索查询，`log.describe-histogram-v1` 可通过顶层 `TotalCount` 字段（在 CLI 信封中为 `data.TotalCount`）提供时间分布预览和更好的整个窗口命中估计。对于分析查询，`Context`、`Sort`、`Limit`、`Offset` 等体字段不会对分析行分页。用户显式编写 SQL `LIMIT`/`OFFSET` 的每次调用都是独立查询，不保证同一快照；不要为续取不完整响应凭空构造 offset。

如果 `ResultStatus=incomplete` 或 `time_out`，检查完整性，并在合适时缩小时间范围后再信任计数或行；`error` 表示失败。

## 9. Agent 技能与自动化

仅在用 `--help` 确认确切语法后，才使用 `skill list` 和 `skill install --name <name> --dir <dir>`。遵循 发现 → 描述 → 预执行 → 执行 的顺序：检查契约、本地校验、然后发送。

1.1.1 内置两个 Skill：

- `volclog-core`：通用的契约优先运行时、路由、交付和恢复模型。
- `tls-logcollector`：LogCollector 采集设计、非持久化样本校验、TLS 资源对账、Linux/Kubernetes 部署指导及端到端验收。

可只安装目标 Agent 所需的 Skill；省略 `--name` 则安装全部内置 Skill：

```bash
volclog skill list
volclog skill install --name volclog-core --dir <agent-skills-dir>
volclog skill install --name tls-logcollector --dir <agent-skills-dir>
```

### 9.1 Skill 生命周期与修改保护

所有生命周期命令都必须提供 `--dir`；重复使用 `--name` 可选择多个 Skill，省略则选择全部内置 Skill：

```bash
volclog skill status --dir <agent-skills-dir> [--name <name>]
volclog skill update --dir <agent-skills-dir> [--name <name>] [--force]
volclog skill uninstall --dir <agent-skills-dir> [--name <name>] [--force]
```

每个已安装 Skill 都有 `.volclog-skill.json` sidecar，字段包括 `schema_version`、`name`、`installed_version`、`source_digest` 和 `installed_digest`。digest 是基于排序后的相对 POSIX 路径与按长度分帧的文件内容计算的稳定 SHA-256，并排除该 sidecar；symlink 和特殊文件会被拒绝。`status` 完全离线，状态包括 `not_installed`、`current`、`outdated`、`modified`、`untracked` 和 `invalid_manifest`；版本不一致会与内容修改分开报告。

`current` 表示磁盘内容等于 `installed_digest` 且内置 source 等于 `source_digest`；`outdated` 表示内容未改动但内置 source digest 已变化；`modified` 表示磁盘内容不同于 `installed_digest`。缺少 sidecar 时为 `untracked`，sidecar 无法读取或内容不一致时为 `invalid_manifest`。

目标已存在时，`install` 默认失败，必须显式使用 `--force`。`update` 和 `uninstall` 默认保留 `modified`、`untracked` 和 `invalid_manifest` Skill；使用 `--force` 才会显式替换或删除。安装和更新会先构造完整临时目录，再通过同文件系统 rename 完成替换；替换失败时恢复旧目录。

### 9.2 版本与显式升级

`volclog version` 输出包含 `schema_version`、`version`、`edition`、`commit`、`catalog_digest`、`operation_count`、`public_operation_count` 和 `workflow_count` 的机器可读 JSON。`volclog --version` 继续保留文本兼容形式，供已有脚本使用。

升级只会在显式调用时执行，永不后台检查：

```bash
volclog upgrade --check
volclog upgrade --version 1.1.1
volclog upgrade --version 1.1.1 --yes
volclog upgrade --yes
```

检查和不带 `--yes` 的版本选择都不会写文件。npm 安装会委托 npm；独立二进制会校验发布 checksum，并使用原子替换。

`tls-logcollector` 是对 `volclog-core` 的补充而不是替代。它要求以当前 `tool describe` 契约为准，并在写入采集规则前使用非持久化的采集解析、索引分词预览和 Processor 调试接口。临时采集 LogCollector 自身日志的 POC 必须显式启用，因为宽泛或递归的自采集可能放大入流。

对于自动化：显式传入 `--profile`（或 `--secrets-file`），对大结果使用确定性文件输出（`--output-mode file --output-dir <dir>`），且永远不要在脚本中持久化明文凭证。

身份确定性：在静态 AK 模式下，完整的 `VOLCENGINE_ACCESS_KEY_ID` + `VOLCENGINE_ACCESS_KEY_SECRET` 环境对会绕过显式选择的配置档。打算使用静态配置档的自动化必须移除意外的静态凭证环境变量，或使用受控的完整 `--secrets-file` 作为预期的身份来源。动态提供者模式会忽略环境 AK/SK。确切的优先级见 [配置](3-Configuration_zh.md) 第 5 节。

Request ID 来源：对于普通的产生信封的命令，从信封中捕获 `requestId`。对于默认的强制文件 `workflow exec log.export` / `log.export-analysis`，标准输出只是固定的通知，数据工件没有 `requestId`。如果需要流式或多页导出的传输 Request ID，请启用受限的 `--trace-dir`，检查追踪文件，并遵循第 5 节中的追踪敏感性指导。

## 10. 通用排障

- 用 `volclog --profile <profile> doctor` 进行本地配置/凭证检查，用 `doctor --online` 进行最小的实时连通性检查。
- 如果命令因 `conflicting runtime selectors` 失败，请查看 [配置](3-Configuration_zh.md) 第 5.3 节中的身份选择器规则：显式选择器是可选的；任何配置档选择器与任何密钥文件选择器组合都会冲突；两个密钥文件选择器会冲突；全局/上下文配置档仅在名称不同时才冲突；重复相同的配置档名称是可接受的，但属于冗余。
- 如果 `--output table` 被拒绝，说明你在默认的 `volclog` agent 路径上；请使用 `--output json` 或 `--output jsonl`，或切换到 `volclog-human` 以使用支持 table 的特定快捷方式入口。
- 如果 `--output-mode file` 对 `tool exec`/`workflow exec` 在标准输出上没有产生信封，这是预期的：标准输出只包含固定的文件通知；请读取工件文件。

---

[← 上一篇：实用指南](5-Practical-Guide_zh.md) | [English](6-Advanced.md) | [下一篇：Human Shortcuts →](7-Human-Shortcuts_zh.md)
