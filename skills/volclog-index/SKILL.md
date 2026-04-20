---
name: volclog-index
description: Use when reading or changing TLS index configuration with volclog, including Chinese intents such as 查看索引 or 修改索引 and English intents such as get index, update index, or tokenizer settings.
---

# volclog Index

## Overview

索引配置最容易出错的地方是 body 结构和字段名。默认先用模板，再结合 `--topic-id` 提交。

> **前置条件：** 先阅读 [`../volclog-shared/SKILL.md`](../volclog-shared/SKILL.md)，并确认已有 `TopicId`。

## Agent 快速执行顺序

1. 先确认 `TopicId`
2. 先 `index get` 看现状
3. 再 `create/modify --describe`
4. 最后才打印模板并落盘执行

## Agent 禁止行为

- 不要在没有 `TopicId` 时组织索引命令
- 不要跳过模板直接手写大 JSON
- 不要先走 explorer 再回头补模板

## References

- 需要完整索引写入流程、模板方式和常见易错点: 看 [references/index-write-workflow.md](references/index-write-workflow.md)
- 需要第一次就走对的查看/创建配方: 看 [references/index-playbook.md](references/index-playbook.md)
- 需要快速定位字段错误、参数边界和模板注意事项: 看 [references/error-quick-reference.md](references/error-quick-reference.md)

## Default Recipes

- 查看索引:
  `volclog index get --topic-id <TopicId>`
- 创建或修改前先看约束:
  `volclog index create --describe`
- 写 body 时先拿模板:
  `volclog index create --print-request-template=full`

## 场景路由

- 用户说“看当前索引 / 查看字段解析 / 看 tokenizer”：
  先用 `volclog index get --topic-id <TopicId>`
- 用户说“创建索引 / 改索引 / 改字段解析规则”：
  先用 `volclog index create --describe` 或 `volclog index modify --describe`
- 用户说“只是不确定 body 怎么写”：
  直接用 `volclog index create --print-request-template=full`

## Required Workflow

1. 先看约束：`volclog index create --describe`
2. 再生成模板：`volclog index create --print-request-template=full`
3. 编辑模板文件
4. 执行：`volclog index create --topic-id <tid> --request file://index.json`

## Rules

- `TopicId` 由 CLI flag 单独提供，不要在模板里重复维护
- 优先使用 `--request`，不要手写超长 inline JSON
- 如果字段名拼错，先看 CLI 校验提示，不要自己猜

## Index 心智模型

- 索引的高风险点不是命令名，而是 body 结构
- `TopicId` 是外层 flag，body 只关注索引配置本身
- 普通索引操作几乎都应从模板开始

## Common Mistakes

- 不要把索引 JSON 当成任意结构；先生成模板
- 不要省略 `--topic-id`
- 不要把 `tokenizer`、字段名、键名大小写靠记忆来写
- 不要先去探索底层 API，再回头补索引模板

## 未命中时下一步

- shortcut 不够时：
  `volclog capabilities --group index --view text`
- 锁定 action 后：
  `volclog api index <Action> --describe`
- 不要把索引问题误切到 `topic` 或 `log`

## 参数边界

- `TopicId` 走外层 flag，不在 body 里重复维护
- 先生成模板，再编辑 body；不要把旧文档字段直接搬进当前 JSON
- 遇到未知字段优先相信 CLI 校验提示，不要自己发明键名

## KeyValue 完整示例

如果用户要做常见日志字段解析，可以从当前 CLI 模板兼容的 `KeyValue` 数组结构起步，再按需裁剪：

```json
{
  "FullText": {
    "CaseSensitive": false,
    "IncludeChinese": true,
    "Delimiter": " "
  },
  "KeyValue": [
    {
      "Key": "level",
      "Value": {
        "ValueType": "text",
        "Delimiter": " ",
        "CaseSensitive": false,
        "IncludeChinese": true,
        "SqlFlag": true
      }
    },
    {
      "Key": "service",
      "Value": {
        "ValueType": "text",
        "Delimiter": " ",
        "CaseSensitive": false,
        "IncludeChinese": true,
        "SqlFlag": true
      }
    }
  ]
}
```

配套执行方式：

```bash
volclog index create --print-request-template=full
volclog index create --topic-id <TopicId> --request file://index.json
```

额外提醒：

- `TopicId` 仍然走外层 `--topic-id`
- 旧文档里常见的 `KeyValue:{... Keys:[...]}`、`Separator`、`Quote` 结构不要直接搬；当前请求体更容易因 `UnknownParameter` 失败
- `Delimiter`、`CaseSensitive`、`IncludeChinese`、`SqlFlag` 在当前结构里位于 `KeyValue[].Value`
- 如果后续要做 analysis / SQL 查询，优先为相关字段开启 `SqlFlag`
