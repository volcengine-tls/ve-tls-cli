# Output And Filter Rules

## 适用场景

- 需要处理 `--jmes-filter`
- 结果太大，想落文件而不是刷 stdout
- shell 转义容易写错

## 必填输入

- 一个目标命令
- 一个明确的过滤或输出目标

## 可选参数触发词

- 说“只要某几个字段”时，补 `--jmes-filter`
- 说“结果很多”“发文件给我”时，补 `--output-mode file`
- 说“shell 转义总出错”时，优先整体加引号

## 字段联动/限制

- `--jmes-filter` 作用于原始结果的 `data` 根，不作用于 CLI envelope
- `--describe` 和 `--print-request-template` 属于元信息输出，不要把它们当业务数据 envelope 处理
- 大结果优先文件模式，尤其是 `log export`、`export-analysis`
- `zsh/bash/fish/PowerShell` 都优先整体加引号，减少 shell 先展开

## 常见误用

- 写 `data.Total` 这种 envelope 路径
- 不先裁字段就让大结果直接打印到 stdout
- 过滤表达式不加引号，导致 shell 先解释

## 下一步命令

```bash
volclog ... --jmes-filter "Total"
volclog ... --jmes-filter "keys(@)"
volclog --output-mode file ...
```
