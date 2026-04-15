# Explore Flow

## 适用场景

- 需求已经超出 shortcut，需要稳定收敛到 `api <group> <action>`
- 需要先探索 group，再锁定 action 和 body

## 必填输入

- 至少先知道 group；如果 group 也不清楚，先看 shared routing

## 可选参数触发词

- 说“我知道 group 但不知道 action”时，先 `capabilities --group <group> --view text`
- 说“我要看 body 怎么填”时，补 `--print-request-template=full`
- 说“先别真发，给我看 method/path/body”时，补 `--dry-run`

## 字段联动/限制

- 已知 group/action 时，优先 `api <group> <action>`
- 只有 method/path 已完全确定时，才用 `api call`
- body 大于几行时，优先 `--request file://req.json`
- 对未知字段不要猜，重新执行 `--describe` 和模板打印

## 常见误用

- 还没锁定 group/action 就直接用 `api call`
- body 很大还坚持 inline JSON
- 不先 `--dry-run` 就直接正式执行复杂写入

## 下一步命令

```bash
volclog capabilities --group <group> --view text
volclog api <group> <action> --describe
volclog api <group> <action> --print-request-template=full
```
