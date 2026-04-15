# Topic List

## 适用场景

- 处理 `DescribeTopics`
- 看某个项目下有哪些主题
- 先拿 `TopicId` / `TopicName`

## 必填输入

- 无固定必填输入
- 已知项目范围时，优先提供 `ProjectId`

## 可选参数触发词

- 说“某个项目下”“按项目列主题”时，补 `--project-id`
- 说“主题名过滤”时，补 `--topic-name`
- 说“别漏”“全量”“一次翻完”时，补 `--all`
- 说“只要 ID / 只要名字 / 缩字段”时，补 `--jmes-filter`
- 说“结果很多想落文件”时，补 `--output-mode file`

## 字段联动/限制

- `ProjectId` 是最稳的缩范围方式，没有它时列表范围通常更大
- `TopicName` 和 `TopicId` 不能同时传
- `--all` 用于翻完分页，`--cursor` 用于继续翻页，不是筛选条件
- 如果目标只是拿一个 `TopicId`，先 list 再筛，不要先猜名字

## 常见误用

- 把 `TopicName` 当稳定主键
- 只想列清单却先跑 `topic get`
- 还没看 shortcut 就直接改写成 `api call`

## 下一步命令

```bash
volclog topic list --project-id <ProjectId> --all
volclog topic list --project-id <ProjectId> --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"
```
