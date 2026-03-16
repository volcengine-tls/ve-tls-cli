# Actions（metric-topic）

本文件定义 `metric-topic` skill 可执行的 action、参数约束与危险等级（用于 plan/apply）。

通用约定：
- 输入：`account + region + action + args`
- 时间：优先使用毫秒时间戳 `*_ms`
- 危险操作：`create/modify/delete` 默认 `dry_run=true`，二次 apply 需要 `confirm_token`

## 指标主题管理（metric_topic.*）

**metric_topic.list**（read-only）
- 可选：`project_id/project_name/topic_name/topic_id/region/page_number/page_size/fuzzy_search_key/description/tags/favourite/order_by_project`
- 互斥：`topic_name` 与 `topic_id` 不可同时提供

**metric_topic.get**（read-only）
- 必填：`topic_id`

**metric_topic.create**（write, confirm）
- 必填：`project_id`、`topic_name`
- 可选：`description/ttl/shard_count/auto_split/max_split_shard/tags/request`

**metric_topic.modify**（write, confirm）
- 必填：`topic_id`
- 可选：`topic_name/description/ttl/auto_split/no_auto_split/max_split_shard/favourite/no_favourite/request`

**metric_topic.delete**（destructive, confirm）
- 必填：`topic_id`

## 指标主题日志检索（metric_topic.search）

**metric_topic.search**（read-only）
- 必填：`topic_id`、`query`
- 可选：`from_ms/to_ms/limit/context/sort/highlight/accurate_query/no_accurate_query/must_complete/no_must_complete/offset/max_pages/request`

## Prom API（metric_topic.prom.*）

**metric_topic.prom.label_values**（read-only）
- 必填：`topic_id`、`label_name`
- 常用：`label_name="__name__"` 用于列出 metric names（替代 “list-metric-names”）
- 可选：`start_ms/end_ms/match`

**metric_topic.prom.series**（read-only）
- 必填：`topic_id`、`start_ms`、`end_ms`、`match`
- 用途：列出匹配 series 的 label set（可用于推断“该指标有哪些 label”）

**metric_topic.prom.labels**（read-only）
- 必填：`topic_id`
- 可选：`start_ms/end_ms/match`
- 用途：列出 label 名（可能是全局 label，不一定只属于某个指标）

**metric_topic.prom.query**（read-only）
- 必填：`topic_id`、`query`
- 可选：`time_ms`（推荐）或 `time`

**metric_topic.prom.query_range**（read-only）
- 必填：`topic_id/query/start_ms/end_ms/step`

