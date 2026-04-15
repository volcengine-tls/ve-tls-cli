# Group Handoff

## 适用场景

- shortcut 没命中，但用户表达已经足够锁定 group
- 需要决定“还留在当前 group，还是切到别的 group”

## 必填输入

- 先确认当前需求属于哪个 group

## 可选参数触发词

- 说“project/topic/index/log/metric-topic/host-group/collector”时，优先留在当前 group
- 说“shipper/alarm/trace/consumer-group”这类没有公开 shortcut 的 group 时，直接进入 `capabilities --group`
- 只有用户意图本身不清楚时，才回头看全局 groups

## 字段联动/限制

- 已知 group 时，不要先跑 `capabilities --view groups`
- 先留在当前 group，不要因为 shortcut 不够就跳到别的 group
- `consumer-group` 更偏管理面；真正拉日志通常还要回到 `shard + log`
- `host-group` 和 `collector` 的基础 CRUD 先走 shortcut，超出后再进各自 API 层

## 常见误用

- group 已经明确却先跑全局 groups
- shortcut 不够就直接跨组
- 把 `consumer-group` 当成实际日志消费的数据面

## 下一步命令

```bash
volclog capabilities --group <group> --view text
volclog api <group> <action> --describe
```
