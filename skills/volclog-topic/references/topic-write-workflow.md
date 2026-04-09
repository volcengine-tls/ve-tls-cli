# Topic Write Workflow

这个 reference 只处理 topic 写入，不处理 list/get。

## Create / Modify Flow

1. 先看约束:
  `volclog topic create --describe`
2. 打印模板:
   `volclog topic create --print-request-template=full`
3. 编辑 `req.json`
4. 执行:
   `volclog topic create --request file://req.json`

如果是修改：

1. 先确认 `TopicId`
2. `volclog topic modify --describe`
3. 字段多时继续走模板，而不是全改成 flags

## Field Reminders

- `AutoSplit=true` 时注意 `MaxSplitShard`
- 写入字段变多时，不要把所有字段都改成 flags；优先回到模板
- 先用 `list` / `get --describe` 确认资源形态，再写入

## 不要误走

- 不要为了 topic 写入先跑 `capabilities --view groups`
- 不要直接退化成 `api call`
