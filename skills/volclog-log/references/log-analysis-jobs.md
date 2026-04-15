# Log Analysis Jobs

## 适用场景
本页用于当前动作族的 API-only 场景；先把用户意图收敛到本页覆盖的动作，再决定是否继续看 shortcut 或 api-explorer。

## 必填输入
先确认本页“覆盖动作”里对应动作的主键或核心输入，例如 TopicId、RuleId、TaskId、Query、Cursor、Request body。

## 可选参数触发词
见本页后续的关键词触发、推荐命令或常见误用；如果用户只是泛泛描述，先按主键优先，不要提前补一堆筛选项。

## 字段联动/限制
以本页的参数约束、字段联动、任务状态或消费语义为准；字段多时先看 `--describe`，不要靠记忆拼。

## 常见误用
不要把本页动作混成普通检索、普通写入、普通删除或普通读详情；不要在主键没确认前直接执行高风险操作。

## 下一步命令
先执行本页给出的推荐命令；如果仍不够，再转对应的 `volclog-api-explorer` 或更细的 shortcut reference。


## 覆盖动作

- `CreateAnalysisJob`
- `DescribeAnalysisJob`
- `DescribeAnalysisJobs`
- `ModifyAnalysisJob`
- `DeleteAnalysisJob`
- `LogFieldQuickAnalyse`
- `PreviewDelimiterLog`

## 先用什么时候

- 用户说“分析任务”“异步分析”“字段快速分析”“预览分隔符解析”

## 参数约束

- 这些动作是分析辅助或分析任务管理，不替代普通 `log search`
- `DescribeAnalysisJob(s)` 依赖分析任务 ID 或任务筛选条件
- `PreviewDelimiterLog` 更适合验证分隔符效果，不是正式写入动作

## 关键词触发可选参数

- 说“异步跑分析”“分析任务状态”时，优先 `Create/DescribeAnalysisJob*`
- 说“某个字段值分布”“快速分析字段”时，优先 `LogFieldQuickAnalyse`
- 说“先看分隔符拆分效果”时，优先 `PreviewDelimiterLog`

## 常见误用

- 不要把分析任务当普通检索
- 不要把 `PreviewDelimiterLog` 当正式规则配置
- 不要在没有任务 ID 时直接修改或删除分析任务
