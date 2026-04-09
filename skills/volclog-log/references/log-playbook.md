# Log Playbook

这个 reference 用于日志高频场景的首命令选择。

## 场景路由

| 用户场景 | 第一条命令 | 不要先做什么 |
|---|---|---|
| 查一段时间内日志 | `volclog log search --describe` | 不要先跑导出 |
| 导出很多原始日志 | `volclog --output-mode file log export --describe` | 不要先跑分析导出 |
| 做统计/聚合/分析 | `volclog --output-mode file log export-analysis --describe` | 不要继续用 `log export` |
| 写日志 / put logs | `volclog api log PutLogs --describe` | 不要继续留在 shortcut |

## Plain Search

用户说“查某段时间的日志”时，先用：

```bash
volclog log search --describe
```

不要先升级到 `log export`。

## Large Raw Export

用户说“导出很多原始日志”时，先用：

```bash
volclog --output-mode file log export --describe
```

## Analysis Export

用户说“做统计/聚合/分析导出”时，先用：

```bash
volclog --output-mode file log export-analysis --describe
```

高频提醒：

- analysis 列可用性依赖索引字段是否开启 `SqlFlag`
- 新加索引字段通常偏增量生效；旧日志可能查询成功但列值仍为 `null`

## First Filters

如果结果很大，优先：

- 先缩字段
- 再落盘
- 再做后续链式处理

## 未命中时下一步

如果 shortcut 不够：

```bash
volclog capabilities --group log --view text
volclog api log <Action> --describe
```

不要直接退到 `api call`。
