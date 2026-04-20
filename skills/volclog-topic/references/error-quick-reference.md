# Topic Error Quick Reference

这个 reference 只解决 topic 高频误用，不负责完整流程设计。

## 没有 `ProjectId`

症状：

- 还没确定项目，就开始列主题或创建主题

做法：

先拿项目 ID：

```bash
volclog project list --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
```

如果只是找主题列表，不要直接猜 `ProjectId` 或跳到底层 API。

## `TopicName` 和 `TopicId` 混用

做法：

- 列表场景优先提取 `TopicId/TopicName`
- 下游 index/log 场景优先用 `TopicId`

## 命令选错

症状：

- 普通列主题却先跑了 `capabilities`
- 创建主题却先跑了 `api topic CreateTopic`

做法：

- 列主题先用 `volclog topic list --describe`
- 创建或修改先用 `volclog topic create --describe` / `modify --describe`
- 只有 shortcut 不覆盖时才升级

## 高频坑

- `AutoSplit=true` 时别忘了 `MaxSplitShard`
- 字段多时不要继续堆 flags
- 普通 topic 管理不要先升级到 `api`
