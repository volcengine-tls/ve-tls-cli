# ve-tls-cli Unified Output Runtime Design

## 背景

当前 CLI 的输出语义仍然是分裂的：

- `tool` / `workflow` / `raw` 虽然已经逐步收口到统一 runtime，但 `payload`、`envelope`、`--jmes-filter`、file 落盘行为并不一致
- `--jmes-filter` 目前主要作用于原始结果，而不是统一的 envelope 视图
- `--output-file` 仍然存在，agent 需要自己决定文件名
- CLI 在未显式授权时仍可能回退到默认目录（如 `.volclog/output`）写文件，这与沙箱环境“仅部分目录可写”的前提相冲突
- 大结果处理目前对 agent 不够稳定：既不知道结果会不会爆 token，也不知道什么时候应该主动落盘

用户已经明确了新的目标：

1. 不同 surface 可以有不同 `payload`
2. 默认必须共享同一个 envelope、同一个 filter 语义、同一个 file 语义
3. agent 不负责指定文件名，只负责提供可写 `output_dir`
4. `tool/workflow/raw` 不再保留 `--output-file`
5. 自动落盘时，stdout 不再返回另一份 JSON envelope，而是固定文本提示文件路径
6. 写入文件的内容必须是完整 envelope，便于 agent 二次读取时拿到完整上下文

## 一句话结论

把输出处理收敛成一个统一运行时层：surface 只决定 `payload`，无 filter 时小结果走 stdout 完整 envelope，大结果在显式提供的可写 `output_dir` 下自动落盘并返回固定文本提示；有 `--jmes-filter` 时统一对完整 envelope 过滤，直接 stdout 返回投影结果，不再自动落盘。

## Agent-friendly 原则

本设计遵循一个总原则：

- Agent 友好的标准是：最小推测、确定性契约、机器可执行的错误、稳定可恢复路径。

落实到 unified output runtime 中，意味着：

- 不让 Agent 猜输出会是 envelope、纯文本还是文件路径三种混合形态。
- 不让 Agent 猜 `--jmes-filter` 到底作用于 raw payload 还是 envelope。
- 不让 Agent 猜大结果应该主动传什么文件名、CLI 又会不会偷偷写默认目录。
- 遇到不可写目录、file/filter 冲突、结果不完整等情况时，返回明确、可执行的错误，而不是隐式降级或静默兜底。

## 非目标

本设计不包含：

- 新的 workflow plane 设计
- 重新设计 shortcut payload 结构
- 引入通用的大型 output DSL
- 自动推断可写目录
- 在本轮中保证历史 `--output-file` 完全无痛兼容
- 把“服务端可能命中中间结果缓存”写成稳定契约

## 核心不变量

### 1. Surface 只决定 payload

- `tool`、`workflow`、`raw` 可以有不同 `data` 形状
- `payload` 由各自 surface 负责构建
- 一旦进入统一输出运行时，后续 envelope/filter/file 规则必须一致

### 2. 默认 envelope 统一

在“无 filter 且未自动落盘”路径下，`tool` / `workflow` / `raw` 必须共享同一 envelope 结构。

最低要求：

- `status`
- `action`
- `requestId`
- `summary`
- `artifacts`
- `data`
- `error`

### 3. 文件内容统一

- 所有自动或手动 file 输出，写入文件的内容都必须是完整 envelope
- 不允许只写 `data`
- 不允许对不同 surface 写不同的文件格式语义

### 4. filter 语义统一

- `--jmes-filter` 一律作用于完整 envelope
- 常见写法仍然是 `data.xxx`
- 也允许过滤：
  - `summary.deliveryMode`
  - `artifacts[0].path`
  - `error.errorCode`
  - 组合投影，如 `{path: artifacts[0].path, count: summary.itemCount}`

### 5. 自动落盘只能发生在无 filter 路径

- 一旦显式提供 `--jmes-filter`
- 输出结果就不再保证保留 envelope 形状
- 因此不再触发自动落盘
- `--jmes-filter` 结果只走 stdout

## 新的输出模型

### 输出模式

统一 runtime 识别三种交付语义：

- `stdout`
- `file_auto`
- `file_forced`

这里不要求所有 surface 暴露同样的 flag 或 context 字段，但最终 envelope 中的交付语义必须一致。

### `summary` 新字段

建议引入以下结构：

```json
{
  "summary": {
    "deliveryMode": "stdout|file_auto|file_forced",
    "totalBytes": 2048576,
    "itemCount": 15234
  }
}
```

字段语义：

