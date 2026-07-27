# Dual Edition CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在同一仓库内产出 `volclog` 与 `volclog-agent` 两个发行物，前者保留 full surface，后者仅保留 `configure / doctor / skill / tool / workflow / raw`，同时保持共享解析器与共享 `tool/workflow/raw` runtime。

**Architecture:** 先把 `workflow` 从 `shortcutSpec` 解耦成独立 catalog，再引入 `edition=full|agent` 抽象，把 group 注册、help 生成和 human-only 数据编译隔离到 edition 层。发布流程最后同时构建两类产物。共享 parser 不做 edition 特判，全局参数仍支持任意位置解析。

**Tech Stack:** Go、`internal/cli`、GitHub Actions release workflow、CLI 单测、黑盒 help 测试、二进制大小对比。

**Design Principle:** 实施过程中必须持续满足“最小推测、确定性契约、机器可执行的错误、稳定可恢复路径”。任何实现如果重新把 Agent 引导回 shortcut、隐藏回退、模糊 edition 边界，或要求 Agent 猜下一步恢复动作，都视为偏离计划。

---

## Scope Guardrails

本计划只做双发行与 surface 隔离，不做：

- 新的 tool/workflow 合约设计
- shortcut 行为重写
- 新增 public API surface
- 独立版本线

本计划必须完成：

- `workflow` 独立 catalog
- dual-edition 与 unified output runtime 共享同一套 `tool / workflow / raw` 输出 contract
- edition 抽象
- `volclog-agent` surface 隔离
- 共享 parser 不分裂
- release 同时产出两类 asset
- agent/full help 与测试矩阵分离

本计划与 [2026-04-17-unified-output-runtime-implementation-plan.md](/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/docs/plans/2026-04-17-unified-output-runtime-implementation-plan.md) 需要并行推进。原因：

- 如果先做 dual-edition，再单独落 unified output runtime，`volclog-agent` 会先携带旧输出语义上线，后续还要承受第二轮 breaking change
- 如果 full/agent 两个发行物的 `tool / workflow / raw` 输出 contract 不一致，Agent 在切换发行物时会混淆

因此本计划要求：

- dual-edition 只裁命令面
- unified output runtime 负责冻结并实现共享的输出 contract
- 在发布 `volclog-agent` 之前，这两条线都必须完成

## File Map

### Workflow decoupling

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/workflow_meta.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/workflow.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/shortcut_meta.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/shortcut_help.go`

### Edition abstraction

- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_full.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_agent.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/cli_meta.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/run.go`

### Human-only surface isolation

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/project.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/topic.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/metric_topic.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/index.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/log.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/host_group.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/collector.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/assistant.go`

### Build and release

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/.github/workflows/release-volclog.yml`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/README.md`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/README_CN.md`

### Tests

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/cli_meta_test.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/help_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_agent_help_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_agent_dispatch_test.go`

## Wave 1A: Decouple Workflow From Shortcut

**Goal:** 让 `workflow` 在没有 shortcut 的情况下也能独立存在。

### Task 1: 写失败测试，锁定 workflow 不再依赖 shortcut spec

**Files:**
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/workflow_meta.go`
- Test: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/help_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_test.go`

- [ ] **Step 1: 写 workflow catalog 独立性测试**

覆盖：

- workflow catalog source 不通过 `lookupShortcutSpec()` 反推
- `workflow list`
- `workflow describe log.export`
- `workflow describe log.export-analysis`
- `workflow describe log.ingest`

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/cli -run '^(TestWorkflowCatalogIsIndependentFromShortcutSpec|TestWorkflowListHelpExplainsCLIWorkflowBoundary)$' -count=1
```

- [ ] **Step 3: 新建独立 workflow catalog 真相源**

实现：

- 在 `workflow_meta.go` 中把 `workflowCatalogSource()` 改成独立静态真相源
- 不再依赖 `lookupShortcutSpec()`
- 显式填写 `id/group/command/action/summary/description/inputMode/preferredOutputMode/apiGroup/apiAction/backedBy/source`

- [ ] **Step 4: 运行定向测试并修正回归**

Run:

```bash
go test ./internal/cli -run '^(TestWorkflowCatalogIsIndependentFromShortcutSpec|TestWorkflowListHelpExplainsCLIWorkflowBoundary)$' -count=1
```

## Wave 1B: Unified Output Runtime Parallel Track

**Goal:** 在 dual-edition 开始裁命令面之前，先并行完成共享 output runtime contract。

**Execution Rule:** 直接按现有 [2026-04-17-unified-output-runtime-implementation-plan.md](/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/docs/plans/2026-04-17-unified-output-runtime-implementation-plan.md) 执行，不在本计划内重复拆细步骤。

**Required Completion Before Wave 2:**

