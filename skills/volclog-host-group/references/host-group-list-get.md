# Host Group List / Get

## 适用场景

- 处理 `DescribeHostGroupsV2`
- 处理 `DescribeHostGroupV2`
- 先拿 `HostGroupId` / `HostGroupName`，再做后续写入

## 必填输入

- `list` 无固定必填输入
- `get` 必须提供 `HostGroupId`

## 可选参数触发词

- 说“列全量”“怕漏页”时，补 `--all`
- 说“按名字查机器组”时，补 `--host-group-name`
- 说“按 ID 查某个机器组”时，补 `--host-group-id`
- 说“结果很多”“只保留字段”时，补 `--output-mode file` 或 `--jmes-filter`

## 字段联动/限制

- `list` 用于找资源，`get` 用于看单对象详情
- `HostGroupId` 适合后续 `modify/delete/binding`
- `HostGroupName` 适合模糊查找，不适合当稳定主键
- `DescribeHostGroupsV2` / `DescribeHostGroupV2` 是当前公开读路径

## 常见误用

- 把 `HostGroupName` 当稳定 ID
- 只看默认一页导致漏掉机器组
- 把列表结果当成成员或绑定详情

## 下一步命令

```bash
volclog host-group list --describe
volclog host-group get --describe
```
