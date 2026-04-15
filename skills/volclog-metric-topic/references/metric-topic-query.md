# Metric Topic Query

## 适用场景

- 处理 `QueryMetrics`
- 处理 `QueryMetricsRange`
- 处理 `GetLabels`、`GetLabelValues`、`GetSeries`
- 处理 `RemoteWrite`

## 必填输入

- PromQL/Prometheus 查询先确认查询语句或匹配条件
- `RemoteWrite` 只在用户明确说“推送指标”时才进入

## 可选参数触发词

- 说“PromQL”“Prometheus”“time series”时，走查询侧
- 说“当前值”“瞬时查询”时，优先 `QueryMetrics`
- 说“过去 1h/24h”“趋势”“query_range”“步长”时，优先 `QueryMetricsRange`
- 说“所有 label key”时，优先 `GetLabels`
- 说“某个 label 的值”时，优先 `GetLabelValues`
- 说“series 列表”“match[]”时，优先 `GetSeries`
- 说“remote write”“推送指标”时，优先 `RemoteWrite`
- 说“结果很多”“发文件给我”时，补 `--output-mode file`

## 字段联动/限制

- 先确认这是查询，不是资源 CRUD
- 不要把 PromQL 需求误路由到 `log search`
- `RemoteWrite` 是写入指标，不是查询
- 如果用户其实要的是资源管理，应该回到 `metric-topic list/get/create/modify/delete`

## 常见误用

- 把 metric topic 查询和资源管理混成一个流程
- 看见 `search` 就默认是日志检索
- 把 `RemoteWrite` 当成检索动作

## 下一步命令

```bash
volclog metric-topic search --describe
volclog api metric-topic QueryMetrics --describe
volclog api metric-topic QueryMetricsRange --describe
```