- `deliveryMode`
  - `stdout`：完整结果直接输出到 stdout
  - `file_auto`：原本默认希望 stdout 返回，但因结果过大自动转移到文件
  - `file_forced`：调用方主动要求 file 输出
- `totalBytes`
  - 完整 envelope 的估算或实际字节数
- `itemCount`
  - 对数组、列表、记录集合的统一计数
  - 不是所有 payload 都天然是“行”，因此不使用 `totalRows`

不推荐单独只用 `autoRouted: true/false`，因为布尔值无法区分 `stdout`、`file_forced` 等不同来源。

## `output_dir` 约束

### 原则

- CLI 绝不在未显式授权的目录写文件
- 不再依赖 `.volclog/output` 作为自动落盘默认目录
- agent 不指定文件名，只指定可写目录
- 文件名由 CLI 生成

### 传递方式

不同 surface 可以有不同的输入载体，但语义必须一致：

- `tool` / `workflow`
  - 推荐由 `context.execution.output.dir` 承载
  - 强制 file 推荐由 `context.execution.output.mode=file` 表达
- `raw`
  - 可保留 surface-local 的 `--output-dir`
  - 强制 file 可由 surface-local 的 `--output-mode file` 表达
- shortcut
  - 可在后续复用同一 runtime，但不要求与 agent surface 同步上线

这里的重点不是 flag 是否相同，而是：

- 调用方只声明“这是一个可写目录”
- CLI 自己决定文件名并回传路径

### 默认目录与配置回退

为了避免 CLI 在沙箱中写到未授权目录，以下来源不再参与自动落盘目录选择：

- `.volclog/output`
- `VOLCLOG_OUTPUT_DIR`
- project config 中的默认 `output_dir`

这些值最多只能作为人类显式配置 file 输出时的辅助信息，不能再作为 agent runtime 的自动落盘后备目录。

### 文件名规则

由 CLI 统一生成，建议包含：

- surface
- action 或 workflow id
- UTC timestamp
- 必要时的短 hash

例如：

- `tool-log.search-2026-04-17T10-30-11.123Z.json`
- `workflow-log.export-analysis-2026-04-17T10-31-02.221Z.json`

## 自动落盘行为

### 默认路径

无 filter 时统一走：

1. 先构建完整 envelope
2. 估算完整 envelope 的输出规模
3. 小结果：stdout 返回完整 envelope
4. 大结果：
   - 若提供了可写 `output_dir`，则写入完整 envelope 文件
   - stdout 只返回固定文本提示
   - 文件中的 envelope 里 `summary.deliveryMode=file_auto`
5. 若结果过大但没有可写 `output_dir`：
   - 不得擅自写默认目录
   - 返回明确的 usage/runtime error，提示缺少可写输出目录

### 早期失败路径

如果 runtime 在 surface 执行前就已经知道“本次调用不适合 stdout，且需要 file 交付”，应在真正发出远端调用前先校验 `output_dir` 是否可用，避免无效 API 调用。

适用场景：

- 调用方显式要求 `file_forced`
- workflow 或 contract 元数据已经明确声明这是 file-first / large-result-first 路径
- 运行前已经能判定 stdout 不应作为主交付路径

不应承诺的场景：

- 纯 `auto` 模式下的普通调用，运行前通常无法可靠知道结果是否一定会超阈值
- 这类调用仍需要在结果组装后再根据实际大小决定是否自动落盘

因此，早期失败不是“预知所有大结果”，而是“对已经声明或已知需要 file 交付的路径，先检查目录可写性”。

### 结果大小判定

不建议只用“行数”判定，应至少综合：

- 完整 envelope 的字节数
- `data` 的 item 数量
- 单条元素大小的极端值保护

阈值可配置，但默认由 CLI runtime 内置，不依赖 agent 决策。

## stdout 固定提示

### 自动落盘

stdout 固定输出：

```text
结果过大，已写入文件。
文件: /path/to/result.json
```

### 主动 file 模式

stdout 固定输出：

```text
结果已写入文件。
文件: /path/to/result.json
```

### 约束

- 文案必须固定，不允许运行时随机变化
- 文件路径必须单独一行
- 前缀固定为 `文件: `
- stdout 不再输出第二份 JSON envelope

这么做的目的是让 agent 不必先解析一层 preview envelope，再决定是否继续读文件。

## `--jmes-filter` 新语义

### 作用对象

`--jmes-filter` 统一作用于完整 envelope，而不是原始 payload。

示例：

- `data.Topics[0].TopicId`
- `summary.deliveryMode`
- `artifacts[0].path`
- `{status: status, count: summary.itemCount}`

