# Index Config

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

用于 `DescribeIndexConfig` / `CreateIndexConfig` / `ModifyIndexConfig` / `DeleteIndexConfig` 这一组原始配置动作。

## 什么时候看这个 reference

- 用户明确说“看原始 config”
- 用户点名 `IndexConfig`
- 用户想先看公开 API 的配置视图，而不是 shortcut 的写入命令

## 推荐命令

```bash
volclog api index DescribeIndexConfig --describe
volclog api index CreateIndexConfig --describe
volclog api index ModifyIndexConfig --describe
volclog api index DeleteIndexConfig --describe
```

## 参数约束

- `TopicId` 仍然是最关键的外层定位键
- config 视图和 `index get` 不是一回事，前者偏原始配置，后者偏 shortcut 读路径
- 改配置前先看 `--describe`，不要直接猜 body 结构

## 常见误用

- 把 `DescribeIndexConfig` 当成 `index get`
- 为了普通索引读写先绕到 config 级 API
- 没确认 `TopicId` 就直接修改或删除 config

## 何时切换

- 如果用户要的是普通索引读写，回到 shortcut
- 如果用户要的是更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
