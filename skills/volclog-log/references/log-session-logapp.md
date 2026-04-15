# Log Session Answers, Attachments And LogApp

## 适用场景
本页用于当前动作族的 API-only 场景；先把用户意图收敛到本页覆盖的动作，再决定是否继续看 shortcut 或 api-explorer。

## 必填输入
先确认本页“覆盖动作”里对应动作的主键或核心输入，例如 TopicId、RuleId、TaskId、Query、Cursor、Request body。

## 可选参数触发词
见本页后续的关键词触发、推荐命令或常见误用；如果用户只是泛泛描述，先按主键优先，不要提前补一堆筛选项。

## 字段联动/限制
以本页的参数约束、字段联动、任务状态或消费语义为准；字段多时先看 `--describe`，不要靠记忆拼。

## 常见误用
不要把本页动作混成普通检索、普通写入、普通删除或普通读详情；不要在主键没确认前直接执行高风险操作。

## 下一步命令
先执行本页给出的推荐命令；如果仍不够，再转对应的 `volclog-api-explorer` 或更细的 shortcut reference。


这个 reference 只处理会话答案、附件和 LogApp 相关 API-only 动作。

## 覆盖动作

- `DescribeSessionAnswer`
- `UploadAssistantAttachment`
- `CreateApp`
- `DescribeApps`
- `DescribeApp`
- `ModifyApp`
- `DeleteApp`
- `CreateAppInstance`
- `DescribeAppInstances`
- `ModifyAppInstance`
- `DeleteAppInstance`
- `CreateAppSceneMeta`
- `DescribeAppSceneMeta`
- `DescribeAppSceneMetas`
- `ModifyAppSceneMeta`
- `DeleteAppSceneMeta`
- `CreateLogApp`
- `DescribeLogApps`
- `DescribeLogApp`
- `ModifyLogApp`
- `DeleteLogApp`
- `DescribeLogAppDashboard`
- `DescribeLogAppDashboardTemplates`
- `DescribeLogAppMarket`
- `DescribeLogAppSession`
- `DescribeLogAppSessions`
- `DescribeLogAppTrace`
- `SearchLogAppTraces`
- `SearchLogAppSpans`
- `DescribeResources`

## 先用什么时候

- 用户说“会话答案 / 总结结果”
- 用户说“上传附件给会话 / 助手附件”
- 用户说“App / App 实例 / 场景元数据”
- 用户说“LogApp / 应用市场 / 会话 / Trace / Dashboard”

## 关键约束

- 会话答案和附件都挂在 `log` 组，不存在单独的 `assistant` 公共 group
- 上传附件前先确认附件用途，再决定是否需要先看会话答案
- `App`、`AppInstance`、`AppSceneMeta`、`LogApp` 是不同层级对象，不要混用 ID
- LogApp 这类动作通常需要先看资源本体，再看会话/Trace/Market 关联

## 常见误用

- 不要把会话答案需求切到 `assistant`
- 不要把 `App`、`AppInstance`、`AppSceneMeta`、`LogApp`、会话、Trace、Dashboard 当成同一件事
- 不要把附件上传当成普通日志写入

## 何时切到 api-explorer

- 用户要更底层的 LogApp 或会话字段
- 用户明确点名官方文档之外的原始参数
