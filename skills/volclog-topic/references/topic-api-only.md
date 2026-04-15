# Topic API Only

## 适用场景

- 处理 `DescribeTopicsWithIndex`
- 处理 `DescribeServerTimezone`
- shortcut 已不覆盖，但用户仍明确停留在 `topic` 组

## 必填输入

- `DescribeTopicsWithIndex` 先确认筛选范围，再看 `--describe`
- `DescribeServerTimezone` 通常无复杂必填输入，先读 action 说明

## 可选参数触发词

- 说“主题和索引一起看”“带索引联查主题”时，优先 `DescribeTopicsWithIndex`
- 说“服务端时区”“查询时区”时，优先 `DescribeServerTimezone`
- 说“我只想列主题 / 拿 TopicId”时，不要补这两个动作，回到 `topic list`

## 字段联动/限制

- `DescribeTopicsWithIndex` 仍是主题视角，不是索引修改入口
- `DescribeServerTimezone` 只用于确认服务端时区，不替代主题详情
- 普通资源读取优先 `topic list/get`，不要把 API-only 动作当成默认读路径

## 常见误用

- 把联查动作当成普通列表动作
- 为了确认时区去改写检索条件
- 为了找 `TopicId` 先跳到 API-only 动作

## 下一步命令

```bash
volclog api topic DescribeTopicsWithIndex --describe
volclog api topic DescribeServerTimezone --describe
```
