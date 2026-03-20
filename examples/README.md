# volclog examples

本目录提供可直接配合 `volclog` 使用的示例文件（用于 `file://...` 输入或 `--request file://...`）。

## 目录

- `create_project.json`：创建日志项目（Project）请求体示例
- `modify_project.json`：修改日志项目（Project）请求体示例
- `create_topic.json`：创建日志主题（Topic）请求体示例
- `modify_topic.json`：修改日志主题（Topic）请求体示例
- `create_metric_topic.json`：创建指标主题（MetricTopic）请求体示例
- `modify_metric_topic.json`：修改指标主题（MetricTopic）请求体示例
- `index.json`：索引创建/修改示例（TopicId 可由命令行注入）
- `search_logs.json`：SearchLogs 请求体示例
- `promql.txt`：PromQL 表达式示例（用于 `metric-topic prom --query file://...`）
- `time.txt`：Prom 查询时间示例（用于 `--time file://...`）
- `match.json`：Prom `match[]` 的 JSON 数组示例（用于 `--match file://...`）
- `match.txt`：Prom `match[]` 的按行文本示例（用于 `--match file://...`）
