# Filter Cookbook

这个 reference 收敛最常见的字段提取，减少模型反复试错。

## Project

```bash
volclog project list --jmes-filter "Projects[0].ProjectId"
volclog project list --jmes-filter "Projects[].ProjectName"
volclog project list --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
```

## Topic

```bash
volclog topic list --project-id <ProjectId> --jmes-filter "Topics[0].TopicId"
volclog topic list --project-id <ProjectId> --jmes-filter "Topics[].TopicName"
volclog topic list --project-id <ProjectId> --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"
```

## General Rules

- 过滤写在原始结果根上，不要写 `data.Projects`
- 先取 `Id/Name` 这种稳定字段，不要一开始就保留整个大对象
- 结果很大时，先裁字段，再考虑是否落盘
