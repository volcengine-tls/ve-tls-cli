# Index Rebuild

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


## 适用范围

用于 `CreateIndexRebuildTask` / `DescribeIndexRebuildTasks` / `OperateIndexRebuildTask`。

## 什么时候看这个 reference

- 用户说“重建索引”
- 用户说“回建历史日志索引”
- 用户说“暂停 / 继续 / 取消重建任务”

## 推荐命令

```bash
volclog api index CreateIndexRebuildTask --describe
volclog api index DescribeIndexRebuildTasks --describe
volclog api index OperateIndexRebuildTask --describe
```

## 参数约束

- 先确认 `TopicId`
- 再确认任务 ID，再做暂停/恢复/取消
- 这类动作是任务流，不是普通查询

## 常见误用

- 不知道任务 ID 就直接操作
- 把 rebuild 当成普通索引读写
- 以为创建任务后就是同步完成

## 何时切换

- 如果用户只是看当前索引配置，回到 config / shortcut
- 如果需要更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
