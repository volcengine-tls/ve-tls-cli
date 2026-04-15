# Log Export Analysis

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


这个 reference 只处理 `volclog log export-analysis`。

## 先用什么时候

- SQL / 聚合 / 统计 / 行结果导出
- 查询语句里已经出现分析语义

## 关键约束

- `--max-pages` 不支持
- 分页应该用 SQL 的 `limit/offset`
- `--output-mode file` 适合大结果
- 分析可见列受索引和 `SqlFlag` 影响，旧日志可能对应列为空
- 如果“查询成功但列为 null”，优先怀疑索引字段没开 `SqlFlag` 或旧数据尚未重建

## 关键词触发可选参数

- 说“SQL”“统计”“聚合”“分组”“分析结果”时，补分析导出语义
- 说“结果很多”“落文件”“别刷屏”时，补 `--output-mode file`
- 说“继续翻页”“再多看几条”时，用 SQL 的 `limit/offset`，不要补 `--max-pages`
- 说“某列总是空”时，先检查索引里的 `SqlFlag`

## 推荐流程

```bash
volclog log export-analysis --describe
volclog --output-mode file log export-analysis --describe
```

## 常见误用

- 不要把普通搜索当分析导出
- 不要把 `--max-pages` 套进分析查询
- 不要把结果理解成原始日志行

## 何时切到 api

- 需要更原始的 SearchLogs 请求字段
- 需要超出 shortcut 的分析调试参数
