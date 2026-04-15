# Log Put

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


这个 reference 只处理 `volclog log put`。

## 先用什么时候

- 用户已经准备好原始 `PutLogs` 请求体
- 需要按原始 API 语义直接写入
- 需要精确控制请求头和请求格式

## 关键约束

- `--topic-id` 必填
- `--request` 必填
- `--request-format` 只在 `json` / `jsonl` 间切换
- `--compress-type`、`--hash-key`、`--content-md5` 都是可选增强头
- `Logs[].Time` 必须是 Unix 毫秒，不是秒

## 推荐流程

```bash
volclog log put --describe
volclog log put --print-request-template=full
```

## 关键词触发可选参数

- 说“已经有完整 PutLogs body”“就按原始 API 发”时，补 `--request`
- 说“文件里是一行一条”“JSONL”时，补 `--request-format jsonl`
- 说“压缩发送”“少占带宽”“lz4”时，补 `--compress-type lz4`
- 说“按哈希路由”“同一 key 保持顺序”时，补 `--hash-key`
- 说“我要自己带摘要”“内容校验”时，补 `--content-md5`

## 常见误用

- 不要把 `log ingest` 能做的事反过来硬塞到 `log put`
- 不要手写秒级时间
- 不要把 JSONL 写入误当成普通检索

## 何时切到 api

- 需要更原始的 `PutLogs` 调试字段
- shortcut 已不足以表达底层请求
