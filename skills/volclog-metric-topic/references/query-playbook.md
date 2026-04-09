# Metric Topic Query Playbook

## Resource Management

用户说“列指标主题/创建指标主题”时，先留在 `metric-topic` 资源管理命令里：

```bash
volclog metric-topic list --project-id <ProjectId> --all
volclog metric-topic get --topic-id <TopicId>
volclog metric-topic create --describe
volclog metric-topic create --print-request-template=full
volclog metric-topic modify --describe
volclog metric-topic delete --describe
```

看单个对象详情时，优先：

```bash
volclog --output-mode file --output-file ./metric-topic-detail.json metric-topic get --topic-id <TopicId>
```

高频边界：

- `Ttl` 优先使用已知可配档位：`15`、`30`、`90`、`180`、`365`
- 创建后马上改/删，可能遇到 `409 TaskIsRunning`
- 遇到 `TaskIsRunning` 先查状态，再重试：

```bash
volclog metric-topic get --topic-id <TopicId>
volclog metric-topic list --project-id <ProjectId>
```

## Query

用户说 PromQL、Prometheus 查询、time series 检索时，先用：

```bash
volclog metric-topic search --describe
```

不要先误路由到 `log search` 或 explorer。
