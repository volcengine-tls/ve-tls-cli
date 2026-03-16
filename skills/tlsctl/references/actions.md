# Actions

本文件定义 `tlsctl` skill 支持的 action 清单、参数约束与危险等级（用于 plan/apply）。

约定：
- 输入以 `account + region + action + args` 为主
- 参数名使用下划线风格：例如 `topic_id`、`project_id`、`from_ms`
- 时间一律建议使用毫秒：`from_ms/to_ms/start_ms/end_ms/time_ms`
- 仅当 action 被声明在本表中时才允许执行（白名单）

## 全局字段

- `account`：账号标识（逻辑名），用于从本地 profiles 里匹配 profile
- `region`：区域，例如 `cn-beijing`、`ap-singapore-1`
- `action`：见下方列表
- `args`：action 相关参数
- `dry_run`：危险操作默认 `true`（plan）；确认后 `false`（apply）
- `confirm_token`：危险操作 apply 需要携带（来自 plan 输出）

## Project

**project.list**（read-only）
- args：可选 `page_number/page_size/project_name/project_id/fuzzy_search_key/description/is_full_name/iam_project_name/tags/favourite/topic_types`

**project.get**（read-only）
- 必填：`project_id`
- 可选：`topic_types`

**project.create**（write, confirm）
- 必填：`project_name`
- 可选：`description/iam_project_name/region/tags/request`

**project.modify**（write, confirm）
- 必填：`project_id`
- 可选：`project_name/description/favourite/request`

**project.delete**（destructive, confirm）
- 必填：`project_id`

## Topic

**topic.list**（read-only）
- 互斥：`topic_name` 与 `topic_id` 不可同时提供
- args：可选 `project_id/topic_name/topic_id/page_number/page_size/cursor/region/project_name/fuzzy_search_key/description/tags/favourite/order_by_project`

**topic.get**（read-only）
- 必填：`topic_id`

**topic.create**（write, confirm）
- 必填：`project_id`、`topic_name`
- 可选：`description/ttl/shard_count/auto_split/max_split_shard/enable_tracking/disable_tracking/metering_mode/log_public_ip/no_log_public_ip/enable_hot_ttl/disable_hot_ttl/hot_ttl/cold_ttl/archive_ttl/time_key/time_format/encrypt_conf/tags/request`

**topic.modify**（write, confirm）
- 必填：`topic_id`
- 可选：同 create（去掉 project_id），以及 `favourite/no_favourite/no_auto_split`

**topic.delete**（destructive, confirm）
- 必填：`topic_id`

## MetricTopic

**metric_topic.list**（read-only）
- 互斥：`topic_name` 与 `topic_id` 不可同时提供
- args：可选 `page_number/page_size/project_id/project_name/topic_name/topic_id/region/fuzzy_search_key/description/tags/favourite/order_by_project/no_order_by_project`

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

**metric_topic.search**（read-only）
- 必填：`topic_id`、`query`
- 可选：`from_ms/to_ms/limit/context/sort/highlight/accurate_query/no_accurate_query/must_complete/no_must_complete/offset/max_pages/request`

## MetricTopic Prom

**metric_topic.prom.query**（read-only）
- 必填：`topic_id`、`query`
- 可选：`time_ms/time`（建议 `time_ms`）

**metric_topic.prom.query_range**（read-only）
- 必填：`topic_id`、`query`、`start_ms`、`end_ms`、`step`

**metric_topic.prom.series**（read-only）
- 必填：`topic_id`、`start_ms`、`end_ms`、`match`

**metric_topic.prom.labels**（read-only）
- 必填：`topic_id`
- 可选：`start_ms/end_ms/match`

**metric_topic.prom.label_values**（read-only）
- 必填：`topic_id`、`label_name`
- 可选：`start_ms/end_ms/match`

## Index

**index.get**（read-only）
- 必填：`topic_id`

**index.create**（write, confirm）
- 必填：`topic_id`、`body`
- 说明：`body` 建议用 `file://` 指向 JSON 文件

**index.modify**（write, confirm）
- 必填：`topic_id`、`body`
- 说明：同 create

## Log

**log.search**（read-only）
- 必填：`topic_id`、`query`
- 可选：`from_ms/to_ms/limit/context/sort/highlight/accurate_query/no_accurate_query/must_complete/no_must_complete/offset/request`

**log.export**（destructive-ish, confirm）
- 必填：`topic_id`、`query`
- 可选：同 `log.search`，额外支持：`max_pages`
- 默认输出：`jsonl`

