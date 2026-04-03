# Index Error Quick Reference

这个 reference 只解决 index 高频误用，不处理未知接口探索。

## `--topic-id` 缺失

症状：

- 命令参数不完整
- 明明 body 里写了 `TopicId`，仍然执行不对

做法：

- 始终把 `TopicId` 放在外层 flag：

```bash
volclog index create --topic-id <TopicId> --request file://index.json
```

不要因为 body 里有 `TopicId` 就省略外层 flag。

## 字段名拼错 / 大小写不对

做法：

- 先重新生成模板：

```bash
volclog index create --print-request-template=full
```

- 再按模板改，不要靠记忆手写

## 命令选错

症状：

- 明明只是看索引，却先跑了创建/修改
- 明明只是改字段解析，却先退到了 `api index ModifyIndex`

做法：

- 看现状先用 `volclog index get --topic-id <TopicId>`
- 写入先用 `volclog index create --describe` 或 `modify --describe`
- shortcut 不够才升级

## 高频坑

- body 不是任意 JSON
- 先 `index get` 看现状，再改配置
- 不要先退化成 `api index ModifyIndex`
