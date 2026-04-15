# Topic Get

## 适用场景

- 处理 `DescribeTopic`
- 已经有 `TopicId`
- 想看单个主题详情

## 必填输入

- `TopicId`

## 可选参数触发词

- 说“详情太长”“别刷屏”“保存下来”时，补 `--output-mode file`
- 说“我只知道名字”时，不要在 get 里猜，先回到 `topic list`

## 字段联动/限制

- `get` 只看单对象详情，不承担列表筛选职责
- 如果只有主题名，先 list 再拿 `TopicId`
- 结果很大时优先落文件，不要一开始就靠复杂 `--jmes-filter` 硬裁

## 常见误用

- 用 `TopicName` 代替 `TopicId`
- 在详情很长时继续堆复杂过滤
- shortcut 够用时先退到 `api call`

## 下一步命令

```bash
volclog topic get --topic-id <TopicId>
volclog --output-mode file --output-file ./topic-detail.json topic get --topic-id <TopicId>
```
