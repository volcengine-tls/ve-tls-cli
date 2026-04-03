# Pagination And All

这个 reference 只关注 generated `api` 的分页策略。

## When `--all` Applies

- 只对 `api <group> <Describe*...s>` 这类复数 Describe action 使用
- 一般是“列举型接口”，名字通常以 `s` 结尾
- 不要把 `--all` 用在 `api call`
- 不要把 `--all` 用在单数 `Describe*`

Examples:

- `volclog api project DescribeProjects --all`
- `volclog api topic DescribeTopics --all`

## Conflict Rules

- 如果已经显式给了 `PageNumber` 或 `Cursor`，不要再加 `--all`
- 聚合后的结果再做 `--jmes-filter`
- 列举接口大结果时，优先配合 `--output-mode file`
