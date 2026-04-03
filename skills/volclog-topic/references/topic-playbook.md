# Topic Playbook

这个 reference 用于“topic 相关需求先落到哪个 shortcut”。

## 场景路由

| 用户场景 | 第一条命令 | 不要先做什么 |
|---|---|---|
| 看某项目下主题 | `volclog topic list --describe`，全量时继续 `--all` | 不要先跑 `api topic DescribeTopics` |
| 拿 `TopicId` | `volclog topic list --project-id <ProjectId> --all --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"` | 不要继续靠 `TopicName` 做下游操作 |
| 创建主题 | `volclog topic create --describe` | 不要直接手写大 body |
| 修改主题 | `volclog topic modify --describe` | 不要继续堆很多 flags |

## List Topics Under A Project

```bash
volclog topic list --project-id <ProjectId> --all --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"
```

只拿一个 `TopicId`：

```bash
volclog topic list --project-id <ProjectId> --jmes-filter "Topics[0].TopicId"
```

如果用户只是想确认有没有这个主题，先 list，不要先 get。

如果是看单个主题详情，优先直接落文件：

```bash
volclog --output-mode file --output-file ./topic-detail.json topic get --topic-id <TopicId>
```

## Create Topic

```bash
volclog topic create --describe
volclog topic create --print-request-template=full
```

填好模板后再执行，不要直接猜 body。

## 未命中时下一步

```bash
volclog capabilities --group topic --view text
volclog api topic <Action> --describe
```

不要把 topic 管理误切到 `project` 或 `index`。
