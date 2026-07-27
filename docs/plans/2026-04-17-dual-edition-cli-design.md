# ve-tls-cli Dual Edition CLI Design

## 背景

当前 `ve-tls-cli` 在一套二进制里同时承载两类用户面：

- Agent/自动化主路径：`configure / doctor / skill / tool / workflow / raw`
- 人类快捷路径：`project / topic / metric-topic / index / log / host-group / collector / assistant`

这带来 4 个持续问题：

1. Agent 很容易被 shortcut、`--describe`、`--print-request-template` 吸走，偏离 `tool / workflow / raw` 主路径。
2. 顶层 help 不得不同时解释 agent 与 human 两套心智，入口文案越来越重。
3. human-only 的模板与能力数据仍被编入产物，二进制体积居高不下。
4. `workflow` 当前仍通过 `shortcutSpec` 反推 catalog，导致 Agent 面无法真正脱离 shortcut 依赖。

用户已经确认以下原则：

- 同一套代码可以发布不同类型的 CLI。
- `agent` 版仍需要保留 `configure / doctor / skill`。
- `agent` 与 `full` 两个发行物都应支持全局参数任意位置解析，不能因为 agent 会犯错就强行要求前置。
- 真正需要隔离的是命令面，而不是 parser 容错能力。

## 一句话结论

采用“同仓同码、双发行物、共享解析器、按 surface 编译隔离”方案：默认 `volclog` 面向人类与通用场景，新增 `volclog-agent` 面向 Agent/CI，只保留 `configure / doctor / skill / tool / workflow / raw`，并先把 `workflow` 从 `shortcutSpec` 依赖中解耦出来。

## Agent-friendly 原则

本设计遵循一个总原则：

- Agent 友好的标准是：最小推测、确定性契约、机器可执行的错误、稳定可恢复路径。

落实到 dual-edition 设计中，意味着：

- 不让 Agent 猜应该走 `tool / workflow / raw` 还是 human shortcut。
- 不让 Agent 从顶层 help 学到两套相互竞争的入口心智。
- 不让 Agent 因 surface 混杂而猜测哪些命令是公开契约、哪些只是人类便捷入口。
- 遇到 edition 不支持的命令时，返回明确、可迁移的错误，而不是隐式回退或模糊提示。

## 目标

### 功能目标

1. 发布两个二进制：
   - `volclog`
   - `volclog-agent`
2. `volclog-agent` 只暴露：
   - `configure`
   - `doctor`
   - `skill`
   - `tool`
   - `workflow`
   - `raw`
3. `volclog` 保留现有 full surface。
4. 两个发行物共享同一套：
   - 全局参数解析逻辑
   - `tool / workflow / raw` 运行时行为
   - unified output runtime
5. `workflow` 不再依赖 shortcut metadata 才能工作。

### 体积目标

1. `volclog-agent` 不再编入 human-only shortcut runner/help/meta。
2. `volclog-agent` 不再编入只服务于 shortcut 的模板/说明数据。
3. `volclog-agent` 二进制大小应显著低于 `volclog`。

### 体验目标

1. `volclog-agent -h` 不再出现 shortcut group。
2. `volclog-agent` 的主入口文案可以只围绕 `tool / workflow / raw` 组织，不再混入 human shortcut 提示。
3. agent 默认不会再因为顶层 discover/help 被引导到 `project/topic/log ... --describe`。

## 非目标

本设计不做：

- 重新设计 `tool` contract 本身
- 在本设计文档中单独重新定义 unified output runtime 语义
- 删除 `volclog` full 版中的 shortcut 能力
- 为 internal/private API 新增 agent surface
- 改变版本号语义规则
- 在本轮中统一 npm/homebrew/go install 的全部分发体验

但本设计明确依赖并要求与既有 unified output runtime 方案同步推进。也就是说：

- dual-edition 不负责重新发明 output runtime
- 但 `volclog` 与 `volclog-agent` 必须共享同一套已确认的 output runtime contract
- 不允许先做 agent/full 双发行、再在后续单独对 agent 版补一轮 `auto-routing / deliveryMode / 完整 envelope / file 语义` breaking change

## 方案对比

### 方案 A：单二进制，运行时切模式

做法：

- 一个 `volclog`
- 通过环境变量或隐藏参数切 `agent/full`

优点：

- 发布最简单

缺点：

- human-only 数据仍然编进 agent 模式
- 二进制体积几乎不变
- help、测试、运行时分支复杂度更高
- 误用面只是“隐藏”，不是“消失”

结论：

- 不推荐

### 方案 B：双发行物，编译时隔离 surface

做法：

