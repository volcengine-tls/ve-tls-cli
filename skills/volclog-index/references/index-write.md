# Index Create/Modify

## 适用场景
本页用于当前 index API-only 动作族；先确认是 config、rebuild、logId 还是普通索引读写，再继续。

## 必填输入
先确认本页“覆盖动作”里的主键，通常是 `TopicId`、任务 ID，或当前查询条件。

## 可选参数触发词
见本页后续的推荐命令和常见误用；如果用户只是在看普通索引读写，优先回到 `index get/create/modify`。

## 字段联动/限制
以本页的动作边界为准；config、rebuild、logId 分布是不同链路，不要混用。

## 常见误用
不要把 API-only 动作混成 shortcut；不要在主键没确认前直接删配置、删任务或改任务。

## 下一步命令
先用本页的推荐命令或对应详情页；如果还不够，再去 `volclog-api-explorer`。


这个 reference 只处理索引写入。

## 适用场景

- 用户说“创建索引 / 修改索引 / 调整字段解析”
- 用户不确定 body 怎么写，或者要从模板起步

## 推荐顺序

1. 先看 `--describe`
2. 再打印模板
3. 再编辑模板文件
4. 最后用 `--request` 提交

## 推荐命令

```bash
volclog index create --describe
volclog index create --print-request-template=full
volclog index create --topic-id <TopicId> --request file://index.json
volclog index modify --describe
```

## 参数约束

- `TopicId` 走外层 `--topic-id`
- `KeyValue` 走当前 CLI 的数组结构
- `KeyValue[].Value` 里才放 `Delimiter`、`CaseSensitive`、`IncludeChinese`、`SqlFlag`

## 关键词触发可选参数

- 说“全文检索”“SQL 查询”“后续要分析”时，补 `SqlFlag`
- 说“分词”“大小写”“中文”“字段解析”时，重点看 `KeyValue[].Value` 里的细项
- 说“字段多”“别手写大 JSON”“先给模板”时，补 `--request` / `--print-request-template=full`
- 说“只改某个主题的索引”时，先补 `--topic-id`

## 字段联动

- 如果后续需要 SQL 查询，优先把对应字段的 `SqlFlag` 打开
- 不要把旧文档里的 `Separator/Quote/Keys` 结构直接搬进来
- 字段名不确定时，先信 CLI 的模板和校验，不要靠记忆补键

## 常见误用

- 不要跳过模板直接手写大 JSON
- 不要省略 `--topic-id`
- 不要把 create 的 body 习惯迁移到 modify

## 何时切换

- 只要还能用模板解决，就不要先去 `api call`
- 如果需要更底层结构说明，再看 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
