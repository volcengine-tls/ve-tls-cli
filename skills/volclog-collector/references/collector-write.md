# Collector Create / Modify / Delete

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


## 覆盖动作

- `CreateRule`
- `ModifyRule`
- `DeleteRule`

## 适用范围

用于创建、修改、删除采集规则。

## 优先用法

```bash
volclog collector create --describe
volclog collector modify --describe
volclog collector delete --describe
```

## 参数约束

- `create` 先看 `--describe`，字段多时再切 `--print-request-template=full`。
- `modify/delete` 先确认 `RuleId`。
- `TopicId` 是规则最终写入目标，创建前先确认。
- `InputType`、`LogType`、`Pause` 是规则形态关键字段，别凭经验猜。
- `Paths` 这类批量输入字段更适合先模板后回填。
- `TimeKey` 和 `TimeFormat` 要成对看；涉及时间解析时不要只填一个。
- 如果用户想“先建规则，再绑机器组”，绑定关系是下一步动作，不在 create/modify 本体里一起完成。

## 关键词触发

| 用户说法 | 优先参数 | 用途 |
|---|---|---|
| `创建规则` / `修改规则` | `--describe` / `--print-request-template=full` | 先看字段说明，再组织 body |
| `写到哪个 topic` / `目标主题` | `--topic-id` | 规则最终落点 |
| `宿主机日志` / `K8s stdout` / `K8s 文件` | `--input-type` | 先选采集来源类型 |
| `单行` / `JSON` / `分隔符` / `多行` / `正则` | `--log-type` | 决定抽取方式 |
| `按路径采集` / `采集路径` | `--paths` / `--exclude-paths` | 路径白名单/黑名单 |
| `按时间字段采集` / `时间格式` | `--time-key` / `--time-format` / `--enable-nanosecond` | 时间字段联动 |
| `先建后开` / `先暂停` | `--pause` | 规则先落库后启用 |

## 字段联动

- `TopicId` 决定写入去向。
- `InputType` 和 `LogType` 决定规则走哪种采集形态。
- `Pause` 影响规则是否立即生效；如果用户只是先搭规则，先确认是否要暂停。
- `Paths` / `ExcludePaths` 与文件路径采集形态相关；不是所有 `InputType` 都要填。
- `TimeKey` / `TimeFormat` / `EnableNanosecond` 是一组，先确认日志里是否真的有该时间字段。

## 常见误用

- 先绑机器组，后看规则。
- 只看 `create` 的记忆，就把字段机械搬到 `modify`。
- 把复杂 body 硬拼成一串 flags。
- 以为创建规则就会自动绑定机器组。
- `ParseTime` 已经需要验证时，还跳过解析辅助直接提交正式规则。

## 何时切模板 / api

- 字段超过几项，或需要完整 body 时，优先 `--print-request-template=full`。
- `--request` 适合复杂写入，`--dry-run` 适合正式提交前确认。
- 如果 shortcut 没暴露某个写入字段，再读 `api --describe`。
