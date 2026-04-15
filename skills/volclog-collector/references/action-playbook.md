# Collector Action Playbook

## Rule Management

```bash
volclog collector list --all
volclog collector list --describe
volclog --output-mode file --output-file ./collector-detail.json collector get --rule-id <RuleId>
volclog collector create --describe
volclog collector modify --describe
volclog collector delete --describe
```

需要 body 时：

```bash
volclog collector create --print-request-template=full
volclog --dry-run collector create --request file://req.json
```

## Bind Host Groups

```bash
volclog api collector ApplyRuleToHostGroups --describe
```

适用：已有 `RuleId` 和 `HostGroupIds`，需要绑定关系。

## Parse Helpers

```bash
volclog api collector ParsePath --describe
volclog api collector ParseTime --describe
volclog api collector ParseTime --print-request-template=full
volclog api collector SplitWithQuote --describe
```

适用：先验证采集规则思路，再回头写正式配置。

高频提醒：

- `ParseTime` 的 `TimeFormat` 不要默认按 Go time layout 传参
- 如果 `ParseTime` 报 `InvalidArgument`，优先回到 `--describe` 和模板核格式，不要继续盲试
