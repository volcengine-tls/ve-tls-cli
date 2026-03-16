# Workflows

本文件提供面向用户的“按需编排流程”。当用户提出目标时，优先按下列流程逐步补全必要信息，再调用 `tlsctl-runner` 执行。

通用原则：
- 优先让用户用 `account + region` 描述上下文；runner 自动选择 profile
- 若出现 `profile_not_found` 或 `profile_ambiguous`，先引导用户完成 profile 命名/映射再继续
- delete/modify/create/export 等危险操作：默认先 dry_run 产出 plan，再二次确认 apply
- 多步骤编排中需要从上一步响应提取 ID（如 ProjectId/TopicId）。字段提取规则见本文件末尾“响应字段提取”。

## 0) 账号与区域准备（首次使用）

当用户还没有 profile：
1) 询问：账号标识（account）与 region（例如 `cn-beijing`）
2) 引导用户创建 profile（推荐命名 `<account>-<regionShort>`）：

```bash
tlsctl configure set --profile <account>-cn --ak <ak> --sk <sk> --region cn-beijing
```

若用户同一 AK/SK 需要多 region：
```bash
tlsctl configure set --profile <account>-cn --ak <ak> --sk <sk> --region cn-beijing
tlsctl configure set --profile <account>-sg --ak <ak> --sk <sk> --region ap-singapore-1
```

## 1) 日志检索（log.search）

用户常见表达：
- “在 account=A region=cn-beijing 的 topic 里查最近 1 小时 error”
- “给我导出某个时间段的数据”

流程：
1) 必填信息：`account`、`region`、`topic_id`、`query`、时间范围（`from_ms/to_ms` 或可推导）
2) 若用户只给“最近 1 小时/1 天”，需要转换为毫秒时间戳（上层智能体/调用方完成）
3) 执行（只读，可直接执行或先 dry_run）

```text
account=acctA region=cn-beijing action=log.search topic_id=xxx query="error" from_ms=... to_ms=...
```

4) 输出建议：
- 返回 `Logs` 数量、是否 `ListOver`、必要字段（如 `LogGroupList` 的关键字段）
- 需要下游处理时建议 `log.export`（jsonl）

## 2) 日志导出（log.export，plan/apply）

目的：按页自动翻页导出，适合下游流式处理。

流程：
1) 收集：`account/region/topic_id/query/from_ms/to_ms/max_pages(可选)`
2) 默认 dry_run（plan）：展示命令与风险，要求用户确认
3) apply：带 `dry_run=false confirm_token=<token>`

Plan：
```text
account=acctA region=cn-beijing action=log.export topic_id=xxx query="*" from_ms=... to_ms=... max_pages=10
```

Apply：
```text
account=acctA region=cn-beijing action=log.export topic_id=xxx query="*" from_ms=... to_ms=... max_pages=10 dry_run=false confirm_token="<token>"
```

输出建议：
- 默认 `jsonl`，runner 会解析为数组返回（或平台侧直接流式处理 stdout）

## 3) 项目与主题管理（project/topic）

### 3.1 列出项目（project.list）
```text
account=acctA region=cn-beijing action=project.list page_size=20
```

### 3.2 创建项目（project.create，confirm）
1) 收集：`project_name`（必填），可选 `description/tags`
2) dry_run → apply

```text
account=acctA region=cn-beijing action=project.create project_name=myproj description="demo"
```

### 3.3 列出主题（topic.list）
互斥：`topic_name` 与 `topic_id` 不可同时给。

```text
account=acctA region=cn-beijing action=topic.list project_id=<pid> page_size=20
```

### 3.4 创建主题（topic.create，confirm）
必填：`project_id`、`topic_name`

```text
account=acctA region=cn-beijing action=topic.create project_id=<pid> topic_name=demo-topic ttl=30 shard_count=2 auto_split=true
```

## 6) 创建资源端到端（project → topic → index，plan/apply）

适用用户表达：
- “我想创建一个 project，然后在里面创建 topic，最后给 topic 配索引”

流程（推荐做成固定编排）：

### 6.1 创建 Project（project.create）

1) 收集：`account/region/project_name`（可选 `description/tags`）
2) 执行 dry_run（plan），展示命令并生成 confirm_token
3) 用户确认后 apply

Plan：
```text
account=acctA region=cn-beijing action=project.create project_name=myproj description="demo"
```

Apply：
```text
account=acctA region=cn-beijing action=project.create project_name=myproj description="demo" dry_run=false confirm_token="<token>"
```

4) 从结果中提取 `ProjectId`，用于后续步骤

### 6.2 创建 Topic（topic.create）

1) 必填：`project_id=<ProjectId>`、`topic_name`
2) 同样 dry_run → apply

Plan：
```text
account=acctA region=cn-beijing action=topic.create project_id=<ProjectId> topic_name=mytopic ttl=30 shard_count=2 auto_split=true
```

Apply：
```text
account=acctA region=cn-beijing action=topic.create project_id=<ProjectId> topic_name=mytopic ttl=30 shard_count=2 auto_split=true dry_run=false confirm_token="<token>"
```

