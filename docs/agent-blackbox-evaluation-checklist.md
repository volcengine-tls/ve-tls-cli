# Agent Black-Box Evaluation Checklist

本清单用于独立评测大模型在 `ve-tls-cli` 上的 agent 使用质量。目标不是验证内部实现细节，而是验证模型在只看 CLI/help/skill 的前提下，能否稳定走出正确的调用路径、读懂契约、消费输出、处理错误并完成恢复。

## 1. 评测原则

- 只做黑盒评测，不给模型源码解释。
- 优先使用 `volclog`；只有当评测明确覆盖 human 版差异时才使用 `volclog-human`。
- 评测时默认安装 `volclog-core` skill。
- 除非场景明确要求，不允许模型使用 human shortcut。
- 评测重点是：
  - 是否选对 surface：`tool / workflow / raw`
  - 是否理解 `describe -> exec` 契约
  - 是否理解统一 envelope、`deliveryMode`、`--jmes-filter`
  - 是否能用 `error.kind/code/message/details` 恢复
  - 是否能分清 `log.search / log.describe-histogram-v1 / log.export-analysis / log.ingest / tool log.put`

## 2. 评测前提

评测环境至少应提供：

- 一个可执行的 `volclog` 二进制
- 已安装的 `volclog-core` skill
- 至少一个可用 profile，或宿主按次注入 `--secrets-file`
- 一个明确可写的输出目录，用于 `--output-dir`
- 如果做在线评测：可访问的 TLS endpoint 与最小可用资源

建议在评测说明里同时明确：

- 可写输出目录路径
- 当前允许使用的 profile 名称或逻辑环境标签
- 是否允许真实写操作；若不允许，应要求模型先 `--dry-run`

## 3. 通过标准

一轮评测至少满足以下条件才算“通过”：

- 主要路径走 `tool / workflow / raw`，而不是 human shortcut
- 在执行前先读 `tool describe` 或 `workflow describe`
- 对大结果不盲目全打 stdout，能理解 `deliveryMode`
- 对失败结果能优先读 `error.kind/code/message/details`
- 不把 histogram 当分析结果计数器
- 不把 `workflow log.ingest` 和 `tool log.put` 混为一谈
- 不把 `--jmes-filter` 和 `execution.projection` 混为一谈
- 不把 profile、secrets_file、env creds 的优先级关系理解错

## 4. 场景清单

下面每个场景都建议以“自然语言任务 -> 模型执行 -> 结果核对”的方式评测。

### A. Surface 选择

1. 公开 API 发现
- 用户任务：列出某个 group 下可创建的资源
- 期望行为：先 `tool list <group> --verb create`
- 失败信号：直接猜 action；直接走 shortcut；直接走 raw

2. 本地日志导入
- 用户任务：把一份 `jsonl` 文件写入某个 topic
- 期望行为：选择 `workflow log.ingest`
- 失败信号：把任务路由到 `tool log.put`

3. 明确 transport 调用
- 用户任务：我已经知道 method/path，只想原样调用
- 期望行为：选择 `raw`
- 失败信号：仍尝试从 tool/workflow 兜圈子

### B. Contract 使用

4. describe 优先
- 用户任务：创建或修改一个资源
- 期望行为：先 `describe`，再构造 `--input`
- 失败信号：直接盲猜字段名和 shape

5. workflow 输入方式
- 用户任务：执行 `workflow log.ingest`
- 期望行为：识别它走 `--input` JSON/文件，而不是把 `TopicId` 等业务字段误当成 `workflow exec` 顶层 flags
- 失败信号：直接拼 `workflow exec log.ingest --topic-id ...`

### C. 输出与过滤

6. 大结果自动落盘
- 用户任务：执行可能产生大结果的查询
- 期望行为：能理解没有 `--output-dir` 时需要补一个可写目录
- 失败信号：不知道为什么报错；反复重试同一命令

7. `deliveryMode` 语义
- 用户任务：读取大结果输出
- 期望行为：区分 `stdout / file_auto / file_forced`
- 失败信号：把 `file_auto` 当成 surface 选择错误，或不去读文件

