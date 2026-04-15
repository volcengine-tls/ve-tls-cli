# Topic Create

## 适用场景

- 处理 `CreateTopic`
- 创建主题
- 字段不多时直接用 shortcut；字段一多时转模板

## 必填输入

- `ProjectId`
- `TopicName`
- `Ttl`
- `ShardCount`

## 可选参数触发词

- 说“备注”“说明”时，补 `--description`
- 说“分层存储”“热/冷/归档”时，补 `--enable-hot-ttl`、`--hot-ttl`、`--cold-ttl`、`--archive-ttl`
- 说“自动分裂”“自动扩容”时，补 `--auto-split`、`--max-split-shard`
- 说“时间字段”“时间格式”时，补 `--time-key`、`--time-format`
- 说“标签管理”时，补 `--tags`
- 说“加密”时，补 `--encrypt-conf`
- 说“记录公网 IP”时，补 `--log-public-ip` / `--no-log-public-ip`

## 字段联动/限制

- `EnableHotTtl=true` 时，`Ttl = HotTtl + ColdTtl + ArchiveTtl`
- `EnableHotTtl=false` 时，不要继续补 `HotTtl`、`ColdTtl`、`ArchiveTtl`
- `AutoSplit=true` 时，`MaxSplitShard` 必填，且要大于 `ShardCount`
- `TimeKey` 和 `TimeFormat` 必须成对提供
- 字段超过几项时优先模板，不要继续堆 flags

## 常见误用

- 只填 `ProjectId/TopicName` 就直接执行
- 把 hot TTL 当成独立配置，忘记总 TTL
- 字段很多时还坚持全手写 flags

## 下一步命令

```bash
volclog topic create --describe
volclog topic create --print-request-template=full
```
