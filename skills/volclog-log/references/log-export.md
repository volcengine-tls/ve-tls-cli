# Log Export

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


这个 reference 只处理 `volclog log export`。

## 先用什么时候

- 导出很多原始日志
- 用户想落文件
- 目标还是原始日志，不是 SQL / 聚合结果

## 关键约束

- 不要把分析查询放到这里
- 查询一旦带 `select/with/insert` 或聚合语义，就改用 `log export-analysis`
- `--max-pages` 只是在需要时控制循环深度
- 大结果优先 `--output-mode file`
- `export` 和 `search` 用的是同一套原始检索语义，只是导出默认分页批次更大
- 要长时间落盘时，优先 `jsonl`，不要先打 stdout 再重定向

## 关键词触发可选参数

- 说“很多原始日志”“导出到文件”“别占屏幕”时，补 `--output-mode file`
- 说“别漏”“多翻几页”“尽量全量”时，补 `--max-pages`
- 说“只想要原始日志，不要 SQL/聚合”时，继续留在 export
- 说“想要原始导出链接”时，不要补分析查询参数

## 推荐流程

```bash
volclog log export --describe
volclog --output-mode file log export --describe
```

## 常见误用

- 不要把分析查询交给 export
- 不要把结果打到 stdout 再手动重定向
- 不要把原始日志导出和分析结果导出混用

## 何时切到 api

- 需要 SearchLogs 的原始字段而 shortcut 不够
- 需要官网文档外的查询控制项
