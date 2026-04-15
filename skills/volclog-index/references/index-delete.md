# Index Delete

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


这个 reference 只处理索引删除。

## 适用场景

- 用户明确说“删除索引”
- 需要先确认 public CLI 里有没有对应 shortcut
- 看到 `DeleteIndex`、`删索引`、`清掉索引配置`、`不再让历史日志可检索` 这些词时，优先把它归到这里

## 当前策略

- 这组公开 shortcut 不把 delete 作为第一入口
- 如果用户一定要删索引，优先走 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
- 锁定 action 后，再用 `api index DeleteIndex --describe`

## 关键词触发可选参数

- 这个动作几乎没有额外可选参数，核心只看 `--topic-id`
- 如果用户只是说“改字段解析”“调整索引”，不要误进 delete
- 如果用户只是想确认当前索引配置，先回 `index get`

## 常见误用

- 不要把删除索引当成普通的 body 写入
- 不要先假设 shortcut 一定覆盖了 delete
- 不要直接跳过确认就执行删除