- `volclog` 维持 full
- `volclog-agent` 只编入 agent surface

优点：

- 命令面真正隔离
- 体积可以实质下降
- help 更干净
- 测试矩阵更清晰

缺点：

- 需要先解耦 `workflow <- shortcutSpec`
- 发布流程需要同时产出两份 asset

结论：

- 推荐方案

### 方案 C：双发行物，但只隐藏命令不裁代码

做法：

- 产出两个名字不同的二进制
- 内部代码和数据几乎完全相同

优点：

- 改动最小

缺点：

- 体积收益接近零
- 实际上只是“文案隔离”

结论：

- 不推荐

## 推荐方案

### 发行物矩阵

| 发行物 | 暴露命令 |
| --- | --- |
| `volclog` | `configure / doctor / skill / tool / workflow / raw / project / topic / metric-topic / index / log / host-group / collector / assistant` |
| `volclog-agent` | `configure / doctor / skill / tool / workflow / raw` |

### Edition 抽象

引入 CLI edition 概念：

- `full`
- `agent`

edition 决定：

1. `cliGroups()` 返回哪些 group
2. 是否注册 human shortcut runner
3. 是否注册 shortcut help / shortcut metadata
4. 是否编入 human-only 生成数据
5. 顶层 help 如何组织入口说明

edition 不决定：

1. 全局参数解析容错
2. `tool / workflow / raw` 运行时语义
3. output runtime 的 contract 本身
4. `configure / doctor / skill` 行为

但 edition 必须建立在统一 output runtime 之上：

- `volclog` 与 `volclog-agent` 的 `tool / workflow / raw` 输出语义必须完全一致
- 统一的 `deliveryMode`、自动落盘、完整 envelope、`--jmes-filter` 语义不能因为 edition 而分叉
- edition 只裁命令面，不裁 `tool / workflow / raw` 的输出 contract

## 共享解析器原则

两个发行物都共享同一套全局参数解析器，且保持容错：

1. 已知全局参数可以出现在任意位置。
2. 解析器先抽取全局参数，再做 group/command 分发。
3. 同名参数冲突时采用 `last wins`。
4. 不因发行物类型改变 parser 规则。

原因：

- Agent 和人类都会犯参数位置错误。
- 真正需要隔离的是命令面，而不是容错能力。
- 如果 `volclog-agent` 再引入“必须前置”的语法，会形成新的心智分裂。

## `workflow` 解耦原则

这是整个方案的前置条件。

### 当前问题

当前 `workflow` catalog 仍通过 `lookupShortcutSpec()` 派生：

- workflow 的 group、command、summary、description、notes、input mode 都来自 shortcut spec
- 这意味着 agent 版如果移除 shortcut，就会同时切断 workflow 数据来源

### 目标状态

`workflow` 必须有独立真相源，不能再依赖 shortcut。

建议做法：

1. 新增独立 workflow catalog source
2. 明确每个 workflow 的：
   - `id`
   - `group`
   - `command`
   - `summary`
   - `description`
   - `input mode`
   - `preferred output mode`
   - `api_group`
   - `api_action`
   - `backed_by`
3. `workflow describe` / `workflow exec` 仅依赖该 catalog
4. full 版里的 shortcut 可以继续“引用 workflow”，但 workflow 不再反向依赖 shortcut

### 设计要求

- workflow 真相源必须能在无 shortcut 的 agent 版单独编译
- workflow 的文案不能靠 shortcut meta 补齐
- workflow 的测试也要转向独立 catalog

## 代码组织建议

### Edition 文件

建议新增：

- `internal/cli/edition.go`
- `internal/cli/edition_full.go`
- `internal/cli/edition_agent.go`

职责：

- 通过 build tag 暴露当前 edition
- 提供 edition-aware 的 group 列表
- 提供 edition-aware 的 help 文案片段
- 提供 edition-aware 的命令注册列表

建议方式：

- `edition_full.go` 使用 full 默认构建约束
- `edition_agent.go` 使用 `//go:build agent`
- `cmd/volclog/main.go` 保持共享入口，不负责 edition 判断
- 真正的 edition 选择在 `internal/cli` 的带 tag 文件内完成

### 命令注册隔离

建议把顶层 group dispatch 收敛成 edition-aware 注册表，而不是继续把所有 group 写死在一个分支里。

目标：

- full 版注册 shortcut runner
- agent 版不注册 shortcut runner

这样可避免：

- `volclog-agent project ...` 进入一段永远不应存在的代码路径
- help 层和 runtime 层再次脱节

### Human-only 数据隔离

human-only 数据至少包括：

