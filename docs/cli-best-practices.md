# CLI 参数最佳实践与场景指南（volclog）

这份文档只讲稳定的运行时语义、参数边界、输出行为、错误读取方式，以及凭证与诊断策略。

文档边界：

- 想看产品入口、安装和 Quick Start：回到 [README.md](../README.md) 或 [README_CN.md](../README_CN.md)
- 想看端到端实战链路：看 [cli-practical-guide.md](cli-practical-guide.md)
- 想看 full 版 shortcut：看 [cli-human-shortcuts.md](cli-human-shortcuts.md)

如果你只记住最少几条规则：

1. Agent/自动化默认先走 `tool / workflow / raw`
2. 写操作先 `--dry-run`
3. 大结果优先 `--output-mode file --output-dir <writable-dir>`
4. 失败时先读单层 `error`
5. `region` 必须显式提供，不从 endpoint 反推

---

## 1. 全局参数与输入边界

推荐统一写法：

```bash
volclog [global flags...] <group> <command> [args]
```

虽然 CLI 现在支持全局参数任意位置解析，但脚本、文档和 Agent 最好仍统一采用前置写法，减少歧义。

全局参数负责“这次执行怎么跑”，而不是“业务语义是什么”：

- 身份/环境：`--profile`、`--secrets-file`
- 输出与落盘：`--output`、`--output-mode`、`--output-dir`、`--jmes-filter`
- 排障：`--trace-dir`、`--trace-redact`
- 预执行：`--dry-run`

业务语义参数仍属于具体命令本身，例如：

- `tool exec ... --input ...`
- `workflow exec ... --input ...`
- `raw --method ... --path ... --body ...`

### 1.1 `tool` / `workflow` / `raw` 的输入约定

- `tool exec` / `workflow exec` 使用 JSON `--input`，运行时控制放在 JSON `--context`
- `raw` 使用 `--method`、`--path`、`--query`、`--header`、`--body`
- 为了降低从 `tool/workflow` 迁移到 `raw` 时的坑，`raw` 也接受 `--input` 作为 `--body` 的兼容别名
- 但 `raw --body` 与 `raw --input` 不能同时传

输入介质统一支持：

- inline JSON
- `file://...`
- `-`（stdin）

### 1.2 `profile`、`secrets-file` 与 `region`

硬规则：

- `region` 必须显式提供
- 不从 endpoint / 域名推导 region
- `--profile` / `context.profile` 与 `--secrets-file` / `context.secrets_file` 是互斥的 runtime selector

也就是说，下面这种组合会 fail fast：

```bash
volclog --profile prod --secrets-file ./.env tool exec ...
```

或者：

```bash
volclog --profile prod workflow exec ... --context '{"secrets_file":"file://creds.env"}'
```

无状态执行推荐顺序：

1. 先确定目标环境或 profile 名称
2. 如需临时注入凭证，再提供一次性的 `--secrets-file`
3. 不要把大范围环境变量直接灌进整个会话

---

## 2. 输出与交付

### 2.1 `--output`

- `json`：默认，适合结构化对象输出
- `jsonl`：适合大量逐行结果
- `table`：只属于 full 版的人类友好输出，不是 `volclog-agent` 主路径格式

### 2.2 `--output-mode` 与 `--output-dir`

当结果可能很大时，优先：

```bash
volclog --output-mode file --output-dir ./out \
  workflow exec log.export --input file://req.json
```

当前语义：

- `stdout`：正常标准输出
- `file`：强制落文件
- 未强制 file 但结果过大时，CLI 可能自动进入 `file_auto`

此时 stdout 只给固定提示与文件路径，完整 envelope 会写入 `output_dir` 下由 CLI 自动生成的文件中。

关键点：

- 调用方只提供目录，不自己指定文件名
- 没有可写 `output_dir` 时，auto spill 会直接报错并提示补 `--output-dir <writable-dir>`
- `summary.outputMode` 表示调用方意图
- `summary.deliveryMode` 表示运行时最终交付方式

### 2.3 如何理解 `deliveryMode`

- `stdout`：结果留在 stdout
- `file_forced`：调用方显式要求 file
- `file_auto`：CLI 因结果过大自动改走 file

对于 Agent/自动化：

- surface 选择由你决定：`tool / workflow / raw`
- stdout 还是 file 由 CLI 的 `summary.deliveryMode` 决定

不要在 skill 或脚本里重复实现第二套“是否该导出文件”的路由逻辑。

