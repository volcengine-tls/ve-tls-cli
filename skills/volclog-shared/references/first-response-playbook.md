# First Response Playbook

## 适用场景

- 用户只给了一个任务目标，还没指定 action
- 需要第一次响应就先走到更窄、更稳的入口

## 必填输入

- 先识别用户要的是项目、主题、索引、日志、消费还是分析导出

## 可选参数触发词

- 说“先看看有哪些项目”时，先 `project list --all`
- 说“拿某个项目下的主题”时，先 `topic list --project-id ... --all`
- 说“创建主题”时，先 `topic create --describe`
- 说“查看或修改索引”时，先 `index get` / `index create --print-request-template=full`
- 说“查日志”时，先 `log search --describe`
- 说“导出很多日志”时，先 `--output-mode file log export --describe`
- 说“消费日志 / 拉原始日志”时，先 `DescribeShards -> DescribeCursor -> Consume*`
- 说“做分析导出”时，先 `--output-mode file log export-analysis --describe`

## 字段联动/限制

- 第一次响应优先 shortcut，不要先上 `capabilities`
- 复数 `list` 默认优先考虑 `--all`
- 单对象详情默认优先考虑 `--output-mode file`
- 真正的日志消费不是单个 action，而是 `shard + cursor + consume` 链路
- 同一个 group 下不同 action 的参数往往不同，不要横向迁移参数记忆

## 常见误用

- 用户还没说清需求就先跑 `api call`
- 为了拿 ID 直接跳到底层 API
- 把“消费日志”误改写成 `log search` / `log export`

## 下一步命令

```bash
volclog project list --all --jmes-filter "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
volclog topic list --project-id <ProjectId> --all --jmes-filter "Topics[].{TopicId: TopicId, TopicName: TopicName}"
volclog log search --describe
```
