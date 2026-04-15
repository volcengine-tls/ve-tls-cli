# Index LogId

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

用于 `SearchLogId` / `DescribeLogIdDistribution`。

## 什么时候看这个 reference

- 用户说“按 LogId 查日志”
- 用户说“看某批 LogId 分布”
- 用户在排障时需要围绕 LogId 做定位

## 推荐命令

```bash
volclog api index SearchLogId --describe
volclog api index DescribeLogIdDistribution --describe
```

## 参数约束

- 先把查询范围说清楚，再看 LogId 结果
- 这类动作更像排障工具，不替代普通检索
- 不要把它们混成普通索引读写

## 常见误用

- 为了普通查日志先绕到 LogId 动作
- 把分布查询当成索引配置修改
- 没明确排障目标就直接进这组 API

## 何时切换

- 如果用户只是要看索引配置或重建任务，回到对应 reference
- 如果需要更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
