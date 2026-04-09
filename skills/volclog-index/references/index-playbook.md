# Index Playbook

这个 reference 用于“index 相关需求第一次该跑什么命令”。

## 场景路由

| 用户场景 | 第一条命令 | 不要先做什么 |
|---|---|---|
| 看当前索引 | `volclog index get --topic-id <TopicId>` | 不要先跑 create/modify |
| 改索引或字段解析 | `volclog index create --describe` 或 `modify --describe` | 不要直接手写 body |
| 不确定 JSON 怎么写 | `volclog index create --print-request-template=full` | 不要靠记忆拼字段 |

## Read First

查看索引时先用：

```bash
volclog index get --topic-id <TopicId>
```

## Write Path

```bash
volclog index create --describe
volclog index create --print-request-template=full
```

再配合：

```bash
volclog index create --topic-id <TopicId> --request file://index.json
```

普通索引修改不要先退化成 `api index ModifyIndex`。

## 未命中时下一步

```bash
volclog capabilities --group index --view text
volclog api index <Action> --describe
```

不要把索引问题误切到 `topic` 或 `log`。
