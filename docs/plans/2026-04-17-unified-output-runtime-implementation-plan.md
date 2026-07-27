# Unified Output Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `tool/workflow/raw` 的输出行为收敛成统一 runtime：默认 `auto`，只接受显式可写 `output_dir`，文件内始终写完整 envelope，`--jmes-filter` 统一作用于完整 envelope 且禁用自动落盘。

**Architecture:** 在现有 `Context + buildAPIEnvelope + artifacts + output.ApplyFilter` 基础上新增一个统一输出决策层。surface 继续产出自己的 payload，但 envelope 组装、大小判定、自动落盘、固定 stdout 提示、`deliveryMode` 写回、`--jmes-filter` 语义统一由同一套 runtime 负责。docs/help/skill 只反映这一套新 contract。

**Tech Stack:** Go、`internal/cli` runtime、`internal/output` JMESPath 处理、README/docs/skills 文本资产、CLI 单测与 `docs/agentic-stage1/stage1_acceptance.sh`。

**Design Principle:** 实施过程中必须持续满足“最小推测、确定性契约、机器可执行的错误、稳定可恢复路径”。任何实现如果重新引入多套输出语义、默认目录猜测、隐式降级或让 Agent 猜下一步怎么恢复，都视为偏离计划。

---

## Scope Guardrails

本计划只做 unified output runtime，不做：

- 新 workflow 能力
- 新 shortcut plane 设计
- 任意新的 name resolver
- 自动猜测可写目录
- 把服务端缓存命中写成契约

本计划必须完成：

- 移除 `tool/workflow/raw` 的 `--output-file`
- 禁止自动写默认输出目录
- 文件内容统一为完整 envelope
- `summary.deliveryMode` 区分 `stdout/file_auto/file_forced`
- `--jmes-filter` 统一作用于完整 envelope
- 存在 `--jmes-filter` 时禁用自动落盘
- 自动落盘时 stdout 只返回固定文本提示

## File Map

### Runtime core

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/run.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/global.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/context.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/envelope.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/artifacts.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/tool_context.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/output/output.go`

### Surface integration

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/tool_exec.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/workflow.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/raw.go`

### Help and docs

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/usage.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/tool_meta.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/workflow_meta.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/README.md`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/README_CN.md`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/docs/cli-best-practices.md`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/docs/cli-practical-guide.md`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/skills/volclog-core/SKILL.md`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/skills/volclog-core/references/best-practices.md`

### Tests

- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/help_test.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/tool_contract_test.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/tool_exec_large_output_test.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/agent_ux_test.go`
- Add: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/internal/cli/output_runtime_test.go`
- Modify: `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/docs/agentic-stage1/stage1_acceptance.sh`

## Wave 1: Freeze Runtime Contract

**Goal:** 先把新的 envelope/file/filter contract 写成测试，再实现最小 runtime 骨架。

- [ ] **Step 1: 写失败测试，锁定 `deliveryMode` 与文件内容语义**

覆盖：

- 小结果 stdout 时，`summary.deliveryMode=stdout`
- 强制 file 时，文件中 envelope 的 `summary.deliveryMode=file_forced`
- 自动落盘时，文件中 envelope 的 `summary.deliveryMode=file_auto`
- 自动落盘写入的是完整 envelope，而不是只写 `data`
- 文件中的 envelope 与 stdout success envelope 共享同一 schema

Run:

```bash
go test ./internal/cli -run '^(TestOutputRuntimeStdoutEnvelopeDeliveryMode|TestOutputRuntimeForcedFileWritesFullEnvelope|TestOutputRuntimeAutoFileWritesFullEnvelope|TestOutputRuntimeFileEnvelopeMatchesStdoutSchema)$' -count=1
```

- [ ] **Step 2: 在 `envelope.go` 新增 `deliveryMode/totalBytes/itemCount`**

实现：

- `summary.deliveryMode`
- `summary.totalBytes`
- `summary.itemCount`
- 由统一 helper 生成，不在各 surface 各自拼装

- [ ] **Step 3: 在 `artifacts.go` / `run.go` 增加目录约束与早期失败路径**

实现：

- 没有显式 `output_dir` 时，不再生成 `.volclog/output`
- file 写入 helper 在缺少目录时返回明确错误
- 自动文件名只接受目录，不接受外部指定文件名
- 对 `file_forced` 或已知 file-first / large-result-first 的 surface，在真正执行远端调用前先校验 `output_dir`
- 不承诺对所有 `auto` 调用都能提前预知大结果

- [ ] **Step 4: 运行定向测试并确认红转绿**

Run:

