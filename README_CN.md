# volclog-cli

[中文版](README_CN.md) | [English](README.md)

火山引擎 TLS（日志服务）官方 CLI 工具，专为人类开发者与 AI Agent 打造。覆盖日志服务全量核心业务域，包括日志项目、主题、索引、检索分析、告警、消费组、数据加工等 20+ 个领域，支持上百个底层 API 与原生 Agent Skills。

**为什么选择 volclog？**

- **Agent 原生设计** — 开箱即用的 10+ 结构化 Agent Skills，兼容主流大模型工具调用规范，Agent 零成本接入。
- **全量能力覆盖** — 涵盖 20+ 个业务领域，无论是高频的快捷命令，还是全量的底层 OpenAPI，皆可一键触达。
- **AI 友好 & 极致优化** — 每个命令均经过真实的 Agent 测试，具备极简的参数设计、智能的默认值、自解释的 `--describe` 能力，以及标准化的 Envelope JSON 结构化输出，最大化 Agent 调用成功率。
- **开箱即用，无缝接入** — 支持快速安装、交互式配置，从下载到发起第一次 API 请求只需 3 步。
- **安全可控** — 完善的 `--dry-run` 预执行校验、终端输出脱敏与环境隔离设计。
- **三层架构设计** — 快捷命令（人类与 AI 友好） → API 命令（与平台同步，全覆盖） → 智能技能（AI 专属），自由选择合适的调用粒度。

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

说明：
- Project、Topic、Metric Topic、Index、Log、Host Group、Collector、Assistant、Doctor 已提供一等 CLI 命令组。
- Alarm、Dashboard、Consumer、Shipper、ETL/Processor 这类能力当前主要通过 `api` 组与内置 Agent Skills 暴露。

---

## 安装与快速开始 (Installation & Quick Start)

### 准备工作

开始前，请确保您已准备好：
- 对应操作系统的终端环境
- 您的火山引擎 AK (Access Key ID) 与 SK (Secret Access Key)
- 目标 Region（如 `cn-beijing`）与 Endpoint（如 `https://tls-cn-beijing.volces.com`）

### 快速开始（人类用户）

*如果您是正在协助用户安装的 AI 助手，请直接跳至 **快速开始（AI Agent）**。*

#### 1. 安装
**方式一：二进制下载（推荐）**
```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download bash scripts/install-binary.sh
# Windows（PowerShell）:
# powershell -File .\scripts\install.ps1
```

**方式二：源码编译**
需要 Go 1.22+ 环境。
```bash
# 方式 2A：不下载源码直接安装（需要 Go 工具链）
go install github.com/volcengine-tls/ve-tls-cli/cmd/volclog@latest

# 方式 2B：下载源码后构建
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
bash scripts/install-local.sh
# 或
go build -o $HOME/.local/bin/volclog ./cmd/volclog
```

安装完成后建议先验证：
```bash
volclog --version
volclog doctor
```

#### 2. 配置凭证
使用非交互命令初始化一个 profile：
```bash
volclog configure set --profile default --ak <ak> --sk <sk> --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
```

#### 3. 开始使用
```bash
# 查询项目列表
volclog project list
# 查询日志主题
volclog topic list --project-id <your-project-id>
```

---

### 快速开始（AI Agent）

如果您是 AI Agent，可以通过以下标准化流程帮助用户完成配置与验证。

#### Step 1 — 安装
```bash
# 如果是在开发环境中
bash scripts/install-local.sh
```

#### Step 2 — 发现能力与约束
对于不熟悉的接口，Agent 应该**永远**先通过 `capabilities` 和 `--describe` 发现约束，而不是盲目猜测参数：
```bash
# 查看有哪些领域
volclog capabilities --view groups
# 查看具体领域的接口
volclog capabilities --group project --view text
# 查看接口的入参要求、模板与限制
volclog api project CreateProject --describe
```

#### Step 3 — 预执行与调用
通过 `--dry-run` 验证您的请求载荷：
```bash
volclog --dry-run api project CreateProject --request '{"ProjectName":"test", "Region": "cn-beijing"}'
```
确认无误后去掉 `--dry-run` 真正执行。

#### Step 4 — 数据过滤
使用 `--jmes-filter` 提取关键数据，避免上下文被长列表撑爆：
```bash
volclog project list --jmes-filter "Projects[].{Id: ProjectId, Name: ProjectName}"
```

---

## Agent Skills (智能体技能)

我们为流行的 AI 开发流准备了标准的 Agent Skills。您可以在项目的 `skills/` 目录下找到它们：

| 技能 (Skill) | 描述 |
| --- | --- |
| **volclog-shared** | 通用配置与诊断（所有其他技能的基础），处理环境、认证与通用查询 |
| **volclog-api-explorer** | 提供底层 OpenAPI 的全量探测与调用能力 |
| **volclog-project** | 项目生命周期管理与查询规划 |
| **volclog-topic** | 主题配置与约束校验 |
| **volclog-index** | 索引分析与创建，自动处理全文与键值索引结构 |
| **volclog-log** | 核心日志检索、SQL 分析与大数据导出路由 |
| **volclog-metric-topic** | 指标流查询与 PromQL 支持 |
| **volclog-collector** | 管理 LogCollector 采集规则与相关资源 |
| **volclog-host-group** | 管理采集端机器组与分发策略 |
| **volclog-shipper** | 配置数据投递到外部存储（如 TOS、Kafka 等） |
| **volclog-alarm** | 告警规则排查与配置 |
| **volclog-trace** | 生成并管理脱敏 trace 工件，便于排障与复盘 |

**安装技能:**
```bash
volclog skill install --dir skills/
```

---

## 高级与最佳实践

除了基础命令外，`volclog` 为 CLI 和自动化（CI/CD、Agent）提供了众多高级能力，例如：

- **自动化探索**：通过 `--describe` 与 `--print-request-template` 一键获取接口约束与复杂 JSON 载荷模板。
- **安全验证**：使用 `--dry-run` 拦截网络请求，本地校验 JSON 的合法性及必填参数。
- **灵活输入**：支持通过内联字符串、本地文件（`file://...`）或标准输入（`-`）流式传递 JSON 载荷。
- **排障与脱敏**：结合 `--trace-dir` 与 `--trace-redact strict` 生成脱敏的 Trace 工件包。
- **大数据流式处理**：通过 `--output jsonl` 和 `--output-mode file` 处理海量导出与 SQL 分析结果。
- **多账号隔离**：使用 `--secrets-file ./.env` 注入局部环境变量。

详细的场景指南（包含日志检索、加工、告警、多账号隔离的具体操作命令），已抽取至独立文档中。

👉 **[强烈推荐阅读：CLI 参数最佳实践与场景指南](docs/cli-best-practices.md)**

---

## 贡献与安全

- **Security**: 避免在命令参数中硬编码明文的 AK/SK。请使用 `volclog configure`，或借助环境变量 `VOLCENGINE_ACCESS_KEY_ID`、`VOLCENGINE_ACCESS_KEY_SECRET`，或通过 `--secrets-file <path>` 注入。
- **Contributing**: 我们欢迎提交 PR 来增强 CLI。添加新的 OpenAPI 命令或技能后，请务必运行 `scripts/update_capabilities_contract.sh` 更新接口契约。
