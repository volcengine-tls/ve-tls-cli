# Index Get

## 适用场景
本页用于当前 index API-only 动作族；先确认是 config、rebuild、logId 还是普通索引读写，再继续。

## 必填输入
先确认本页“覆盖动作”里的主键，通常是 `TopicId`、任务 ID，或当前查询条件。

## 可选参数触发词
见本页后续的推荐命令和常见误用；如果用户只是在看普通索引读写，优先回到 `index get/create/modify`。

## 字段联动/限制
以本页的动作边界为准；config、rebuild、logId 分布是不同链路，不要混用。

## 常见误用
不要把 API-only 动作混成 shortcut；不要在主键没确认前直接删配置、删任务或改任务。

## 下一步命令
先用本页的推荐命令或对应详情页；如果还不够，再去 `volclog-api-explorer`。


这个 reference 只处理索引读取。

## 适用场景

- 用户说“看当前索引 / 看字段解析 / 看 tokenizer”
- 用户想先确认现状，再考虑写入

## 推荐命令

```bash
volclog index get --topic-id <TopicId>
volclog --output-mode file --output-file ./index.json index get --topic-id <TopicId>
```

## 参数约束

- `TopicId` 是外层 flag，不在 body 里重复维护
- 看详情时优先落文件，避免索引 JSON 占满上下文

## 关键词触发可选参数

- 说“看当前索引”“看字段解析”“看 tokenizer”时，直接用 `--topic-id`
- 说“详情太长”“别占屏幕”“存文件里”时，补 `--output-mode file` / `--output-file`
- 说“只想确认有没有索引”时，不要先看 `create / modify`，直接用 `volclog index get --describe`

## 常见误用

- 不要先写 create/modify，再回头看现状
- 不要把 index body 和 topic body 混起来
- 不要把 `TopicId` 写进模板里

## 何时切换

- 如果 shortcut 已能看懂现状，就不要先进 api-explorer
- 需要更底层 action 时，再看 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
