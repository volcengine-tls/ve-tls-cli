# Project Workflow

这个 reference 用于项目读写顺序，不用于发现未知 group。

## Read Path

- 列项目:
  `volclog project list --describe`
- 看单项目:
  `volclog project get --describe`

读路径的固定顺序：

1. 先 `list`
2. 再提 `ProjectId`
3. 真正要看单对象时再 `get`

## Write Path

1. `volclog project create --describe`
2. 如果命令支持模板，先打印模板；否则按 `--describe` 中约束组织参数
3. 正式执行

如果是修改项目：

1. 先确认 `ProjectId`
2. 再 `volclog project modify --describe`
3. 按 `guidance` 继续组织参数

## Escalation

项目 shortcut 不覆盖某个接口时，再切到:

`volclog capabilities --group project --view text`

然后用 `api project <action> --describe`

不要直接跳到 `api call`。