- `tool / workflow / raw` 共享同一套 envelope / file / filter 语义
- `deliveryMode`
- `file_auto / file_forced`
- 完整 envelope 写文件
- `--jmes-filter` 作用于完整 envelope

**Verification Gate:**

至少通过：

```bash
go test ./...
bash ./docs/agentic-stage1/stage1_acceptance.sh
```

## Wave 2: Introduce Edition Abstraction

**Goal:** 建立 `full|agent` edition，不先碰发布，只先让运行时能区分命令面。

### Task 2: 写失败测试，锁定 edition 命令矩阵

**Files:**
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_full.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_agent.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/cli_meta.go`
- Test: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/cli_meta_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_test.go`

- [ ] **Step 1: 写 edition group 测试**

覆盖：

- full edition 包含 shortcut group
- agent edition 只包含 `configure / doctor / skill / tool / workflow / raw`

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/cli -run '^(TestFullEditionGroupsIncludeShortcuts|TestAgentEditionGroupsExcludeShortcuts)$' -count=1
```

- [ ] **Step 3: 引入 edition 抽象**

实现：

- 新增 edition getter
- `edition_full.go` / `edition_agent.go` 使用 build tag 选择 edition
- `cliGroups()` 改成 edition-aware
- `cliGroupNames()` 跟随 edition
- 不要求 `cmd/volclog/main.go` 自己判断 edition；main 保持共享入口

- [ ] **Step 4: 运行定向测试**

Run:

```bash
go test ./internal/cli -run '^(TestFullEditionGroupsIncludeShortcuts|TestAgentEditionGroupsExcludeShortcuts)$' -count=1
```

## Wave 3: Edition-Aware Dispatch And Help

**Goal:** agent 版在 runtime 和 help 上都真正看不到 shortcut。

### Task 3: 写失败测试，锁定 agent 版 help 与 dispatch

**Files:**
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/run.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/usage.go`
- Test: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/help_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_agent_help_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_agent_dispatch_test.go`

- [ ] **Step 1: 写 agent help/dispatch 失败测试**

覆盖：

- `volclog-agent -h` 不出现 shortcut group
- `volclog-agent project -h` 失败
- `volclog-agent tool -h`、`workflow -h`、`raw -h` 正常

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/cli -run '^(TestAgentEditionTopLevelHelpOmitsShortcuts|TestAgentEditionRejectsProjectGroup|TestAgentEditionToolWorkflowRawStillAvailable)$' -count=1
```

- [ ] **Step 3: edition-aware 命令分发**

实现：

- `run.go` 顶层 dispatch 改成依据 edition 决定可注册 group
- 不允许 agent edition 进入 shortcut runner
- 顶层 usageText 按 edition 生成不同帮助内容

- [ ] **Step 4: 运行定向测试**

Run:

```bash
go test ./internal/cli -run '^(TestAgentEditionTopLevelHelpOmitsShortcuts|TestAgentEditionRejectsProjectGroup|TestAgentEditionToolWorkflowRawStillAvailable)$' -count=1
```

## Wave 4: Keep Shared Global Flag Parsing

**Goal:** 确保双发行后 parser 不分裂，全局参数仍支持任意位置。

### Task 4: 写失败测试，锁定共享 parser 行为

**Files:**
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/global.go`
- Test: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_test.go`

- [ ] **Step 1: 写 parser 共享性测试**

覆盖：

- full edition 的 `tool exec ... --dry-run`
- agent edition 的 `tool exec ... --dry-run`
- full edition 的 `raw --output json`
- agent edition 的 `raw --output json`
- 两边都支持前/中/后位置的已知全局参数

- [ ] **Step 2: 运行测试确认失败或确认当前行为**

Run:

```bash
go test ./internal/cli -run '^(TestGlobalFlagsRemainPositionFlexibleInFullEdition|TestGlobalFlagsRemainPositionFlexibleInAgentEdition)$' -count=1
```

- [ ] **Step 3: 如有必要，修正 edition 对 parser 的影响**

实现要求：

- edition 不得影响已知全局参数抽取逻辑
- `last wins` 行为保持一致

- [ ] **Step 4: 运行定向测试**

Run:

```bash
go test ./internal/cli -run '^(TestGlobalFlagsRemainPositionFlexibleInFullEdition|TestGlobalFlagsRemainPositionFlexibleInAgentEdition)$' -count=1
```

## Wave 5: Split Human-Only Data From Agent Build

**Goal:** 让 agent 版不再编入 shortcut-only 数据。

### Task 5: 写失败测试，锁定 agent 版不含 human-only surface

**Files:**
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/shortcut_help.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/shortcut_meta.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/project.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/topic.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/log.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/generated_request_templates.go` or build wiring around it
- Test: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/edition_agent_dispatch_test.go`

- [ ] **Step 1: 写 agent build smoke 测试/脚本**

覆盖：

