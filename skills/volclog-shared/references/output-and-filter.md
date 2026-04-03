# Output And Filter Rules

这个 reference 解决输出、过滤和 shell 转义问题。

## JMESPath Scope

- `--jmes-filter` 作用于原始结果的 `data` 根，不作用于 CLI envelope。
- 所以要写 `Total`，不要写 `data.Total`。
- 如果最终只想拿数据本体，优先让过滤表达式直接落在数据根上。

Examples:

- 取总数: `--jmes-filter "Total"`
- 列键名: `--jmes-filter "keys(@)"`
- 取数组长度: `--jmes-filter "length(Projects)"`

## Envelope Rules

- `api` 和高频 shortcut 默认会输出统一 envelope。
- 过滤发生在 envelope 之前，而不是之后。
- `--describe` 和 `--print-request-template` 属于元信息输出，优先直接读取其结果，不要把它们当业务数据 envelope 处理。

## Shell Quoting

- `zsh`/`bash`: 表达式里带 `@`、括号、空格时优先整体加双引号
- `fish`: 也优先整体加引号，避免 shell 先展开
- PowerShell: 复杂表达式优先用单引号包起来

Examples:

- `--jmes-filter "keys(@)"`
- `--jmes-filter "length(Topics)"`
- `--jmes-filter "Topics[].TopicName"`

## Large Result Guidance

- 结果大、需要落盘、或后续还要给别的命令消费时，优先 `--output-mode file`
- `log export`、`export-analysis` 这类大结果默认就应该偏向文件模式
- 如果只是 agent 内部做二次处理，也可以先输出到文件再读，避免 stdout 过大
