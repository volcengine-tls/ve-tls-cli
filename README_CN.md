# volclog-cli

[中文版](README_CN.md) | [English](README.md)

火山引擎 TLS（日志服务）官方 CLI 工具。主路径按 Agent-native 的 `tool / workflow / raw` 设计，同时保留 full 版的人类 shortcut 与模板能力。

**volclog 提供什么？**

- **默认 Agent 优先** — 主执行路径是 `tool / workflow / raw`，不要求 Agent 先学习 shortcut flags。
- **双发行版** — 发布同时提供面向 Agent/CI 的 `volclog-agent`，以及面向人类/完整能力的 `volclog`；两者共享相同的 `tool`、`workflow` 和 `raw` 契约。
- **Agent 可集成** — 提供一组可选 skills，方便把常见 TLS 操作接到大模型或自动化流程里。
- **覆盖范围完整** — 涵盖 20+ 个业务领域；full 版继续保留 shortcut 作为人类高频入口。
- **命令约束更容易看清** — 提供 `tool/workflow describe`、`--dry-run`、结构化输出，以及可选的 shortcut 模板能力，适合先看约束再执行。
- **安装和配置路径直接** — 支持二进制安装、源码编译、本地 profile、环境变量和 `--secrets-file`。
- **安全边界相对清楚** — 提供 `--dry-run`、终端输出脱敏和环境隔离相关能力。
- **分层使用** — shortcut / tool / workflow / raw / skills 多层并存，需要时再逐层下沉。

---

## 核心能力 (Features)

| 分类 | 能力概述 |
| --- | --- |
| 📁 **日志项目 (Project)** | 创建、查询、修改和删除日志项目 |
| 📚 **日志主题 (Topic)** | 创建、查询、修改和删除日志主题，管理日志生命周期 |
| 🔍 **索引与检索 (Index & Log)** | 创建与管理索引，提供强大的日志检索能力、SQL 分析导出、原始日志下载 |
| 📈 **指标主题 (Metric Topic)** | 创建指标主题，支持基于 PromQL 的监控指标查询 |
| 🚨 **告警与通知 (Alarm)** | 配置告警策略、通知渠道，保障业务异常秒级触达 |
| 🖥️ **仪表盘 (Dashboard)** | 创建与管理可视化仪表盘，实时洞察数据变化 |
| 🔄 **消费与投递 (Consumer/Shipper)** | 管理消费组，配置数据投递至外部存储（如 TOS、Kafka 等） |
| 🛠 **数据加工 (ETL/Processor)** | 日志的清洗、富化、分发与格式化处理 |
| 🌐 **机器组与采集 (Host/Collector)** | 管理日志采集端机器组，配置并下发 LogCollector 采集规则 |

---

## 安装与快速开始 (Installation & Quick Start)

### 准备工作

开始前，请确保您已准备好：
- 对应操作系统的终端环境
- 您的火山引擎 AK (Access Key ID) 与 SK (Secret Access Key)
- 目标 Region（如 `cn-beijing`）与 Endpoint（如 `https://tls-cn-beijing.volces.com`）

### 快速开始（AI Agent）

这部分是 Agent / CI / 自动化的主路径。

#### Step 1 — 安装
```bash
# 如果是在开发环境中
bash scripts/install-local.sh
```

发布版的 agent 版本请安装 `volclog-agent`，而不是 full 版 `volclog`：
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download VOLCLOG_EDITION=agent bash scripts/install-binary.sh
```

`volclog-agent` 只保留 `configure`、`doctor`、`skill`、`tool`、`workflow` 和 `raw`。
`project`、`topic`、`log`、`host-group`、`collector` 等 shortcut group 继续留在 full 版 `volclog`。

如果你已经发布了 npm 包，也可以直接：

```bash
npm install -g @volcengine-tls/volclog
```

当前 npm 包安装的是 full 版 `volclog`。如果你需要只暴露 Agent 命令面的版本，请优先安装发布资产里的 `volclog-agent` 二进制。

#### Step 2 — 发现可执行工具与约束
对未知任务先走 `tool / workflow` 契约主链路，再进入 raw 调试层：
```bash
# 查看可见 tool group（公开 API 主入口）
volclog tool list
# 查看领域内 action
volclog tool list project
# 查看可见 workflow group（CLI 编排入口）
volclog workflow list
# 查看某 action 的完整契约
volclog tool describe project.create
# 查看某 workflow 的完整契约
volclog workflow describe log.export
```

如果你已经明确 method/path，就直接走专家级 raw 入口：
```bash
# 直接发原始 transport 调用
volclog raw --method POST --path /CreateProject --body file://req.json
```

#### Step 3 — 预执行与调用
先准备 `context` 与 `input`，并在 `ctx.json` 写入 `execution.dry_run`：
```bash
cat >ctx.json <<'EOF'
{
  "region": "cn-beijing",
  "execution": {"dry_run": true}
}
EOF
cat >req.json <<'EOF'
{
  "ProjectName": "test",
  "Region": "cn-beijing"
}
EOF
volclog tool exec project.create --context file://ctx.json --input file://req.json
```
确认无误后将 `ctx.json` 中的 `dry_run` 去掉再执行。

#### Step 4 — 数据过滤
使用 `--jmes-filter` 提取关键数据，避免上下文被长列表撑爆：
```bash
volclog tool exec project.describe-projects \
  --input '{"ProjectName":"demo"}' \
  --jmes-filter "data.Projects[].{Id: ProjectId, Name: ProjectName}"
