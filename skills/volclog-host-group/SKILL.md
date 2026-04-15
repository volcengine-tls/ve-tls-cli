---
name: volclog-host-group
description: Use when operating TLS host groups with volclog, including Chinese intents such as 机器组、主机纳管、绑定采集规则 or 自动更新 and English intents such as host group, machine group, bind collectors, or host auto update.
---

# volclog Host Group

## Overview

这个 skill 负责 `host-group` 组。基础机器组管理优先走 `host-group` shortcut；查看成员、绑定采集规则、自动更新这类低频或更细动作，再升级到 `api`。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)。

## Agent 快速执行顺序

1. 基础机器组管理先用 `host-group list/get/create/modify/delete`
2. 如果要看主机成员、规则绑定或自动更新，再进入 `capabilities -> api --describe`
3. 写入型动作先模板、再 `--dry-run`

## Agent 禁止行为

- 不要把机器组问题误路由到 `collector` 或 `shipper`
- 不要没确认 `HostGroupId` 就直接改、删、绑定
- 不要把“查看主机成员”和“修改机器组属性”混成一条路径

## Default Recipes

- 看机器组能力：
  `volclog capabilities --group host-group --view text`
- 列机器组：
  `volclog host-group list --describe`
- 列机器组时先裁剪深层输出：
  `volclog host-group list --all --jmes-filter "HostGroupHostsRulesInfos[].HostGroupInfo.{HostGroupId: HostGroupId, HostGroupName: HostGroupName}"`
- 创建机器组：
  `volclog host-group create --describe`
- 修改机器组：
  `volclog host-group modify --describe`
- 绑定规则：
  `volclog api host-group ApplyHostGroupToRules --describe`

## 场景路由

- 用户说“机器组 / machine group / host group”：
  先用 `volclog host-group list --describe`
- 用户说“创建机器组 / 修改机器组”：
  先用 `volclog host-group create --describe` 或 `host-group modify --describe`
- 用户说“看机器组里的主机 / 机器状态 / hosts”：
  先用 `volclog api host-group DescribeHosts --describe`
- 用户说“把规则绑到机器组”：
  先用 `volclog api host-group ApplyHostGroupToRules --describe`
- 用户说“自动更新 / auto update”：
  先用 `volclog api host-group ModifyHostGroupsAutoUpdate --describe`

## Core Rules

- `host-group list/get/create/modify/delete` 是基础机器组管理主路径
- `DescribeHostGroupsV2 / DescribeHostGroupV2` 偏底层资源查看
- `DescribeHosts` 偏机器成员与在线情况查看
- `ApplyHostGroupToRules` 偏规则绑定，不是机器组属性修改
- 自动更新类操作先看 `ModifyHostGroupsAutoUpdate`，不要自己猜字段

## 常用过滤范式

`DescribeHostGroupsV2` 返回层级通常较深，优先用 `HostGroupHostsRulesInfos[].HostGroupInfo` 作为入口路径：

```bash
volclog api host-group DescribeHostGroupsV2 --jmes-filter "HostGroupHostsRulesInfos[].HostGroupInfo.{HostGroupId: HostGroupId, HostGroupName: HostGroupName}"
volclog api host-group DescribeHostGroupsV2 --jmes-filter "HostGroupHostsRulesInfos[].{HostGroupInfo: HostGroupInfo, Rules: RuleInfos}"
```

如果一时不确定字段路径，先用：

```bash
volclog api host-group DescribeHostGroupsV2 --jmes-filter "keys(@)"
```

## Host Group 心智模型

- `HostGroupId` 是机器组稳定标识
- 机器组本体、主机成员、规则绑定是三类不同动作
- 如果用户说“把采集规则绑到机器组”，最终通常会落到 `host-group` 或 `collector` 的绑定动作，先按当前 group 看 `--describe`

## References

- 常见 action 分流：看 [references/action-playbook.md](references/action-playbook.md)

## Common Mistakes

- 没确认 `HostGroupId` 就直接修改或删除
- 把机器组属性修改和规则绑定混为一体
- 想看机器状态，却误走创建/修改动作

## 未命中时下一步

- 先留在 `host-group`：
  `volclog capabilities --group host-group --view text`
- 再锁定 action：
  `volclog api host-group <Action> --describe`
- 不要一上来就跳去 `collector`