8. `--jmes-filter`
- 用户任务：只取 `data`、`summary` 或 `error` 的一部分
- 期望行为：知道 `--jmes-filter` 作用于完整 envelope
- 失败信号：把它当成 raw result filter

9. `null` 过滤边界
- 用户任务：在成功 envelope 上取 `error`
- 期望行为：接受 stdout 直接返回 `null`
- 失败信号：把 `null` 当成过滤失败

### D. 错误恢复

10. flat error object
- 用户任务：处理一次失败结果
- 期望行为：优先读 `error.kind/code/message/details`
- 失败信号：继续解析旧的嵌套错误体，或试图二次 JSON 解析 `error.message`

11. validation 恢复
- 用户任务：故意漏掉必填字段
- 期望行为：读到 `kind=validation`，回到 `describe` 或 `input_encoding_hint`
- 失败信号：把它当作服务端错误或网络错误

12. profile 冲突
- 用户任务：同时给全局 `--profile` 和 `context.profile`
- 期望行为：识别这是冲突并 fail fast
- 失败信号：以为其中一个会静默覆盖另一个

### E. 日志检索 / 分析 / 直方图

13. `log.search` vs `log.export-analysis`
- 用户任务：交互式分析少量结果
- 期望行为：先走 `log.search`
- 失败信号：只要看到 SQL 就直接路由到 `export-analysis`

14. histogram 边界
- 用户任务：想知道纯检索的时间分布
- 期望行为：先考虑 `log.describe-histogram-v1`
- 失败信号：直接把 histogram 用在分析结果计数

15. `ResultStatus=incomplete`
- 用户任务：遇到 incomplete 结果
- 期望行为：缩小时间窗重试
- 失败信号：把 incomplete 当完整结果继续推理

16. `HitCount` vs `Histogram.TotalCount`
- 用户任务：判断总量
- 期望行为：知道 `HitCount` 不是整窗总数；纯检索时 `Histogram.TotalCount` 更接近整窗总数
- 失败信号：直接拿 `HitCount` 当全量统计

### F. 双发行物边界

17. agent 版主入口
- 用户任务：正常 agent 流程
- 期望行为：使用 `volclog`
- 失败信号：回到 `volclog-human` shortcut 或被 human 版 help 误导

18. human shortcut 不可见
- 用户任务：在 agent 版中尝试 `project/topic/log` shortcut
- 期望行为：知道这些不属于 agent 主路径
- 失败信号：持续尝试 shortcut

## 5. 评分建议

建议每轮评测从以下 5 个维度打分，每项 `0-2` 分：

- 路由正确性：是否选对 `tool / workflow / raw`
- 契约遵循：是否先 `describe`，是否按 `input/context` 语义执行
- 输出消费：是否正确处理 `deliveryMode`、文件输出、`--jmes-filter`
- 错误恢复：是否读懂 `error.kind/code/details` 并采取正确下一步
- 日志语义：是否分清 `search / histogram / export-analysis / ingest / tool put`

总分 `10` 分。建议：

- `9-10`：可用于主评测
- `7-8`：可继续迭代 skill 或 help 后复测
- `<=6`：说明 CLI/skill 仍有明显歧义或模型主路径未成型

## 6. 常见误判

- 把 skill 当成契约真相源，而不先读 `describe`
- 把 `workflow log.ingest` 当成 `tool log.put` 的别名
- 把 `deliveryMode=file_auto` 当成 surface 路由错误
- 把 `--jmes-filter` 当成 raw result 过滤器
- 把 `Histogram.TotalCount` 用在分析查询上
- 把 `ResultStatus=incomplete` 当成完整证据
- 看到 SQL 就直接走 `export-analysis`

## 7. 建议使用方式

- 每次评测尽量只改一个变量：
  - 只换 skill
  - 或只换 help
  - 或只换 CLI 版本
- 保留模型完整轨迹，尤其要记录：
  - 第一次选错 surface 的位置
  - 第一次误解 envelope / error / deliveryMode 的位置
  - 第一次被 skill 文案误导的具体句子
- 黑盒报告最终应沉淀成三类结论：
  - CLI 契约缺失
  - skill 语义重复或误导
  - 模型自身推理失误
