# Index API Only

## 适用场景
本页用于当前 index API-only 动作族；先确认是 config、rebuild、logId 还是普通索引读写，再继续。

## 必填输入
先确认本页“覆盖动作”里的主键，通常是 `TopicId`、任务 ID，或当前查询条件。

## 可选参数触发词
见本页后续的推荐命令和常见误用；如果用户只是在看普通索引读写，优先回到 `index get/create/modify`。

## 字段联动/限制
以本页的动作边界为准；config、rebuild、logId 分布是不同链路，不要混用。

## 常见误用
不要把 API-only 动作混成 shortcut；不要在主键没确认前直接删配置、删任务或改任务。

## 下一步命令
先用本页的推荐命令或对应详情页；如果还不够，再去 `volclog-api-explorer`。


这页只做总索引，不重复展开细节。`index` 组里公开 shortcut 没覆盖、但用户仍可能点名的 API-only 动作，分别落到下面三页：

- config 视图：[`index-config.md`](index-config.md)
- rebuild 任务：[`index-rebuild.md`](index-rebuild.md)
- logid / 分布：[`index-logid.md`](index-logid.md)

## 先用什么时候

- 用户明确说“看配置/原始 config 视图”，先看 config 页
- 用户明确说“重建索引/暂停继续取消任务”，先看 rebuild 页
- 用户明确说“按 LogId 查/看 LogId 分布”，先看 logid 页

## 总体规则

- 这几类动作都不要混进 shortcut 的第一跳
- 如果用户其实只是想看普通索引读写，回到 `volclog index get/create/modify`
- 如果需要更底层 method/path，再去 [`../../volclog-api-explorer/SKILL.md`](../../volclog-api-explorer/SKILL.md)