3) 从结果中提取 `TopicId`，用于后续步骤

### 6.3 创建/修改 Index（index.create / index.modify）

索引请求体较复杂，建议用户先准备 `index.json`，然后用 `file://` 引用：

Plan：
```text
account=acctA region=cn-beijing action=index.create topic_id=<TopicId> body=file://./index.json
```

Apply：
```text
account=acctA region=cn-beijing action=index.create topic_id=<TopicId> body=file://./index.json dry_run=false confirm_token="<token>"
```

输出建议：
- 最终返回：`ProjectId`、`TopicId`、index 配置摘要
- 若任何一步失败：停止后续步骤，并提示用户如何重试（可复用已创建的 ProjectId/TopicId）

## 7) 按名称定位并 search/export（projectName/topicName → topicId → search/export）

适用用户表达：
- “给我在 projectName=xxx、topicName=yyy 的日志里搜 …”
- “我只有 projectName/topicName，不知道 ID”
- “我只知道 projectId/topicId 其中一个”

### 7.1 已知 TopicId（最短路径）

直接执行 `log.search` / `log.export`：

```text
account=acctA region=cn-beijing action=log.search topic_id=<TopicId> query="error" from_ms=... to_ms=...
```

### 7.2 已知 ProjectId，需选择 Topic

1) 先列出 topic（可用 `topic_name` 精确筛选；也可用 `fuzzy_search_key` 模糊匹配）
2) 从返回中选出目标 `TopicId`
3) 再执行 search/export

列出 topic（精确 topic_name）：
```text
account=acctA region=cn-beijing action=topic.list project_id=<ProjectId> topic_name=<TopicName> page_size=20
```

列出 topic（模糊匹配）：
```text
account=acctA region=cn-beijing action=topic.list project_id=<ProjectId> fuzzy_search_key=<keyword> page_size=20
```

### 7.3 仅已知 ProjectName/TopicName（先找 ProjectId，再找 TopicId）

1) 列出 project：
   - 精确：`project_name`
   - 模糊：`fuzzy_search_key`
2) 从返回选出 `ProjectId`
3) 列出 topic 选 `TopicId`
4) 执行 search/export

列出 project（精确 project_name）：
```text
account=acctA region=cn-beijing action=project.list project_name=<ProjectName> page_size=20
```

列出 project（模糊匹配）：
```text
account=acctA region=cn-beijing action=project.list fuzzy_search_key=<keyword> page_size=20
```

最后执行 export（plan/apply）：
```text
account=acctA region=cn-beijing action=log.export topic_id=<TopicId> query="*" from_ms=... to_ms=...
```

## 8) 响应字段提取（ProjectId / TopicId）

上层智能体在编排 `project → topic → index` 或 `name → id → search/export` 时，需要从 runner 输出的 JSON 里提取 ID 并注入到下一步请求中。

runner 的输出结构：
- plan 模式：`{"plan":{...},"audit":{...}}`
- 执行成功：`{"data":<tlsctl_json>,"audit":{...}}`

提取规则（按优先级）：

### 8.1 ProjectId 提取

适用：`project.create`、`project.list`（用户从列表中选）

1) `data.ProjectId`（最常见，创建项目返回）
2) `data.Projects[0].ProjectId` 或 `data.Projects[0].ProjectID`（列表取第一条时）

### 8.2 TopicId 提取

适用：`topic.create`、`topic.list`、`metric_topic.create`、`metric_topic.list`

1) `data.TopicId` 或 `data.TopicID`（创建主题返回）
2) `data.Topics[0].TopicId` 或 `data.Topics[0].TopicID`（DescribeTopics 列表取第一条）

### 8.3 Index 相关

index.create/index.modify 通常不需要额外提取 ID，但需要把上一阶段得到的 `TopicId` 传入：

```text
action=index.create topic_id=<TopicId> body=file://./index.json
```

### 8.4 工程化建议（减少字段差异风险）

当遇到不同返回结构时，建议按“多候选 key”策略提取：

- ProjectId：`ProjectId`、`ProjectID`
- TopicId：`TopicId`、`TopicID`

并对列表返回统一使用：

- `Projects`、`Topics`（取第一个元素或按名称过滤后的元素）

## 4) 指标主题与 Prom 查询（metric_topic.*）

### 4.1 列出指标主题（metric_topic.list）
```text
account=acctA region=cn-beijing action=metric_topic.list project_id=<pid>
```

### 4.2 Prom query_range（metric_topic.prom.query_range）
必填：`topic_id/query/start_ms/end_ms/step`

```text
account=acctA region=cn-beijing action=metric_topic.prom.query_range topic_id=<mtid> query="rate(up[5m])" start_ms=... end_ms=... step=15
```

## 5) 索引管理（index.*）

索引请求体较复杂，建议用 `file://`：
1) 用户准备 `index.json`
2) 使用 `body=file://./index.json`

创建：
```text
account=acctA region=cn-beijing action=index.create topic_id=<tid> body=file://./index.json
```