```bash
go test ./internal/cli -run '^(TestOutputRuntimeStdoutEnvelopeDeliveryMode|TestOutputRuntimeForcedFileWritesFullEnvelope|TestOutputRuntimeAutoFileWritesFullEnvelope|TestOutputRuntimeFileEnvelopeMatchesStdoutSchema|TestOutputRuntimePreflightRejectsMissingOutputDirForKnownFileDelivery)$' -count=1
```

## Wave 2: Remove `--output-file` And Introduce `output_dir`

**Goal:** 删除 agent 主路径上的 `--output-file`，改成“调用方只给目录，CLI 生成文件名”。

- [ ] **Step 1: 写失败测试，锁定 `--output-file` 移除**

覆盖：

- `tool/workflow/raw` 不再接受 `--output-file`
- help 不再出现 `--output-file`
- 缺少 `output_dir` 时 file/auto-file 路径给出明确报错

Run:

```bash
go test ./internal/cli -run '^(TestToolWorkflowRawRejectOutputFile|TestHelpDoesNotMentionOutputFile|TestOutputRuntimeRequiresWritableOutputDirForFileDelivery)$' -count=1
```

- [ ] **Step 2: 修改 `global.go` / `run.go` 的全局输出参数解析**

实现：

- 去掉 `OutputFile`
- `global.go` 不再保留 `--output-file`
- 不新增新的推荐全局输出参数；`tool/workflow` 的 agent 主路径改由 `context.execution.output.*` 承载
- `raw` 如需目录输入，可保留 surface-local `--output-dir`

- [ ] **Step 3: 修改 `Context`、`tool_context.go` 和 surface 执行路径**

实现：

- `Context` 仅持有 `OutputDir`
- `tool_context.go` 支持 `context.execution.output.dir` 与 `context.execution.output.mode`
- `tool_exec.go` / `workflow.go` / `raw.go` 不再写 `ctx.OutputFile`
- 强制 file 仅表达“交付模式”，不表达目标文件名

- [ ] **Step 4: 运行定向测试**

Run:

```bash
go test ./internal/cli -run '^(TestToolWorkflowRawRejectOutputFile|TestHelpDoesNotMentionOutputFile|TestOutputRuntimeRequiresWritableOutputDirForFileDelivery)$' -count=1
```

## Wave 3: Unify `--jmes-filter` On Envelope

**Goal:** 把 filter 语义统一到 envelope，并明确“有 filter 时不自动落盘”。

- [ ] **Step 1: 写失败测试，锁定 envelope filter 语义**

覆盖：

- `--jmes-filter 'data.xxx'` 可工作
- `--jmes-filter 'summary.deliveryMode'` 可工作
- `--jmes-filter 'artifacts[0].path'` 可工作
- 有 filter 时即使结果很大也不自动落盘
- `--jmes-filter` 与强制 file 组合时报 usage error

Run:

```bash
go test ./internal/cli -run '^(TestOutputRuntimeJMESFilterTargetsEnvelope|TestOutputRuntimeFilterDisablesAutoFile|TestOutputRuntimeFilterRejectsForcedFile)$' -count=1
```

- [ ] **Step 2: 修改 `output.ApplyFilter` 调用时机**

实现：

- surface 先产出 payload
- runtime 先组 envelope
- 再对完整 envelope 过滤
- `tool_exec.go` 中现有 projection 仍先作用于 raw result；但全局 `--jmes-filter` 改为作用于 envelope

- [ ] **Step 3: 调整错误提示**

实现：

- filter 失败时，错误语义不再声称是“原始 API 结果”
- help/错误提示改成 envelope 视角
- 不引入长期兼容的 `--jmes-filter-legacy`
- 在帮助与文档中把这次语义切换明确标注为 breaking change

- [ ] **Step 4: 运行定向测试**

Run:

```bash
go test ./internal/cli -run '^(TestOutputRuntimeJMESFilterTargetsEnvelope|TestOutputRuntimeFilterDisablesAutoFile|TestOutputRuntimeFilterRejectsForcedFile)$' -count=1
```

## Wave 4: Auto Spill Routing And Fixed Stdout Text

**Goal:** 统一自动落盘判定和 stdout 固定提示。

- [ ] **Step 1: 写失败测试，锁定 stdout 固定提示**

覆盖：

- 自动落盘 stdout 只输出：
  - `结果过大，已写入文件。`
  - `文件: <path>`
- 强制 file stdout 只输出：
  - `结果已写入文件。`
  - `文件: <path>`
- 不再输出二次 envelope preview

Run:

```bash
go test ./internal/cli -run '^(TestOutputRuntimeAutoFileStdoutNotice|TestOutputRuntimeForcedFileStdoutNotice|TestOutputRuntimeNoPreviewEnvelopeWhenSpilled)$' -count=1
```

- [ ] **Step 2: 在 `run.go` 落地统一 spill 决策**

实现：

