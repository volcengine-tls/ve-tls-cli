# Project Create/Modify

## 适用场景

- 处理 `CreateProject`
- 处理 `ModifyProject`
- 用户说“创建项目”“修改项目”

## 必填输入

- `create` 至少先确认项目名称
- `modify` 必须提供 `ProjectId`

## 可选参数触发词

- 说“备注”“说明”时，补 `--description`
- 说“挂到 IAM 项目”“隔离项目”时，补 `--iam-project-name`
- 说“指定地域”时，补 `--region`
- 说“打标签”“按标签管理”时，补 `--tags`
- 说“字段多”“先给模板”“我想贴 JSON”时，补 `--print-request-template=full` 或 `--request`

## 字段联动/限制

- `create` 和 `modify` 的输入不完全相同，不要横向迁移参数记忆
- `modify` 先锁定 `ProjectId`，再考虑改名、改描述、改标签
- 标签适合检索和归类，不适合当稳定标识
- 字段一多就切模板，不要继续把很多写入字段硬塞进命令行

## 常见误用

- 不带 `ProjectId` 就直接修改
- 把 list 过滤字段当成写入字段
- shortcut 明明够用还先退回 `api call`

## 下一步命令

```bash
volclog project create --describe
volclog project modify --describe
volclog project create --print-request-template=full
```
