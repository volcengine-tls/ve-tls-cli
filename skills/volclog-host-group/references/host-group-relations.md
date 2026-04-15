# Host Group Relations

## 适用场景

- 查看主机成员、规则绑定关系、自动更新配置
- 处理 `DescribeHosts`、`DescribeBoundRuleIds`、`DescribeHostGroupRules`
- 处理 `ApplyHostGroupToRules`、`DeleteHostGroupFromRules`、`ModifyHostGroupsAutoUpdate` 等 API-only 动作
- 处理 `DescribeConfigTopicsByLabel`
- 处理 `CreateLogCollectorOpsPolicy`、`DescribeLogCollectorOpsPolicy`、`DeleteAgentOpsPolicy`
- 处理 `DescribeLogCollectorSupportVersion`

## 必填输入

- 绝大多数关系动作都先需要 `HostGroupId` 或 `HostGroupIds`
- 绑定/解绑规则时，还需要 `RuleId` 或 `RuleIds`

## 可选参数触发词

- 说“成员”“在线”“心跳”时，优先 `DescribeHosts`
- 说“绑定了哪些规则 ID”时，优先 `DescribeBoundRuleIds`
- 说“这组绑定了什么规则”时，优先 `DescribeHostGroupRules`
- 说“绑定规则”“解绑规则”时，优先 `ApplyHostGroupToRules` / `DeleteHostGroupFromRules`
- 说“自动更新”“升级窗口”时，优先 `ModifyHostGroupsAutoUpdate`
- 说“删机器”“心跳异常主机”时，优先 `DeleteHost` / `DeleteAbnormalHosts`
- 说“按标签看配置主题”时，优先 `DescribeConfigTopicsByLabel`
- 说“agent 运维策略”“collector ops policy”时，优先 `CreateLogCollectorOpsPolicy`、`DescribeLogCollectorOpsPolicy`、`DeleteAgentOpsPolicy`
- 说“支持哪些 agent 版本”“升级支持矩阵”时，优先 `DescribeLogCollectorSupportVersion`

## 字段联动/限制

- `DescribeHosts` 看的是成员和在线状态，不是机器组本体
- `DescribeBoundRuleIds` 只回规则 ID；要看规则详情再切到规则视角
- `ApplyHostGroupToRules` / `DeleteHostGroupFromRules` 需要同时确认机器组和规则范围
- `AutoUpdate`、`UpdateStartTime`、`UpdateEndTime` 是一组联动字段
- `DescribeConfigTopicsByLabel` 是根据标签反查配置主题，不是机器组本体详情
- `CreateLogCollectorOpsPolicy`、`DescribeLogCollectorOpsPolicy`、`DeleteAgentOpsPolicy` 围绕机器组和 agent 运维策略展开
- `DescribeLogCollectorSupportVersion` 用于查看支持矩阵，不是写入动作
- 老版 `DescribeHostGroup` / `DescribeHostGroups` 只作为兼容动作，普通读路径优先 V2 shortcut

## 常见误用

- 把成员查询当成机器组详情
- 把绑定规则当成修改机器组属性
- 只改 `AutoUpdate`，却不一起检查时间窗
- 需要 V2 详情时误退回老版读接口

## 下一步命令

```bash
volclog api host-group DescribeHosts --describe
volclog api host-group DescribeBoundRuleIds --describe
volclog api host-group ApplyHostGroupToRules --describe
volclog api host-group DeleteHostGroupFromRules --describe
volclog api host-group ModifyHostGroupsAutoUpdate --describe
```
