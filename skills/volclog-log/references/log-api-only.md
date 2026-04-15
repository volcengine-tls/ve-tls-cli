# Log API Only Index

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


这个 reference 只做索引，不承载详细流程。

## 优先看这些专题

- 分析任务 / 字段快速分析：看 [log-analysis-jobs.md](log-analysis-jobs.md)
- 下载任务：看 [log-download.md](log-download.md)
- 保存检索 / 收藏：看 [log-saved-search.md](log-saved-search.md)
- 归档检索：看 [log-archive-search.md](log-archive-search.md)
- WebTracking / Kafka：看 [log-webtracking-kafka.md](log-webtracking-kafka.md)
- 会话答案 / 附件 / LogApp：看 [log-session-logapp.md](log-session-logapp.md)

## 仍然放在这里的零散动作

- `DescribeCursorTime`
- `DescribeLogContext`
- `DescribeHistogram`
- `DescribeHistogramV1`
- `Statistics`
- `DescribeLatestLog`
- `SearchFullTexts`
- `SearchDocIDs`
- `DescribeLogReduceWildcardSummaries`

## 使用方式

- 用户先说清动作族，再看对应专题
- 如果专题里仍不够，再转 `volclog-api-explorer`
- 不要把这里当成完整 playbook
