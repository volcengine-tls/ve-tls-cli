# Log Search

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


这个 reference 只处理 `volclog log search`。

## 先用什么时候

- 查一段时间内的日志
- 先确认有没有命中
- 还不确定要不要导出

## 关键约束

- `--topic-id`、`--query`、`--from`、`--to` 是核心输入
- `--limit`、`--sort`、`--offset`、`--context` 只适合普通搜索语义
- `--accurate-query`、`--must-complete` 是检索执行策略，不是筛选条件；只有用户明确提到精确匹配或必须完整返回时再补
- 只要查询里出现 `select`、`with`、`insert`、聚合语义，就不要再把它当普通 search

## 关键词触发可选参数

- 说“多给点”“少一点”“最多 N 条”“只看前 N 条”时，补 `--limit`
- 说“最新的”“最早的”“倒序”“升序”时，补 `--sort`
- 说“上下文”“前后文”“周边日志”时，补 `--context`
- 说“只看某个范围”“从哪到哪”时，补 `--from` / `--to`
- 说“结果很多想落盘”“别刷屏”时，补 `--output-mode file`
- 说“必须完整返回”“不要部分结果”时，补 `--must-complete`
- 说“更精确匹配”“减少误匹配”时，补 `--accurate-query`

## 推荐判断

- 普通检索 -> `log search`
- 结果很多 -> `log export`
- SQL / 聚合 / 行结果 -> `log export-analysis`

## 常见用法

```bash
volclog log search --describe
```

```bash
volclog log search --topic-id <TopicId> --query "<Query>" --from <StartMs> --to <EndMs>
```

## 常见误用

- 不要把分析查询丢给 `log search`
- 不要因为结果多就先切 export-analysis
- 不要把 `--context` 当成通用分页参数
- 不要把 `--accurate-query`、`--must-complete` 当成所有查询都该开的默认参数

## 何时切到 api

- 需要 SearchLogs 的原始字段而 shortcut 不够
- 需要官网文档外的调试参数