- shortcut specs
- generated request templates
- shortcut describe/template 相关能力数据

设计要求：

- full 版编入
- agent 版不编入

如果某些公共数据仍被 shortcut 与 workflow 共用，则先完成 workflow 解耦，再决定是否保留。

## 帮助与文案策略

### `volclog-agent`

顶层 help 只围绕：

- `tool`
- `workflow`
- `raw`
- `configure`
- `doctor`
- `skill`

不应再出现：

- project/topic/log 等 shortcut group
- `--print-request-template`
- “人工快捷命令”说明

### `volclog`

继续保留 full 帮助，但明确：

- shortcut 是 human-first
- agent 默认仍走 `tool / workflow / raw`

## 发布策略

### 版本

- 两个发行物使用同一个语义版本号
- 不新增独立版本线

例如：

- `volclog v1.0.2`
- `volclog-agent v1.0.2`

### Asset 命名

建议：

- `volclog_<version>_<os>_<arch>.tar.gz`
- `volclog-agent_<version>_<os>_<arch>.tar.gz`

### GitHub Actions

发布工作流需要：

1. 对每个 `GOOS/GOARCH` 同时构建两份产物
2. full/agent 使用不同的 build tag 选择 edition
3. release 资产列表同时上传两类产物

推荐构建方式：

- full：
  `go build -o <out> ./cmd/volclog`
- agent：
  `go build -tags=agent -o <out> ./cmd/volclog`

不推荐仅靠 `-ldflags -X ...` 注入 edition，因为：

- ldflags 只能改变运行时值
- 无法实现 human-only surface 的编译期剔除
- 也无法带来我们想要的体积收益

## 测试策略

### Full 版

保留现有主要测试矩阵，重点验证：

- shortcut 仍可用
- `tool / workflow / raw` 不回归

### Agent 版

新增 edition-specific 测试：

1. `cliGroups()` 只返回 agent surface
2. 顶层 help 不出现 shortcut
3. `Run(["project", ...])` 返回 unknown group/command
4. `tool / workflow / raw / configure / doctor / skill` 正常可用
5. 全局参数任意位置解析仍然有效

### 体积测试

发布前比较：

- `volclog`
- `volclog-agent`

至少记录：

- 普通构建体积
- `-s -w` 后体积

## 风险与对策

### 风险 1：workflow 解耦不完整

后果：

- agent 版编译仍隐式依赖 shortcut

对策：

- 把 workflow 解耦作为 Wave 1 前置任务
- 先完成 catalog 独立，再切 edition

### 风险 2：help 隔离了，runtime 没隔离

后果：

- `volclog-agent` 仍能跑 shortcut，只是文案没显示

对策：

- 以 edition-aware 命令注册替代单纯 help 隐藏

### 风险 3：共用 parser 时引入 edition 特判

后果：

- 语法规则再次分裂

对策：

- 明确 parser 为共享层
- edition 只影响命令面，不影响全局参数解析器

### 风险 4：human-only 数据隔离收益不足

后果：

- agent 版体积下降不明显

对策：

- 在解耦后重新盘点：
  - generated tool catalog
  - generated capabilities
  - generated request templates
  - shortcut specs
- 再决定第二轮压缩工作

## 验收标准

### 功能验收

1. `volclog-agent -h` 只出现：
   - `configure`
   - `doctor`
   - `skill`
   - `tool`
   - `workflow`
   - `raw`
2. `volclog-agent project -h` 失败，且不会进入 shortcut help。
3. `volclog-agent tool/workflow/raw` 与 full 版行为一致。
4. 两个发行物都支持全局参数任意位置解析。

### 结构验收

1. `workflow` 不再依赖 `shortcutSpec`。
2. agent 版不编入 human-only shortcut surface。
3. agent 版 help 不再出现 human-only 模板提示。

### 体积验收

1. `volclog-agent` 小于 `volclog`
2. 体积差异中能确认 human-only 数据隔离确实生效

## 最终建议

这套方案值得做，但必须按顺序推进：

1. `workflow` 解耦与 unified output runtime 改造并行推进
2. 在共享 output runtime contract 已冻结的前提下引入 edition
3. 再切发布

否则很容易做成“help 看起来分开了，但代码和体积其实没分开”的半成品。

这里的并行关系是成立的，因为两条线主要改动不同代码路径：

- `workflow` 解耦主要集中在 catalog/source/dispatch/help
- unified output runtime 主要集中在 `Context / run.go / output runtime / envelope / artifacts`

两者的共同约束只有一个：

- edition 上线时，`tool / workflow / raw` 必须已经共享同一套 output runtime 语义
