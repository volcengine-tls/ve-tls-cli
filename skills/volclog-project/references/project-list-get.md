# Project List/Get

## 适用场景

- 处理 `DescribeProjects`
- 处理 `DescribeProject`
- 用户说“看有哪些项目”“拿 ProjectId”“看某个项目详情”

## 必填输入

- `list` 无固定必填输入
- `get` 必须提供 `ProjectId`

## 可选参数触发词

- 说“全量”“别漏”“一次看完”时，补 `--all`
- 说“按项目名筛”“我知道名字”时，补 `--project-name`
- 说“按游标继续翻页”时，补 `--cursor`
- 说“只要 ID / 名字 / 少数字段”时，补 `--jmes-filter`
- 说“结果很多”“发文件给我”时，补 `--output-mode file`

## 字段联动/限制

- `list` 用于找资源和拿 `ProjectId`，`get` 用于看单对象详情
- `ProjectId` 是稳定标识，`ProjectName` 只适合过滤
- 复数查询优先考虑 `--all`，避免只看第一页
- 详情很大时优先落文件，不要让长 JSON 占满上下文

## 常见误用

- 把 list 的筛选参数机械迁移到 get
- 把 `ProjectName` 当长期稳定 ID
- 为了拿 `ProjectId` 直接跳到 `api call`

## 下一步命令

```bash
volclog project list --describe
volclog project list --all
volclog project get --describe
volclog --output-mode file --output-file ./project-detail.json project get --project-id <ProjectId>
```
