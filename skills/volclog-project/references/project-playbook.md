# Project Playbook

这个 reference 用于“第一次就选对 project 命令”，不用于底层 API 探索。

## 场景路由

| 用户场景 | 第一条命令 | 不要先做什么 |
|---|---|---|
| 看项目清单 | `volclog project list --describe`，全量时继续 `--all` | 不要先跑 `capabilities` |
| 拿 `ProjectId` | `volclog project list --all --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"` | 不要先跑 `project get` |
| 按完整项目名过滤 | `volclog project list --project-name <name> --all` | 不要直接猜 `ProjectId` |
| 看单个项目详情 | `volclog project get --describe`，详情大时配合 `--output-mode file` | 不要继续在 list 结果里猜字段 |
| 创建项目 | `volclog project create --describe` | 不要先退化成 `api project CreateProject` |

## List Projects

第一次就优先裁成小对象：

```bash
volclog project list --all --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
```

如果用户只需要一个 ID：

```bash
volclog project list --jmes-filter "Projects[0].ProjectId"
```

如果用户已知完整项目名：

```bash
volclog project list --project-name <name> --all
```

如果只知道关键片段，先把完整列表收回来，再用 `--jmes-filter` 或本地过滤。

看详情时，如果返回层级较深，优先直接落文件：

```bash
volclog --output-mode file --output-file ./project-detail.json project get --project-id <ProjectId>
```

## Create Project

先看：

```bash
volclog project create --describe
```

如果参数很多，再根据 `--describe` 或模板组织输入；不要直接回退到底层 API。

## 未命中时下一步

如果 shortcut 不够：

```bash
volclog capabilities --group project --view text
volclog api project <Action> --describe
```

不要离开 `project` 组去别的 group 兜圈子。
