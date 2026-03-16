# Workflows（metric-topic）

本文件将“指标主题运维与查询”拆解为可执行的强制流程。任何 PromQL 查询都必须遵循 Phase 1→2→3 的校验顺序。

通用输入格式（推荐 `--text`）：
```text
account=<acct> region=<region> action=<action> <key=value...>
```

## Phase 0：环境与前置条件检查（首次使用必做）

1) 确认用户已配置 profile（account+region 可匹配到 profile）：
- 若执行返回 `profile_not_found/profile_ambiguous`，先引导用户完成 profile 配置或映射文件

2) 确认 topic_id 的来源：
- 用户直接提供 topic_id：进入 Phase 1
- 仅有名称：用 `metric_topic.list` 定位 topic_id（见 Phase 1）

## Phase 1：定位 TopicId 与时间范围

### 1.1 通过名称定位 TopicId

按 projectName/topicName 精确筛选：
```text
account=acctA region=cn-beijing action=metric_topic.list project_name=<ProjectName> topic_name=<TopicName> page_size=20
```

输出要求：
- 若返回多条候选，必须列出候选并让用户确认选择（禁止自动猜测）
- 选定后提取 `TopicId`（字段提取规则：`data.Topics[*].TopicId/TopicID`）

### 1.2 时间范围策略（用户未给时）

- 当前状态/快速排障：最近 1 小时
- 趋势分析：最近 6 小时

所有时间必须转成毫秒：
- `start_ms/end_ms` 或 `time_ms`

## Phase 2：指标与 label 校验（禁止跳步）

### Step 1：发现指标（metric names）

通过 Prom label values 获取 `__name__`：
```text
account=acctA region=cn-beijing action=metric_topic.prom.label_values topic_id=<TopicId> label_name=__name__ start_ms=<StartMs> end_ms=<EndMs>
```

处理规则：
- 用户指定的指标名必须在返回列表中，否则直接告知“该指标在当前 Topic 内不存在”
- 禁止凭空构造指标名

### Step 2：推断指标 labels（series）

用 `series` 获取 label set（建议缩小时间范围与加 match）：
```text
account=acctA region=cn-beijing action=metric_topic.prom.series topic_id=<TopicId> match=<MetricName> start_ms=<StartMs> end_ms=<EndMs>
```

处理规则：
- label 名来自 series 对象的 key 集合（排除 `__name__`）
- label 值来自 key 对应值的去重集合

### Step 2（label 值验证）：当 PromQL 含 label 过滤时必做

若 PromQL 中包含 `{label="value"}`，必须先验证该 value 真实存在：

```text
account=acctA region=cn-beijing action=metric_topic.prom.query topic_id=<TopicId> query="count by (<label>)(<MetricName>)" time_ms=<NowMs>
```

处理规则：
- 若用户提供的 label value 不在枚举结果中：提示正确值并停止

## Phase 3：Pre-flight Check（查询前预判）

对 `query_range` 必须预估结果规模，避免静默截断/超时：

```
预期点数 ≈ series 数 × ceil(时间范围秒数 ÷ step秒数)
```

执行建议：
1) 先用 `series` 估计 series 数（或用更严格 match/过滤后再估计）
2) 若预期点数 > 5000：
   - 增大 step（例如 15 → 60 → 300）
   - 缩短时间窗口（例如 1h → 5m）
   - 增加 label 过滤（缩小 series 数）

## Phase 4：执行 PromQL 并输出（必须带来源）

### 4.1 query（瞬时）
```text
account=acctA region=cn-beijing action=metric_topic.prom.query topic_id=<TopicId> query="<PromQL>" time_ms=<NowMs>
```

### 4.2 query_range（区间）
```text
account=acctA region=cn-beijing action=metric_topic.prom.query_range topic_id=<TopicId> query="<PromQL>" start_ms=<StartMs> end_ms=<EndMs> step=60
```

输出铁律：
- 必须包含：TopicId、时间范围、查询状态、行数/点数
- 若无数据：说明“无数据/过滤值不匹配/时间范围不含上报”三类可能原因，并给出下一步排查动作（回到 Phase 2）

