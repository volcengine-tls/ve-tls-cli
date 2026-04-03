# Export Patterns

这个 reference 只回答“什么时候该用 export / export-analysis”，不处理底层 API 选择。

## Raw Log Export

适合原始日志:

1. `volclog log export --describe`
2. `volclog log export --print-request-template=full`
3. 写 `req.json`
4. 正式执行时优先 `--output-mode file`

适用信号：

- 用户明确说“导出很多原始日志”
- 用户想落文件
- 用户不是在做 SQL/聚合结果集分析

## Analysis Export

适合分析型查询:

1. `volclog log export-analysis --describe`
2. 按模板组织查询
3. 输出理解为“行对象结果集”，不是原始日志行

适用信号：

- 查询里有 `select`
- 用户要 count / group by / 聚合统计
- 结果关注的是表格/行对象，而不是原始日志

## Practical Rules

- 大结果默认落盘
- 如果要继续链式处理，先输出文件再由后续步骤读取
- 原始日志和分析结果的心智模型不同，不要混用

## 未命中时下一步

如果两者都不合适，先回：

```bash
volclog log search --describe
```

再判断是否其实是普通检索，不要直接跳到 explorer。
