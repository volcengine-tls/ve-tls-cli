# 7. Human Shortcuts

[← 上一篇：进阶](6-Advanced_zh.md) | [English](7-Human-Shortcuts.md) | [下一篇：README →](../README_ZH.md)

`volclog-human` 面向已经明确目标资源和操作的人类终端用户。它与 `volclog` 共享认证和配置，并额外提供 `project`、`topic`、`metric-topic`、`index`、`log`、`host-group`、`collector` 等资源级 shortcut。

## 1. 选择正确入口

| 场景 | 推荐入口 |
| --- | --- |
| 已经知道要列项目、创建 Topic、查日志或看采集规则 | `volclog-human` shortcut |
| 需要单个公开 API 操作的稳定契约 | `volclog-human tool` |
| 需要导入、导出等 CLI 高层流程 | `volclog-human workflow` |
| 写操作需要先做 `--dry-run` | `tool exec` / `workflow exec` |
| 已经明确 HTTP method 和 path | `volclog-human raw` |

这些入口都包含在 `volclog-human` 中。Agent、CI 和脚本应优先使用 `tool` / `workflow` / `raw`，不要依赖 shortcut 参数。

## 2. 从帮助开始

安装方式见[快速开始](1-Getting-Started_zh.md#3-安装)。安装后先验证：

```bash
volclog-human --version
volclog-human --help
volclog-human --profile default doctor
```

不要背诵全部参数，按以下顺序发现当前版本的用法：

```bash
volclog-human topic --help
volclog-human topic create --help
volclog-human topic create --describe
volclog-human index create --print-request-template=required > index-request.json
```

Shortcut 的复杂请求使用 `--request`；`tool exec` 和 `workflow exec` 使用 `--input`。全局身份参数建议放在命令组之前，例如 `volclog-human --profile default project list`。

## 3. 三个常用场景

### 3.1 查看资源

```bash
volclog-human --profile default --output table project list
volclog-human --profile default --output table topic list \
  --project-id <project-id> --all
volclog-human --profile default host-group list --all
volclog-human --profile default collector list \
  --project-id <project-id> --all
volclog-human --profile default host-group get \
  --host-group-id <host-group-id>
volclog-human --profile default collector get --rule-id <rule-id>
```

`--all` 会自动翻完支持的分页，不要与 `--page-number` 或 `--cursor` 同时使用。`table` 仅支持常用的 list/get、`index get` 和 `log search`，并非所有 shortcut 都支持。

采集异常时，先用这些命令定位机器组和规则；需要继续检查主机或绑定关系时，运行 `volclog-human tool describe host-group.describe-hosts` 和 `volclog-human tool describe collector.apply-rule-to-host-groups`。

### 3.2 创建 Topic 和索引

下面的 shortcut 会真实写入。需要预检查时，先切换到公开契约；索引使用同样方式查看 `index.create`：

```bash
volclog-human tool describe topic.create
volclog-human tool describe index.create
volclog-human --profile default --dry-run tool exec topic.create \
  --input '{"ProjectId":"<project-id>","TopicName":"<topic-name>","Ttl":<ttl-days>,"ShardCount":<shard-count>}'
```

确认请求后，再使用 shortcut：

```bash
volclog-human --profile default topic create \
  --project-id <project-id> \
  --topic-name <topic-name> \
  --ttl <ttl-days> \
  --shard-count <shard-count>

volclog-human index create --print-request-template=required > index-request.json
volclog-human --profile default index create \
  --topic-id <topic-id> \
  --request file://index-request.json
```

Shortcut 本身不支持 `--dry-run`；需要预执行时应完成 `tool` / `workflow` 路径，不要把 `--dry-run` 加到 shortcut 上。

### 3.3 检索和导出日志

先用小结果确认查询和时间范围，再用相同查询导出：

```bash
volclog-human --profile default --output table log search \
  --topic-id <topic-id> \
  --query "error" \
  --from <start-time-ms> \
  --to <end-time-ms> \
  --limit 20

volclog-human --profile default \
  --output jsonl --output-mode file --output-dir ./out \
  log export \
  --topic-id <topic-id> \
  --query "error" \
  --from <start-time-ms> \
  --to <end-time-ms> \
  --max-pages <max-pages>
```

普通检索结果使用 `log export`；SQL/分析结果改用 `log export-analysis`。完整的输出、分页和不完整结果语义见[使用](4-Usage_zh.md#7-输出与交付)和[进阶](6-Advanced_zh.md)。

---

[← 上一篇：进阶](6-Advanced_zh.md) | [English](7-Human-Shortcuts.md) | [下一篇：README →](../README_ZH.md)