```

说明：
- `--jmes-filter` 作用于完整 CLI envelope，所以 `data.*`、`summary.*`、`error.*` 都是合法路径。
- 如果目标字段真实存在但值为 `null`，stdout 会直接输出字面量 `null`，命令仍算成功。
- 失败执行统一使用单层 `error` 对象，优先读取 `error.kind`、`error.code`、`error.message`、`error.details`，不要再去二次解析错误字符串。

---

### 快速开始（人类用户 / Full 版）

如果你是直接在终端里操作的人工用户，请安装 full 版 `volclog`。shortcut 是建立在同一套 `tool / workflow / raw` 基础之上的人类便利层。

#### 1. 安装
**方式一：二进制下载（推荐）**
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download bash scripts/install-binary.sh
```

如果要安装 agent 版：
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download VOLCLOG_EDITION=agent bash scripts/install-binary.sh
# 或
bash scripts/install-binary.sh --edition agent
```

如果你在 Windows PowerShell 下安装：
```powershell
pwsh -File scripts/install.ps1
pwsh -File scripts/install.ps1 -Edition agent
```

**方式二：npm 全局安装**
```bash
npm install -g @volcengine-tls/volclog
```

**方式三：Go 安装**
需要 Go 1.22+ 环境。
```bash
go install github.com/volcengine-tls/ve-tls-cli/cmd/volclog@latest
```

**方式四：本地源码安装**
```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
bash scripts/install-local.sh
```

#### 2. 配置凭证
执行配置命令并按照提示填入 AK、SK、Region 与 Endpoint：
```bash
volclog configure
```

#### 3. 开始使用
```bash
volclog project list
volclog topic list --project-id <your-project-id>
```

如果你想直接走 full 版的人类 shortcut 与模板路径，请看：

👉 **[Human Shortcut Guide](docs/cli-human-shortcuts.md)**

## Agent Skills (智能体技能)

仓库里带了一组可直接安装的 Agent Skill，放在 `skills/` 目录下：

| 技能 (Skill) | 描述 |
| --- | --- |
| **volclog-core** | 仅补充 `tool describe` / `workflow describe` 之外的 Agent 增量知识：意图路由、跨 group SOP、错误恢复、profile 选择、大结果控制 |

`volclog-core` 内部拆成三类薄参考：

- `routing`：自然语言意图到 `tool / workflow / raw` 的路由
- `sops`：跨 group 的常见多步任务编排
- `best-practices`：错误恢复、已知陷阱、token 与大结果控制

**安装技能:**
```bash
volclog skill install --dir /path/to/agent/skills
```

如果你只是想临时装一次 skill，也可以直接用 `npx`：

```bash
npx @volcengine-tls/volclog skill install --dir /path/to/agent/skills
```

---

## 高级与最佳实践

除了基础命令外，还有一些在排障和自动化里比较常用的能力：

- **自动化探索**：通过 `tool describe` / `workflow describe` 获取机器契约；复杂 shortcut 请求体再用 `--print-request-template` 生成骨架。
- **安全验证**：使用 `--dry-run` 拦截网络请求，本地校验 JSON 的合法性及必填参数。
- **灵活输入**：支持通过内联字符串、本地文件（`file://...`）或标准输入（`-`）流式传递 JSON 载荷。
- **排障与脱敏**：结合 `--trace-dir` 与 `--trace-redact strict` 生成脱敏的 Trace 工件包。
- **大数据流式处理**：通过 `--output jsonl` 和 `--output-mode file` 处理海量导出与 SQL 分析结果。
- **多账号隔离**：使用 `--secrets-file ./.env` 注入局部环境变量。

Agent-first 的实战主线，优先看：

👉 **[volclog CLI 实战指导](docs/cli-practical-guide.md)**

共享的运行时、输出、安全与自动化参数说明，优先看：

👉 **[CLI 参数最佳实践与场景指南](docs/cli-best-practices.md)**

如果你明确是在 full 版里做人工 shortcut 操作，再看：

👉 **[Human Shortcut Guide](docs/cli-human-shortcuts.md)**

---

## 贡献与安全

- **Security**: 避免在命令参数中硬编码明文的 AK/SK。请使用 `volclog configure`，或借助环境变量 `VOLCENGINE_ACCESS_KEY_ID`、`VOLCENGINE_ACCESS_KEY_SECRET`，或通过 `--secrets-file <path>` 注入。
- **Contributing**: 我们欢迎提交 PR 来增强 CLI。修改公开 tool catalog 后，请运行 `go run ./internal/openapigen --spec repos/docs/swagger.json` 重新生成，并执行 `go test ./...`。