---

## 3. 过滤与错误读取

### 3.1 `--jmes-filter`

`--jmes-filter` 作用于完整 CLI envelope，而不是原始服务端结果。

合法路径示例：

```bash
volclog tool exec project.describe-projects --jmes-filter "data.Projects[0].ProjectId"
volclog tool exec project.describe-projects --jmes-filter "summary.deliveryMode"
volclog tool exec project.describe-projects --jmes-filter "error"
```

注意：

- 命中存在但值为 `null` 的字段时，stdout 直接输出 `null`，这仍然是成功结果
- `filter matched no value` 才表示路径写错了
- `--jmes-filter` 是 stdout-only；不能和 file delivery 同时使用

### 3.2 `execution.projection`

`execution.projection` 与 `--jmes-filter` 不是一回事：

- `--jmes-filter`：作用于完整 envelope
- `execution.projection`：作用于 raw result，再由 CLI 重新包 envelope

因此：

- `summary.deliveryMode`、`error.code` 这类 envelope 字段只能给 `--jmes-filter`
- 原始响应里的字段才适合放进 `execution.projection`

### 3.3 失败结果怎么读

失败时统一读取单层 `error` 对象：

1. `error.source`
2. `error.kind`
3. `error.code`
4. `error.message`
5. `error.details`
6. `error.requestId`
7. `error.statusCode`

解释：

- `error.source=cli|upstream`
- `error.kind` 决定恢复方向
- `error.code` 是最适合脚本和 Agent 判断的稳定错误码
- `error.details` 只有在确实有额外结构时才出现

对于上游服务端错误：

- `error.code` 会直接是业务错误码，例如 `ProjectAlreadyExist`
- 如果服务端错误文案里还嵌了一层 JSON，CLI 会尽量提炼到 `error.details`
- 不要再把 `error.message` 当 JSON 二次解析

---

## 4. 诊断与复盘

推荐流程：

```bash
volclog doctor
volclog --trace-dir ./.volclog/traces --trace-redact strict tool exec ...
```

其中：

- `doctor`：先检查配置、鉴权、endpoint、region 与环境状态
- `--trace-dir`：生成 trace 工件用于复盘
- `--trace-redact strict`：优先保证敏感字段被脱敏

当你面对的是“为什么这次调用没按预期发出去 / 为什么返回 400/403/500”这类问题时，先跑 `doctor`，再开 trace，而不是直接猜 body。

---

## 5. 常见稳定规则

### 5.1 Search / Histogram / Analysis

- `log.search` 同时支持普通检索和 SQL/analysis `Query`
- `log.describe-histogram-v1` 只适合纯检索 query 的时间分布预览与总量估计
- `log.export-analysis` 沿用 `SearchLogs` 的 SQL/analysis `Query` 语法，但它的定位是“大结果分析行导出”，不是交互式预览

计数语义：

- `HitCount` 只是当前 `SearchLogs` 响应窗口里的数量
- 对纯检索 query，`Histogram.TotalCount` 更适合看整窗总量
- 对分析 query，不要用 histogram 当分析行总数；要么直接读分析结果，要么在 SQL 里写 `count(*)`

### 5.2 `ResultStatus=incomplete`

`ResultStatus=incomplete` 表示服务端只返回了部分扫描结果。

这在以下场景都可能出现：

- 普通检索
- 检索 + 分析
- 纯分析

恢复动作：

- 缩小时间范围
- 重新执行
- 在拿到 `complete` 之前，不要信任计数、空结果或桶分布

### 5.3 `page.all`

- `page.all` 只在契约明确报告 `execution.supports_all=true` 时可用
- 它提高的是完整性，不是压缩输出
- 开启后 payload 可能更大，不会更小

---

## 6. 凭证注入与多账号隔离

优先级建议：

1. 本地长期使用：profile
2. 无状态一次性执行：`--secrets-file`
3. 只在必要时再用 scoped env injection

示例：

```bash
volclog --secrets-file ./.env tool exec project.describe-projects
```

如果同一套 AK/SK 需要复用到多个 region / endpoint，优先用 profile + `cred-ref` 方案，而不是复制多份明文凭证配置。

---

## 7. 一句话索引

- 想看入口和安装：回 README
- 想看长链路实战：回 `cli-practical-guide`
- 想看 full 版 shortcut：回 `cli-human-shortcuts`
- 想看稳定 runtime 规则：留在这份文档
