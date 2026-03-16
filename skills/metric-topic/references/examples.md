# Examples（metric-topic）

## 1) 定位 TopicId（按名称）

```text
account=acctA region=cn-beijing action=metric_topic.list project_name=<ProjectName> topic_name=<TopicName> page_size=20
```

## 2) 列出 metric names（替代 list-metric-names）

通过 Prom 的 label values 获取 `__name__`：

```text
account=acctA region=cn-beijing action=metric_topic.prom.label_values topic_id=<TopicId> label_name=__name__ start_ms=<StartMs> end_ms=<EndMs>
```

## 3) 推断某指标有哪些 label（series）

```text
account=acctA region=cn-beijing action=metric_topic.prom.series topic_id=<TopicId> match=<MetricName> start_ms=<StartMs> end_ms=<EndMs>
```

拿到 series 列表后：
- label 名来自每个 series 的 key 集合（排除 `__name__`）
- label 值来自对应 key 的去重集合

## 4) Label 值验证（避免过滤错值导致无数据）

```text
account=acctA region=cn-beijing action=metric_topic.prom.query topic_id=<TopicId> query="count by (ip)(<MetricName>)" time_ms=<NowMs>
```

## 5) PromQL query_range（带 pre-flight）

先做 pre-flight 估算：`series_count × points_per_series`，过大则加大 step 或缩短窗口。

```text
account=acctA region=cn-beijing action=metric_topic.prom.query_range topic_id=<TopicId> query="rate(up[5m])" start_ms=<StartMs> end_ms=<EndMs> step=60
```

## 6) 创建指标主题（plan/apply）

Plan：
```text
account=acctA region=cn-beijing action=metric_topic.create project_id=<ProjectId> topic_name=my-metric-topic ttl=30
```

Apply：
```text
account=acctA region=cn-beijing action=metric_topic.create project_id=<ProjectId> topic_name=my-metric-topic ttl=30 dry_run=false confirm_token="<token>"
```

