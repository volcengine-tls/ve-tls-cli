# Project Delete

## 适用场景

- 处理 `DeleteProject`
- 用户明确说“删除项目”
- 已经准备先确认目标，再执行删除

## 必填输入

- `ProjectId`

## 可选参数触发词

- 说“我只有项目名”时，不要继续补 delete 参数，先回到 `project list`
- 说“先确认一下再删”时，先做 list/get，再 delete

## 字段联动/限制

- 删除只以 `ProjectId` 为准，不要只靠项目名
- 删除前先确认一次 `ProjectName` 和 `ProjectId` 的对应关系
- 如果当前还没拿到稳定 ID，这一步不应该直接执行

## 常见误用

- 直接拿项目名删除
- 跳过确认步骤
- 为了删除而先退回 `api call`

## 下一步命令

```bash
volclog project list --all
volclog project delete --describe
```
