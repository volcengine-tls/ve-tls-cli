# Explore Flow

这个 reference 用于从未知需求收敛到稳定的 `api` 调用。

## Standard Flow

1. 看全局组:
   `volclog capabilities --view groups`
2. 缩小到组:
   `volclog capabilities --group <group> --view text`
3. 看 action 约束:
   `volclog api <group> <action> --describe`
4. 需要 body 时打印模板:
   `volclog api <group> <action> --print-request-template=full`
5. 落盘编辑请求体
6. 用 `--dry-run` 检查 method/path/query/body 预览
7. 再正式执行

## Preferred Decision Rules

- 已知 group/action 时，优先 `api <group> <action>`
- 只有 method/path 已完全确定时，才用 `api call`
- 如果命令本来就有对应 shortcut，先让 shortcut 的 `--describe` 帮你确认输入语义，再决定要不要升级

## Write Path Rules

- body 大于几行时，优先 `--request file://req.json`
- 先模板，后编辑，最后 `--dry-run`
- 对未知字段不要猜，先回到 `--describe` 或模板
