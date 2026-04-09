# volclog-cli

[中文版](README_CN.md) | [English](README.md)

火山引擎 TLS（日志服务）官方 CLI 工具，兼顾人类开发者和 AI Agent 的使用场景。覆盖日志项目、主题、索引、检索分析、告警、消费组、数据加工等主要业务域，提供快捷命令、底层 API 调用和可选 skills。

**volclog 提供什么？**

- **Agent 可集成** — 提供一组可选 skills，方便把常见 TLS 操作接到大模型或自动化流程里。
- **覆盖范围完整** — 涵盖 20+ 个业务领域；常见场景可走 shortcut，细粒度场景可直接调用底层 OpenAPI。
- **命令约束更容易看清** — 统一提供 `--describe`、请求模板、`--dry-run` 和结构化输出，适合先看约束再执行。
- **安装和配置路径直接** — 支持二进制安装、源码编译、本地 profile、环境变量和 `--secrets-file`。
- **安全边界相对清楚** — 提供 `--dry-run`、终端输出脱敏和环境隔离相关能力。
- **分层使用** — shortcut / api / skills 三层并存，需要时再逐层下沉。

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

### 快速开始（人类用户）

*如果您是在用 AI 助手协助安装，可以直接跳至 **快速开始（AI Agent）**。*

#### 1. 安装
**方式一：二进制下载（推荐）**
```bash
bash scripts/install-binary.sh
```

**方式二：npm 全局安装**
```bash
npm install -g @volcengine/volclog
```

**方式三：源码编译**
需要 Go 1.22+ 环境。
```bash
git clone https://github.com/volcengine-tls/ve-tls-cli.git
cd ve-tls-cli
make install # 或 bash scripts/install-local.sh
```

#### 2. 配置凭证
执行配置命令并按照提示填入 AK、SK、Region 与 Endpoint：
```bash
volclog configure
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

如果您是 AI Agent，可以按下面这个顺序帮用户完成配置和验证。

#### Step 1 — 安装
```bash
# 如果是在开发环境中
bash scripts/install-local.sh
```

如果你已经发布了 npm 包，也可以直接：

```bash
npm install -g @volcengine/volclog
```

#### Step 2 — 发现能力与约束
对于不熟悉的接口，先通过 `capabilities` 和 `--describe` 看约束，比直接猜参数稳一些：
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
确认无误后去掉 `--dry-run` 再执行。

#### Step 4 — 数据过滤
使用 `--jmes-filter` 提取关键数据，避免上下文被长列表撑爆：
```bash
volclog project list --jmes-filter "Projects[].{Id: ProjectId, Name: ProjectName}"
```

---

## Agent Skills (智能体技能)

仓库里带了一组 Agent Skills，放在 `skills/` 目录下：

| 技能 (Skill) | 描述 |
| --- | --- |
| **volclog-shared** | 通用配置与诊断（所有其他技能的基础），处理环境、认证与通用查询 |
| **volclog-project** | 项目生命周期管理与查询规划 |
| **volclog-topic** | 主题配置与约束校验 |
| **volclog-index** | 索引分析与创建，自动处理全文与键值索引结构 |
| **volclog-log** | 核心日志检索、SQL 分析与大数据导出路由 |
| **volclog-metric-topic** | 指标流查询与 PromQL 支持 |
| **volclog-alarm** | 告警规则排查与配置 |
| **volclog-api-explorer** | 提供底层 OpenAPI 的全量探测与调用能力 |

**安装技能:**
```bash
volclog skill install --dir skills/
```

如果你只是想临时装一次 skill，也可以直接用 `npx`：

```bash
npx @volcengine/volclog skill install --dir skills/
```

---

## 高级与最佳实践

除了基础命令外，还有一些在排障和自动化里比较常用的能力：

- **自动化探索**：通过 `--describe` 与 `--print-request-template` 一键获取接口约束与复杂 JSON 载荷模板。
- **安全验证**：使用 `--dry-run` 拦截网络请求，本地校验 JSON 的合法性及必填参数。
- **灵活输入**：支持通过内联字符串、本地文件（`file://...`）或标准输入（`-`）流式传递 JSON 载荷。
- **排障与脱敏**：结合 `--trace-dir` 与 `--trace-redact strict` 生成脱敏的 Trace 工件包。
- **大数据流式处理**：通过 `--output jsonl` 和 `--output-mode file` 处理海量导出与 SQL 分析结果。
- **多账号隔离**：使用 `--secrets-file ./.env` 注入局部环境变量。

详细的场景指南（包含日志检索、加工、告警、多账号隔离的具体操作命令），已抽取至独立文档中。

👉 **[CLI 参数最佳实践与场景指南](docs/cli-best-practices.md)**
  
如果你更希望按“安装 -> 第一次调用 -> 常见场景实操”的顺序看，也可以直接读：

👉 **[volclog CLI 实战指导](docs/cli-practical-guide.md)**

---

## 贡献与安全

- **Security**: 避免在命令参数中硬编码明文的 AK/SK。请使用 `volclog configure`，或借助环境变量 `VOLCENGINE_ACCESS_KEY_ID`、`VOLCENGINE_ACCESS_KEY_SECRET`，或通过 `--secrets-file <path>` 注入。
- **Contributing**: 我们欢迎提交 PR 来增强 CLI。添加新的 OpenAPI 命令或技能后，请务必运行 `scripts/update_capabilities_contract.sh` 更新接口契约。
