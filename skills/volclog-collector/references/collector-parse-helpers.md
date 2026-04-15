# Collector Parse Helpers

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

用于 `ParsePath` / `ParseTime` / `SplitWithQuote` / `ExtractLogSample` / `GenerateBeginRegex` / `GenerateLogRegex`。

## 什么时候看这个 reference

- 用户说“路径解析”
- 用户说“时间解析 / 时间格式 / 时区”
- 用户说“带引号拆字段 / 分隔符拆分”
- 用户说“先取样例日志，再生成规则”

## 推荐命令

```bash
volclog api collector ParsePath --describe
volclog api collector ParseTime --describe
volclog api collector SplitWithQuote --describe
volclog api collector ExtractLogSample --describe
volclog api collector GenerateBeginRegex --describe
volclog api collector GenerateLogRegex --describe
```

## 参数约束

- `ParseTime` 对 `TimeFormat` 很敏感，不要默认按 Go time layout 传参
- `ParsePath` 先验证路径样例和正则，再回填正式规则
- `SplitWithQuote` 适合先确认分隔符和引号拆分结果，再写正式规则
- 解析类动作先拿样例，再谈正式配置

## 常见误用

- 先写正式规则，后补解析验证
- `ParseTime` 出错后继续盲试时间格式
- 把拆分结果没确认就直接落规则

## 何时切换

- 如果用户其实是在建/改规则，回到 write reference
- 如果需要更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
