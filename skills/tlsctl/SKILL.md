---
name: tlsctl
description: Use Volcengine TLS via tlsctl with multi-account and multi-region support. Converts Chinese semi-structured requests (account+region+action+args) into safe plan/apply executions through tlsctl-runner, covering log search/export, project/topic/metric-topic/index management, and Prom query APIs.
---

通过 `tlsctl`（底层调用 TLS OpenAPI）为用户执行日志检索/导出、项目/主题/指标主题/索引管理，以及指标主题 Prom 查询等操作。该 skill 面向“工具调用型智能体”：把用户输入（推荐“中文 + 参数键值”半结构化）转成对 `tlsctl-runner` 的调用，并消费其 JSON 输出。

本 skill 不要求用户在对话中提供 AK/SK 明文；要求用户在本地提前配置 profile 或环境变量。

## 推荐工作流（确定性）

1) 从用户输入中提取/确认以下字段（缺一不可）：
- `account`：账号标识（逻辑名）
- `region`：如 `cn-beijing` / `ap-singapore-1`
- `action`：如 `log.search` / `log.export` / `project.list` / `metric_topic.prom.query_range`

2) 用 `tlsctl-runner` 执行（优先 `--text` 半结构化）：
- 只读操作可直接执行
- 危险操作（create/modify/delete/export）默认先 `dry_run`，拿到 `plan.confirm_token` 后再二次执行

3) 输出给用户时：
- 仅展示必要字段（比如数量、ID、关键错误信息）
- 需要审计时附带 `audit.commands`（避免泄露密钥）

### 常见编排

- 创建资源链路：project.create → topic.create → index.create（逐步从响应提取 ProjectId/TopicId）
- 按名称定位再检索：project.list（按 project_name/fuzzy_search_key）→ topic.list（按 topic_name/fuzzy_search_key）→ log.search/log.export
- 已知 TopicId：直接 log.search/log.export（最短路径）

## 用户侧前置配置（多账号多 region）

推荐为同一 account 的不同 region 建多个 profile：`<account>-<region>`（AK/SK 可相同）。

```bash
tlsctl configure set --profile acctA-cn --ak <akA> --sk <skA> --region cn-beijing
tlsctl configure set --profile acctA-sg --ak <akA> --sk <skA> --region ap-singapore-1
```

## 关键工具：tlsctl-runner

`tlsctl-runner` 是本 skill 的执行器：
- 输入：stdin JSON 或 `--text` 半结构化文本
- 输出：stdout JSON（包含 plan/data/error/audit）
- 规则：account+region 自动匹配本地 profile；危险操作需要 confirm_token

实现与规格：
- `cmd/tlsctl-runner`
- `skills/tlsctl-runner/spec.md`

## 调用示例（推荐：--text）

### 日志检索（只读）

```bash
tlsctl-runner --text 'account=acctA region=cn-beijing action=log.search topic_id=xxx query="error" from_ms=1710374400000 to_ms=1710378000000'
```

### 日志导出（危险操作：先 dry_run 再确认）

```bash
tlsctl-runner --text 'account=acctA region=cn-beijing action=log.export topic_id=xxx query="*" from_ms=1710374400000 to_ms=1710378000000'
```

若返回 `plan.confirm_required=true` 且包含 `plan.confirm_token`，则二次执行：

```bash
tlsctl-runner --text 'account=acctA region=cn-beijing action=log.export topic_id=xxx query="*" from_ms=1710374400000 to_ms=1710378000000 dry_run=false confirm_token="<token>"'
```

## 资源

如需将该 skill 接入到不同平台的 “tool schema”，参考：
- [tool schema](references/tool_schema.json)
- [examples](references/examples.md)
- [actions](references/actions.md)
- [workflows](references/workflows.md)