- 统一的大结果判定 helper
- auto 路径只在无 filter 时生效
- 自动/主动 file 两条路径共用同一写文件 helper
- stdout 固定提示逻辑集中实现，不散落到各 surface

- [ ] **Step 3: 更新 acceptance 及黑盒用例**

覆盖：

- stdout 小结果
- file_auto
- file_forced
- missing output_dir
- envelope filter

- [ ] **Step 4: 运行定向测试**

Run:

```bash
go test ./internal/cli -run '^(TestOutputRuntimeAutoFileStdoutNotice|TestOutputRuntimeForcedFileStdoutNotice|TestOutputRuntimeNoPreviewEnvelopeWhenSpilled)$' -count=1
```

## Wave 5: Docs, Help, Skill Alignment

**Goal:** 清理所有过期口径，避免实现完成后文档继续误导 agent。

- [ ] **Step 1: 更新 help 文案**

修改：

- `tool exec -h`
- `workflow exec -h`
- `raw -h`

统一说明：

- `output_dir` 必须是显式可写目录
- 文件内容是完整 envelope
- `--jmes-filter` 作用于 envelope
- 有 filter 时不自动落盘
- `--jmes-filter` 从原始结果切换到 envelope 是行为变更

- [ ] **Step 1.5: 更新 `tool describe` / `workflow describe` 契约文案**

修改：

- `tool_meta.go`
- `workflow_meta.go`

统一说明：

- `context.execution.output.dir`
- `context.execution.output.mode`
- `deliveryMode` 的含义
- `--jmes-filter` 基于 envelope，而不是原始 payload

- [ ] **Step 2: 更新 README / 最佳实践 / 实战指南**

移除：

- `--output-file`
- 默认写 `.volclog/output`
- “filter 作用于原始结果” 的旧表述

- [ ] **Step 3: 更新 skill**

补充：

- 看到“结果过大，已写入文件。”时应立即读取文件
- 小结果优先 stdout，大结果再读文件
- envelope filter 的使用方式

- [ ] **Step 4: 运行 help/docs 相关测试**

Run:

```bash
go test ./internal/cli -run '^(TestToolAndWorkflowExecHelpCarryCommonExecutionGuidance|TestHelpDoesNotMentionOutputFile)$' -count=1
```

## Wave 6: Final Verification

**Goal:** 在 claim 完成前跑完整验证，确保 contract、docs、acceptance 一致。

- [ ] **Step 1: 运行 gofmt**

Run:

```bash
gofmt -l .
```

Expected:

- 无输出

- [ ] **Step 2: 运行 targeted acceptance**

Run:

```bash
bash ./docs/agentic-stage1/stage1_acceptance.sh
```

Expected:

- 通过

- [ ] **Step 3: 运行全量测试**

Run:

```bash
go test ./...
```

Expected:

- 通过

- [ ] **Step 4: 黑盒抽样**

Run:

```bash
go run ./cmd/volclog tool exec ...
go run ./cmd/volclog workflow exec ...
go run ./cmd/volclog raw ...
```

检查：

- stdout/file/filter 语义与设计一致

## Acceptance Checklist

- [ ] `tool/workflow/raw` 默认小结果 stdout 行为一致
- [ ] 自动落盘只在显式提供可写 `output_dir` 时发生
- [ ] 对 `file_forced` 与已知 file-first 路径，缺少可写 `output_dir` 时会在远端调用前失败
- [ ] 自动落盘 stdout 只返回固定文本提示
- [ ] 强制 file stdout 只返回固定文本提示
- [ ] 文件内容始终是完整 envelope
- [ ] 文件内 `summary.deliveryMode` 能区分 `stdout/file_auto/file_forced`
- [ ] 文件中的 envelope 与 stdout success envelope 共享同一 schema，agent 可用同一 JSON 解析逻辑读取
- [ ] `--jmes-filter` 针对 envelope 工作
- [ ] 存在 `--jmes-filter` 时不会自动落盘
- [ ] `--jmes-filter` 与强制 file 组合报 usage error
- [ ] README/docs/skill/help 与实现一致

## Self-Review Notes

- 该计划覆盖了 runtime contract、input transport、filter 语义、docs/help/skill 对齐和最终验收
- 没有保留 `--output-file` 相关任务
- 没有依赖默认输出目录
- `raw` 没有被排除在统一 contract 之外

## Execution Handoff

Plan complete and saved to `/Users/bytedance/workspace/src/sdk/github/ve-tls-cli/docs/plans/2026-04-17-unified-output-runtime-implementation-plan.md`.

Two execution options:

1. Subagent-Driven（推荐）- 每个 wave/任务拆给独立子 agent，主线程审查并收口
2. Inline Execution - 在当前线程按 wave 直接实施并逐步验收