### 执行顺序

1. surface 先产出 payload
2. 统一 runtime 组装完整 envelope
3. 如果存在 `--jmes-filter`：
   - 对完整 envelope 执行过滤
   - 直接 stdout 返回过滤结果
   - 不再自动落盘
4. 如果不存在 `--jmes-filter`：
   - 再进入自动落盘判定

### 原因

如果过滤结果已经不再保持 envelope 结构，再把它自动落盘会让 agent 难以判断文件里的内容到底是完整结果还是投影结果，因此应直接禁止。

## 文件 envelope 与 stdout schema 一致性

- 自动或手动落盘写入的完整 envelope，字段结构必须与 stdout 成功路径保持同一 schema
- agent 读取文件后，应能沿用与 stdout 相同的 JSON 解析逻辑
- 这里强调的是“同一 envelope schema”，不是“可直接作为下一次 `tool exec --input` 原样传回”

## 文档与 help 分层

### CLI help 负责的内容

应前移到必走路径：

- 输出模式的基本语义
- `output_dir` 的要求
- 自动落盘的固定 stdout 提示
- `--jmes-filter` 作用于 envelope
- 有 filter 时不自动落盘

重点位置：

- `tool exec -h`
- `workflow exec -h`
- `raw -h`

### `tool describe` / `workflow describe` 负责的内容

只保留 action / workflow 级差异提示，例如：

- 某个 action 是否天然大结果
- 某个 action 的 `page.all` / `supports_all`
- 某个 action 的 `ResultStatus=incomplete`

不再重复通用 output/filter/file 规则。

### skill 负责的内容

skill 只补策略层说明，例如：

- 什么情况下接受 stdout 小结果
- 什么情况下看到固定文本提示后应立即读文件
- 大结果导出与交互式分析的选择策略

## 兼容性策略

### 明确移除

- `tool/workflow/raw` 的 `--output-file`
- 自动落盘回退到默认目录
- `--jmes-filter` 作用于原始 payload 的旧语义

### 可接受的兼容层

如果短期必须过渡：

- 旧的 `artifact.path` / `output_file` 输入可在解析层暂时兼容
- 但对外 help、README、skill、contract 中不再推荐

兼容层必须有明确下线目标，不能成为长期真相源。

## 验收标准

### Runtime

- 小结果时，`tool/workflow/raw` 默认 stdout 返回完整 envelope
- 大结果时，只有在显式提供可写 `output_dir` 时才自动落盘
- 自动落盘时，stdout 只返回固定文本提示
- 自动/主动 file 模式写入的文件都包含完整 envelope
- 文件内 `summary.deliveryMode` 能区分 `file_auto` 与 `file_forced`

### Filter

- `--jmes-filter` 对 envelope 生效
- `data.xxx`、`summary.xxx`、`artifacts.xxx`、`error.xxx` 都可过滤
- 存在 `--jmes-filter` 时不自动落盘

### Docs

- README / 最佳实践 / 实战指南 / skill 与实现语义一致
- 不再出现 `--output-file`
- 不再宣称默认会写 `.volclog/output`

## 风险

### 1. 兼容 breakage

已有脚本可能依赖：

- `--output-file`
- file 模式 stdout 返回路径但文件内容不是完整 envelope
- `--jmes-filter` 作用于原始 payload

需要通过 targeted tests 和文档迁移说明显式收口。

### 2. `raw` 的心智变化

`raw` 用户可能更习惯直接对原始 payload 过滤。统一改为 envelope 过滤后，文档必须明确告诉用户：

- 过去写 `Total`
- 新语义下需要写 `data.Total`

### 3. 自动落盘阈值调优

如果阈值过低，会导致 agent 经常读文件；过高又会重新打爆 token。需要保留阈值集中定义与回归测试。

### 4. `--jmes-filter` 的 breaking change

把 `--jmes-filter` 从“原始结果过滤”切到“envelope 过滤”是显式行为变更，必须在 help、README、最佳实践、release notes 中明确标记。

本设计不建议同时引入长期兼容的 `--jmes-filter-legacy`：

- 双语义会重新把 runtime 做分裂
- 用户和 agent 都需要学习两套过滤目标
- 这与本轮统一 contract 的目标相冲突

默认策略应是：

- 明确文档迁移
- 明确示例迁移
- 必要时在发布说明中单独列为 breaking change

## 建议实施顺序

1. 先冻结新的 runtime contract
2. 再统一 `tool/workflow/raw` 的输出路径
3. 再清理 docs/help/skill
4. 最后补 acceptance 与黑盒回归
