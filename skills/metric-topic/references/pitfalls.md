# Pitfalls（必读）

本文件收集“仅靠 PromQL 常识/CLI help 推导不出”的真实踩坑点。遇到异常结果时优先对照本清单排查。

## 1) 时间戳必须毫秒（13 位）

- `start_ms/end_ms/from_ms/to_ms/time_ms` 必须是 13 位毫秒时间戳
- 防呆：时间戳必须 `>= 1000000000000`（2001 年以后），否则极可能是秒级，查询会落到 1970

## 2) 指标名/label 不能凭空构造

指标名必须来自工具返回：
- 优先用 `metric_topic.prom.label_values label_name="__name__"` 列出 metric names
- 用户说“网络延迟”这类语义时，先列指标名，再选择最匹配的真实指标

label 名/label 值必须来自：
- 用户明确提供
- 或 `metric_topic.prom.series` / `metric_topic.prom.labels` / `metric_topic.prom.label_values` 的返回

## 3) 查询前必须做 pre-flight 预估（防静默截断/超时）

对 `query_range` 类查询，结果规模近似：

```
预期点数 ≈ series 数 × ceil(时间范围秒数 ÷ step秒数)
```

建议阈值：
- 预期点数 > 5000：必须调整（增大 step、缩短时间范围、加 label 过滤、分批）

## 4) 二元运算性能风险（尤其 irate/rate）

在某些后端实现中，`irate(A[5m]) / irate(B[5m])` 这类二元运算可能更容易触发超时。

推荐策略：
- 二元运算优先用 `rate`，必要时拆成两次查询再在客户端 join 计算

## 5) “无数据”与“过滤错值”表现相同

常见现象：
- 查询返回空，但没有报错

优先排查顺序：
1) 指标名是否存在（从 `__name__` 列表交叉验证）
2) label 名是否存在（从 `series/labels` 交叉验证）
3) label 值是否真实存在（用 `count by (label)(metric)` 或 `label_values` 验证）
4) 时间范围内是否确实没有上报（扩大时间范围重试）

## 6) 输出必须带来源

向用户展示任何数据时，必须同时带：
- TopicId（用户输入或定位得到）
- 时间范围（start_ms/end_ms 或 from_ms/to_ms）
- 查询状态（success/incomplete/empty）

