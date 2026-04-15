# Log Ingest

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


这个 reference 只处理 `volclog log ingest`。

## 先用什么时候

- 批量导入文本或 JSON 日志
- 用户不想自己组 `PutLogs` body
- 需要 CLI 自动补时间、分批、统计头和压缩

## 关键约束

- `--topic-id` 和 `--input` 必填
- `--input-format` 默认 `lines`
- `lines` 输入写入 `__content__`
- `jsonl/json-array` 保留原始字段，不做 `message` 重映射
- `--time-field` 只支持 `jsonl/json-array`
- `--time-format` 只接受 CLI 已支持的格式；当前优先用 `unix_ms`、`unix`、`rfc3339`、`auto`
- 不传 `--time-field` 时，CLI 用本次命令启动时的毫秒时间戳补齐时间
- `--batch-max-count` 必须大于 0，默认 500
- 默认压缩为 `lz4`
- `--source`、`--file-name`、`--tag` 会进入最终发送的 `LogGroup`
- 每批请求会自动带 `log-count`、`earliest-log-time`、`latest-log-time` 统计头

## 关键词触发可选参数

- 说“纯文本”“逐行日志”“一行一条”时，补 `--input-format lines`
- 说“每行都是 JSON”时，补 `--input-format jsonl`
- 说“整个文件就是数组”时，补 `--input-format json-array`
- 说“用某个字段当时间”“时间字段名”时，补 `--time-field` / `--time-format`
- 说“来源主机”“文件来源”时，补 `--source` / `--file-name`
- 说“标签”“元信息”时，补 `--tag`
- 说“量很大”“别一条条发”“多批次”时，补 `--batch-max-count`
- 说“压缩”“省流量”时，默认保留 `lz4`

## 推荐用法

```bash
volclog log ingest --describe
```

```bash
volclog log ingest --topic-id <TopicId> --input file://./app.log --input-format lines --source host-a --file-name app.log
```

```bash
volclog log ingest --topic-id <TopicId> --input file://./events.jsonl --input-format jsonl --time-field ts --time-format unix_ms
```

## 常见误用

- 不要把 lines 输入当成 JSON 再去写字段映射
- 不要给 lines 传 `--time-field`
- 不要把 `jsonl/json-array` 里的字段改成 message 风格重映射
- 不要把 `--time-format` 按 Go layout 或自定义格式随便试
- 不要在只有原始文本时强行走 `log put`

## 何时切到 put

- 用户已经有现成的 `PutLogs` 请求体
- 需要精确控制 `x-tls-compresstype` / `x-tls-hashkey`
- 需要按原始 API 语义直接写入
