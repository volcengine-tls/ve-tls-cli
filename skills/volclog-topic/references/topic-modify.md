# Topic Modify

## 适用场景

- 处理 `ModifyTopic`
- 修改主题基础配置
- 已经确认 `TopicId`

## 必填输入

- `TopicId`

## 可选参数触发词

- 说“改描述”时，补 `--description`
- 说“调整 TTL / 热存储 / 冷存储 / 归档”时，补 `--ttl`、`--enable-hot-ttl`、`--hot-ttl`、`--cold-ttl`、`--archive-ttl`
- 说“自动分裂阈值”“自动扩容”时，补 `--auto-split`、`--max-split-shard`
- 说“换时间字段”“时间格式不对”时，补 `--time-key`、`--time-format`
- 说“加密配置”“标签更新”时，补 `--encrypt-conf`、`--tags`

## 字段联动/限制

- `EnableHotTtl=true` 时，`Ttl` 必须等于 `HotTtl + ColdTtl + ArchiveTtl`
- `EnableHotTtl=false` 时，不要继续补 `HotTtl`、`ColdTtl`、`ArchiveTtl`
- 只改其中一个分层 TTL 时，也要同时检查总 `Ttl`
- `AutoSplit=true` 时仍要检查 `MaxSplitShard`
- `TimeKey` 和 `TimeFormat` 仍然要成对提供
- 字段开始变多时优先切模板

## 常见误用

- 把 create 里的必填项机械迁移到 modify
- 不知道 `TopicId` 就直接修改
- 字段很多时还坚持全手写 flags

## 下一步命令

```bash
volclog topic modify --describe
volclog topic modify --print-request-template=full
```
