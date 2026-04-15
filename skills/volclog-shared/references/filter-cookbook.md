# Filter Cookbook

## 适用场景

- 需要快速提取 `ProjectId`、`TopicId`、`Name`
- 想减少模型在大对象上反复试错

## 必填输入

- 一个会返回结构化 JSON 的命令
- 一个明确的字段提取目标

## 可选参数触发词

- 说“只要 ID”“只要名字”“只保留几个字段”时，补 `--jmes-filter`
- 说“结果很大”时，先裁字段，再考虑 `--output-mode file`

## 字段联动/限制

- 过滤写在原始结果根上，不要写 `data.Projects`
- 先取 `Id/Name` 这种稳定字段，不要一开始就保留整个大对象
- 如果结果还是很大，先裁字段，再落盘

## 常见误用

- 在 envelope 根上写过滤表达式
- 一开始就保留完整对象
- 想拿 ID 却不先裁字段

## 下一步命令

```bash
volclog project list --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
volclog topic list --project-id <ProjectId> --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"
```
