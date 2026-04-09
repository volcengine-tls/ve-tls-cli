# Index Write Workflow

这个 reference 只处理 index 写入，不处理读取路径。

## Standard Flow

1. 先看约束:
   `volclog index create --describe`
2. 打印模板:
   `volclog index create --print-request-template=full`
3. 编辑模板文件
4. 执行:
   `volclog index create --topic-id <tid> --request file://index.json`

如果是修改：

1. 先 `volclog index get --topic-id <TopicId>`
2. 再 `volclog index modify --describe`
3. 继续走模板，不要靠旧 JSON 直接硬改

## Key Rules

- `TopicId` 走独立 flag，不要在 body 里重复维护
- 优先 `--request`，不要写超长 inline JSON
- CLI 已有顶层字段校验和候选提示，拼错字段时先信任提示

## 不要误走

- 不要为了 index 写入先跑 `capabilities --view groups`
- 不要直接退化成 `api call`
- 不要跳过 `index get` 就盲改现有配置

## Common Failure Modes

- 省略 `--topic-id`
- 键名大小写靠记忆手写
- 直接把旧接口文档里的字段名抄到当前 CLI 模板里
