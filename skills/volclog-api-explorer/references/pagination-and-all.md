# Pagination And All

## 适用场景

- 使用 generated `api` 的复数 Describe 动作
- 需要判断什么时候该加 `--all`

## 必填输入

- 一个明确的复数 Describe action

## 可选参数触发词

- 说“全量”“别漏”“一次看完”时，考虑 `--all`
- 说“继续翻页”“沿用上次 cursor”时，优先分页参数，不要再叠加 `--all`
- 说“结果很多”时，再补 `--output-mode file`

## 字段联动/限制

- `--all` 只用于 `api <group> <Describe*...s>` 这类复数 Describe action
- `--all` 不用于 `api call`
- 如果已经显式给了 `PageNumber` 或 `Cursor`，不要再加 `--all`
- 聚合后的结果再做 `--jmes-filter`

## 常见误用

- 在单数 `Describe*` 上加 `--all`
- 已经给了分页参数还继续叠 `--all`
- 在 `api call` 上期待 `--all` 生效

## 下一步命令

```bash
volclog api project DescribeProjects --all
volclog api topic DescribeTopics --all
```
