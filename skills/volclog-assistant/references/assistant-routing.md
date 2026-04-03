# Assistant Routing

## Default Entry

如果用户想看会话回答详情、排查 session answer、定位 AI 助手输出，先跑:

`volclog assistant describe-session-answer --describe`

最小执行闭环：

```bash
volclog assistant describe-session-answer --describe
volclog assistant describe-session-answer --topic-id <TopicId> --question 'What happened?'
```

如果问题很长，优先：

```bash
volclog assistant describe-session-answer --topic-id <TopicId> --question file://./q.txt
```

## When To Upgrade

这些情况升级到 `volclog-api-explorer`:

- 需要实例管理
- 需要 shortcut 未覆盖的 assistant 接口
- 用户明确指定底层 action
