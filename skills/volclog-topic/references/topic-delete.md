# Topic Delete

## 适用场景

- 处理 `DeleteTopic`
- 删除主题
- 已经确认目标主题

## 必填输入

- `TopicId`

## 可选参数触发词

- 说“按 ID 删除”时，只补 `--topic-id`
- 说“我只有名字”时，先回到 `topic list`
- 说“先确认再删”时，先 `list/get`，再 `delete`

## 字段联动/限制

- 删除只以 `TopicId` 为准，不要把 `TopicName` 当删除参数
- 删除前先用 list/get 确认目标，避免误删
- 如果当前还没有稳定 ID，这一步不应该直接执行

## 常见误用

- 直接按主题名删除
- 没确认对象就删
- shortcut 已足够时还先退到 `api call`

## 下一步命令

```bash
volclog topic list --project-id <ProjectId>
volclog topic get --topic-id <TopicId>
volclog topic delete --topic-id <TopicId>
```
