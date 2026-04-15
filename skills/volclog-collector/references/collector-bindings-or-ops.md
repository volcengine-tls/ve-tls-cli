# Collector Bindings or Ops

## 适用场景
本页用于当前 collector API-only 动作族；先判断是规则读写、绑定关系、导入任务还是解析辅助，再看下方专题。

## 必填输入
先确认本页“覆盖动作”里的核心主键，通常是 `RuleId`、`TopicId`、`HostGroupIds`，或导入/解析所需的样例输入。

## 可选参数触发词
见本页后续的关键词触发、推荐命令或常见误用；如果用户只是普通规则管理，优先回到 `collector list/get/create/modify/delete`。

## 字段联动/限制
以本页的参数约束和字段联动为准；绑定、导入、解析辅助和规则本体是不同链路，不要混。

## 常见误用
不要把绑定关系当成规则本体，不要把导入任务当成规则创建，不要在没有主键时直接批量操作。

## 下一步命令
先执行本页或对应专题页给出的推荐命令；如果要更底层字段，再去 `volclog-api-explorer`。


## 适用范围

用于规则绑定/解绑、规则关系查看、以及其他不适合放进 shortcut 的运维辅助动作。

## 覆盖动作

- `DescribeBoundHostGroups`
- `DescribeBoundHostGroupIds`
- `ApplyRuleToHostGroups`
- `DeleteRuleFromHostGroups`
- `DescribeRule`
- `DescribeRules`
- `AssociateInstancesToTLSRole`
- `DisassociateInstancesToTLSRole`
- `DescribeSLSConfigs`
- `GetLogCollectorConfig`
- `LogCollectorHeartbeat`
- `EncryptAccountId`

## 什么时候看这个 reference

- 用户说“绑定机器组 / 解绑机器组”
- 用户说“这条规则绑了哪些机器组”
- 用户说“老版规则视图 / 兼容视图”
- 用户说“TLS 角色绑定实例 / 采集端心跳 / 账号加密”这类运维辅助动作

## 推荐命令

```bash
volclog api collector ApplyRuleToHostGroups --describe
volclog api collector DeleteRuleFromHostGroups --describe
volclog api collector DescribeBoundHostGroups --describe
volclog api collector DescribeBoundHostGroupIds --describe
```

## 参数约束

- 绑定/解绑前先确认 `RuleId` 和 `HostGroupIds`
- 先查绑定关系，再做批量改动
- 老版 `DescribeRule` / `DescribeRules` 只作为兼容视图，普通读路径优先 V2 shortcut

## 常见误用

- 没确认规则范围就直接批量绑机器组
- 把绑定关系当成规则本体
- 明明只是读关系，却先切到 create/modify

## 何时切换

- 如果用户要的是导入配置，回到导入页
- 如果用户要的是路径/时间/正则解析，回到解析辅助页
- 如果需要更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
