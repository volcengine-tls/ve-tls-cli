# Intent Routing

## 适用场景

- 用户只描述业务意图，没有明确说 group 或 action
- 需要先把表达映射到稳定 group

## 必填输入

- 至少先判断资源域：`project`、`topic`、`metric-topic`、`index`、`log`、`collector`、`host-group` 或其他 group

## 可选参数触发词

- 说“项目 / project”时，先进 `project`
- 说“主题 / topic”时，先进 `topic`
- 说“指标主题 / PromQL / Prometheus”时，先进 `metric-topic`
- 说“索引 / tokenizer”时，先进 `index`
- 说“日志检索 / 导出 / 分析查询”时，先进 `log`
- 说“消费日志 / cursor / shard / 原始日志”时，先走 `shard + log` 链路
- 说“采集器 / parse time / 机器组绑定”时，先进 `collector`
- 说“机器组 / auto update”时，先进 `host-group`

## 字段联动/限制

- 命中 group 后，优先先看 shortcut 的 `--describe`
- shortcut 不覆盖时，再看 `capabilities --group <group> --view text`
- 锁定 action 后，再看 `api <group> <action> --describe`
- 写入型动作需要 body 时，再 `--print-request-template=full`
- 日志消费是例外，不是单个 action 就结束

## 常见误用

- group 已经很明确，却先跑全局 `capabilities --view groups`
- 把写日志、查日志、消费日志混成一个 group 选择问题
- 因为 shortcut 不够就直接换到别的 group

## 下一步命令

```bash
volclog capabilities --group <group> --view text
volclog api <group> <action> --describe
```
