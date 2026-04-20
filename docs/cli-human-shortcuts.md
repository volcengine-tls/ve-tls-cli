# volclog Full 版：人类 Shortcut 使用指南

> 这篇文档只面向 **full 版 `volclog`** 的人工交互场景。Agent/CI 默认请走 `tool / workflow / raw`，不要把 shortcut 当主路径。

## 适用范围

这份文档聚焦：

- `project / topic / metric-topic / index / log / host-group / collector / assistant`
- `--describe`
- `--print-request-template`
- 以人类手工操作为主的高频 shortcut 流程

不覆盖：

- `volclog-agent`
- Agent 默认执行路径
- `tool / workflow / raw` 的详细契约说明

---

## 什么时候用 shortcut

如果你已经明确知道自己要做的是：

- 列项目、列主题、查机器组
- 创建 topic / index / collector
- 做一次日志检索、导出、上下文查看

那么 full 版 `volclog` 的 shortcut 往往是最短路径。

如果你遇到下面任一情况，建议转回 `tool / workflow`：

- 不确定应该调用哪个 action
- 不确定请求体结构
- 要给 Agent/脚本消费
- 想先看机器契约再执行

---

## 最常用的 shortcut

### 资源查看

```bash
volclog project list --output table
volclog topic list --project-id <ProjectId> --output table
volclog host-group list --all
volclog collector list --all
```

### 创建或修改前先看说明

```bash
volclog project create --describe
volclog topic create --describe
volclog index create --describe
volclog collector create --describe
volclog log search --describe
```

### 需要复杂请求体时打印模板

```bash
volclog topic create --print-request-template=full
volclog index create --print-request-template=full
volclog collector create --print-request-template=full
volclog log put --print-request-template=full
```

---

## 常见场景

### 新建 topic 并配置索引

```bash
volclog topic create --describe
volclog topic create --print-request-template=full > topic_req.json
volclog topic create --request file://topic_req.json

volclog index create --describe
volclog index create --print-request-template=full > index_req.json
volclog index create --topic-id <TopicId> --request file://index_req.json
```

### 快速检索和导出日志

```bash
volclog log search \
  --topic-id <TopicId> \
  --query "error" \
  --from <StartTimeMs> \
  --to <EndTimeMs> \
  --limit 100

volclog --output jsonl --output-mode file --output-dir ./out \
  log export \
  --topic-id <TopicId> \
  --query "*" \
  --from <StartTimeMs> \
  --to <EndTimeMs>
```

### 导出分析结果

```bash
volclog --output jsonl --output-mode file --output-dir ./out \
  log export-analysis \
  --topic-id <TopicId> \
  --query "* | select status, count(*) as cnt group by status" \
  --from <StartTimeMs> \
  --to <EndTimeMs>
```

---

## 与 Agent 路径的边界

- 人类默认可以先 shortcut
- Agent 默认不要先 shortcut
- 需要公开 API 契约时，转到 `tool describe`
- 需要 CLI 高层编排时，转到 `workflow describe`
- 已明确 `method/path` 时才转到 `raw`

一句话：

**shortcut 是 full 版里的人类高频入口，不是 agent 主路径。**
