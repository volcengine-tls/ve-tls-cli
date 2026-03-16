# Examples

以下示例以 `tlsctl-runner` 为执行器，建议平台侧封装为工具调用。

## 1) 日志检索（只读）

```text
account=acctA region=cn-beijing action=log.search topic_id=xxx query="error" from_ms=1710374400000 to_ms=1710378000000
```

## 1.1) 项目列表（只读）

```text
account=acctA region=cn-beijing action=project.list page_size=20
```

## 1.2) 主题列表（只读）

```text
account=acctA region=cn-beijing action=topic.list project_id=<pid> page_size=20
```

## 2) 指标 Prom query_range（只读）

```text
account=acctA region=cn-beijing action=metric_topic.prom.query_range topic_id=mtid query="rate(up[5m])" start_ms=1710374400000 end_ms=1710378000000 step=15
```

## 3) 日志导出（危险操作：plan/apply）

Plan：
```text
account=acctA region=cn-beijing action=log.export topic_id=xxx query="*" from_ms=1710374400000 to_ms=1710378000000
```

Apply（需要把 plan 输出里的 confirm_token 透传回来）：
```text
account=acctA region=cn-beijing action=log.export topic_id=xxx query="*" from_ms=1710374400000 to_ms=1710378000000 dry_run=false confirm_token="<token>"
```

## 4) 创建项目（危险操作：plan/apply）

Plan：
```text
account=acctA region=cn-beijing action=project.create project_name=myproj description="demo"
```

Apply：
```text
account=acctA region=cn-beijing action=project.create project_name=myproj description="demo" dry_run=false confirm_token="<token>"
```

## 5) 创建索引（危险操作：plan/apply）

Plan：
```text
account=acctA region=cn-beijing action=index.create topic_id=<tid> body=file://./index.json
```

Apply：
```text
account=acctA region=cn-beijing action=index.create topic_id=<tid> body=file://./index.json dry_run=false confirm_token="<token>"
```

## 6) 创建资源链路（project → topic → index）

1) project.create（拿到 ProjectId）
```text
account=acctA region=cn-beijing action=project.create project_name=myproj
```

2) topic.create（使用 ProjectId，拿到 TopicId）
```text
account=acctA region=cn-beijing action=topic.create project_id=<ProjectId> topic_name=mytopic ttl=30 shard_count=2 auto_split=true
```

3) index.create（使用 TopicId）
```text
account=acctA region=cn-beijing action=index.create topic_id=<TopicId> body=file://./index.json
```

说明：上述三个步骤均为危险操作，默认返回 plan；需要带 confirm_token 二次 apply。

## 7) 编排时提取 ID（示例片段）

当 `dry_run=false` 执行成功后，runner 会返回：

```json
{
  "data": {
    "ProjectId": "pid-xxx"
  },
  "audit": {
    "account": "acctA",
    "region": "cn-beijing",
    "action": "project.create",
    "profile": "acctA-cn"
  }
}
```

下一步把 `ProjectId` 注入为 `project_id`：

```text
account=acctA region=cn-beijing action=topic.create project_id=pid-xxx topic_name=mytopic
```
