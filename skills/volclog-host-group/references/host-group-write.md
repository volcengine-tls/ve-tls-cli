# Host Group Create / Modify / Delete

## 适用场景

- 处理 `CreateHostGroup`
- 处理 `ModifyHostGroup`
- 处理 `DeleteHostGroup`

## 必填输入

- `modify/delete` 必须提供 `HostGroupId`
- `create` 先确认 `HostGroupName` 和 `HostGroupType`

## 可选参数触发词

- 说“IP 机器组”“Label 机器组”时，补 `--host-group-type`
- 说“改机器组成员”“改 IP 列表”时，补 `--host-ip-list` 或 `--host-identifier`
- 说“自动升级”“升级时间窗”时，补 `--auto-update`、`--update-start-time`、`--update-end-time`
- 说“开服务日志”时，补 `--service-logging`

## 字段联动/限制

- `HostGroupType` 会影响后续成员字段
- `AutoUpdate=true` 时，升级时间窗才有意义
- `HostIpList` 和 `HostIdentifier` 面向不同类型机器组，不要混填
- 创建机器组和绑定采集规则是两个动作，不要指望 create 一次做完
- 字段多时优先 `--print-request-template=full`

## 常见误用

- 没有 `HostGroupId` 就直接修改或删除
- 写入字段一多还不切模板
- 把“机器组属性修改”写成“绑定规则”

## 下一步命令

```bash
volclog host-group create --describe
volclog host-group modify --describe
volclog host-group delete --describe
```
