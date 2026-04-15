# Assistant Routing

## Default Entry

如果用户明确点名了 `volclog assistant` 或 session-answer 命令，再继续。

默认先确认当前环境是否真的暴露 assistant：

`volclog capabilities --view groups`

确认当前环境存在 assistant 能力后，再看：

```bash
volclog assistant describe-session-answer --describe
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

如果 `capabilities --view groups` 里没有 assistant，就不要继续猜命令或 action。