- agent 版编译成功
- top-level help 不暴露 shortcut
- shortcut 相关路径不会被注册

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/cli -run '^(TestAgentEditionBuildDoesNotExposeShortcutSurfaces)$' -count=1
```

- [ ] **Step 3: 用 edition 隔离 human-only 源**

实现要求：

- shortcut runner/help/meta 仅在 full edition 下参与构建或注册
- agent edition 编译不要求 generated request templates
- 必须使用 build tag 明确区分 full/agent 编译入口

- [ ] **Step 4: 运行定向测试**

Run:

```bash
go test ./internal/cli -run '^(TestAgentEditionBuildDoesNotExposeShortcutSurfaces)$' -count=1
```

## Wave 6: Build And Release Dual Assets

**Goal:** 发布流程同时产出 `volclog` 与 `volclog-agent`。

### Task 6: 更新发布脚本与版本展示

**Files:**
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/.github/workflows/release-volclog.yml`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/version/version.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/README.md`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/README_CN.md`

- [ ] **Step 1: 写失败检查，锁定 release 产物矩阵**

覆盖：

- workflow 里对每个 `GOOS/GOARCH` 产出 2 份资产
- asset 名包含 `volclog-agent`

- [ ] **Step 2: 更新构建命令**

实现：

- full 默认构建
- agent 版通过 `-tags=agent` 注入 edition
- 发布时同时构建两个二进制
- 版本号一致，asset 名不同

- [ ] **Step 3: 更新 README**

实现：

- 说明 `volclog` 与 `volclog-agent` 的命令面差异
- 明确 agent 版仍保留 `configure / doctor / skill`

- [ ] **Step 4: 运行发布前 smoke 命令**

Run:

```bash
go build -o /tmp/volclog-full ./cmd/volclog
go build -tags=agent -o /tmp/volclog-agent ./cmd/volclog
/tmp/volclog-full --version
/tmp/volclog-agent --version
```

## Wave 7: Final Verification

**Goal:** 完成双发行功能、行为和体积验收。

### Task 7: 运行全量验证并记录对比结果

**Files:**
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/docs/plans/2026-04-17-dual-edition-cli-design.md`（如需补最终已知风险）

- [ ] **Step 1: 运行全仓测试**

Run:

```bash
go test ./...
bash ./docs/agentic-stage1/stage1_acceptance.sh
```

- [ ] **Step 2: 构建 full/agent 并检查 help**

Run:

```bash
go build -o /tmp/volclog-full ./cmd/volclog
go build -tags=agent -o /tmp/volclog-agent ./cmd/volclog
/tmp/volclog-full -h
/tmp/volclog-agent -h
```

- [ ] **Step 3: 对比体积**

Run:

```bash
wc -c /tmp/volclog-full /tmp/volclog-agent
go build -ldflags='-s -w' -o /tmp/volclog-full-stripped ./cmd/volclog
go build -tags=agent -ldflags='-s -w' -o /tmp/volclog-agent-stripped ./cmd/volclog
wc -c /tmp/volclog-full-stripped /tmp/volclog-agent-stripped
```

- [ ] **Step 4: 黑盒验证命令矩阵**

Run:

```bash
/tmp/volclog-agent tool list
/tmp/volclog-agent workflow list
/tmp/volclog-agent raw -h
/tmp/volclog-agent project -h
```

Expected:

- 前三条成功
- `project -h` 失败或 unknown group

- [ ] **Step 5: 记录剩余风险**

至少确认：

- workflow 是否完全不再依赖 shortcut
- agent 版是否仍意外带入 human-only 数据
- release workflow 是否已双产物

## Acceptance Checklist

- [ ] unified output runtime 已与 dual-edition 并行完成，且 full/agent 共享同一套 `tool / workflow / raw` 输出 contract
- [ ] `workflow` catalog 独立，不再依赖 `shortcutSpec`
- [ ] `volclog-agent -h` 只显示 `configure / doctor / skill / tool / workflow / raw`
- [ ] `volclog-agent project -h` 不进入 shortcut
- [ ] full/agent 都支持全局参数任意位置解析
- [ ] `tool/workflow/raw` 在两个发行物里行为一致
- [ ] release workflow 同时产出 `volclog` 与 `volclog-agent`
- [ ] `volclog-agent` 体积显著小于 `volclog`

## Known Decision Constraints

1. 不允许把“agent 版更严格”设计成“全局参数必须前置”。
2. `configure / doctor / skill` 必须保留在 agent 版。
3. 如果 workflow 解耦未完成，不得继续推进 edition 隔离。
4. 如果 unified output runtime 共享 contract 未冻结，不得发布 `volclog-agent`。
5. 如果只是 help 隐藏、但 runtime 仍可进入 shortcut，则不算完成。
6. edition 必须由 build tag 选择，不能仅靠 ldflags 注入运行时变量。
