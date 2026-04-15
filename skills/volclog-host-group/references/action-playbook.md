# Action Playbook

## 我想先看看有哪些机器组

先用：

```bash
volclog host-group list --all
volclog host-group list --describe
volclog host-group list --all --jmes-filter "HostGroupHostsRulesInfos[].HostGroupInfo.{HostGroupId: HostGroupId, HostGroupName: HostGroupName}"
```

如果返回太深，先从 `HostGroupHostsRulesInfos[].HostGroupInfo` 开始裁剪，不要手翻整棵 JSON。
如果目标是“把机器组都列全”，优先补 `--all`，不要只看默认一页。

## 我想创建或修改机器组

先用：

```bash
volclog host-group create --describe
volclog host-group modify --describe
```

如果是写入型动作，再继续：

```bash
volclog host-group create --print-request-template=full
volclog --dry-run host-group create --request file://req.json
volclog host-group modify --print-request-template=full
volclog --dry-run host-group modify --request file://req.json
```

## 我想看机器组里的机器

先看：

```bash
volclog api host-group DescribeHosts --describe
```

不要把“看成员”误当成“看机器组本体”。

如果先要拿 `HostGroupId`，可以先用上一节的 `--jmes-filter` 范式。

如果是单个对象详情较大，优先配合：

```bash
volclog --output-mode file --output-file ./host-group-detail.json host-group get --host-group-id <HostGroupId>
```

## 我想把规则绑到机器组

先看：

```bash
volclog api host-group ApplyHostGroupToRules --describe
```

如果用户问题更像“把机器组绑到采集规则”，也可以回看 `volclog-collector`，但不要直接跳到别的 group 猜 action。

## 我想调自动更新

先看：

```bash
volclog api host-group ModifyHostGroupsAutoUpdate --describe
```

这类动作通常依赖多个 `HostGroupIds`，不要凭经验硬写 body。
