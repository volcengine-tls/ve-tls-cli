# Metric Topic Routing

## 适用场景

- 用户先说“metric topic”，但还没说清是在做资源管理还是指标查询
- 需要先分流到 `metric-topic-resource.md` 或 `metric-topic-query.md`

## 必填输入

- 先判断用户要的是“资源 CRUD”还是“PromQL / Prometheus 查询”

## 可选参数触发词

- 说“列/查/建/改/删指标主题”时，进入 `metric-topic-resource.md`
- 说“PromQL / Prometheus / time series”时，进入 `metric-topic-query.md`
- 说“我只想知道该看哪篇”时，先停在 routing，不要直接猜 action

## 字段联动/限制

- 资源管理和查询是两条路径，不要混在一条命令里
- 用户意图还不清楚时，先分流，再进入具体 reference
- shortcut 不覆盖的公开 Prom API 也先留在 `metric-topic` 组内继续找

## 常见误用

- 把 PromQL 需求直接改写成资源 CRUD
- 把资源创建/删除误路由到查询接口
- 默认跳到 `log` 组

## 下一步命令

- 资源管理：看 [metric-topic-resource.md](metric-topic-resource.md)
- 指标查询：看 [metric-topic-query.md](metric-topic-query.md)
- 仍不够时：`volclog api metric-topic <Action> --describe`
