# volclog-cli

[中文版](README_CN.md) | [English](README.md)

火山引擎 TLS 官方 CLI，兼顾人工操作与 AI Agent 使用场景。Agent 与自动化主路径使用契约优先的 `tool / workflow / raw`，full 版 `volclog` 则继续保留面向人工交互的 shortcut。

**volclog 提供什么？**

- **Agent-native 契约主路径** — 通过 `tool describe/exec`、`workflow describe/exec` 与 `raw`，先读契约再执行，而不是先猜 flags 和请求体。
- **双发行版** — 同时提供 `volclog`（full，人类友好）和 `volclog-agent`（agent/CI 优先），两者共享相同的 `tool / workflow / raw` 运行时语义。
- **TLS 覆盖范围完整** — 覆盖项目、主题、索引、检索分析、告警、机器组、采集规则、ETL、消费组等主要领域。
- **执行路径更安全** — `--dry-run`、结构化 envelope、trace 工件与 file delivery 让预检查、验证和恢复更直接。
- **凭证接入更灵活** — 支持本地 profile、显式 region/endpoint、环境变量，以及一次性的 `--secrets-file` 注入。
- **分层使用** — 默认从 `tool / workflow / raw` 开始；只有你明确需要 full 版人工交互层时，才进入 shortcut。

---

## 核心能力

| 分类 | 能力概述 |
| --- | --- |
| 📁 **Project** | 创建、查询、修改和删除日志项目 |
| 📚 **Topic** | 创建、查询、修改和删除日志主题，并管理生命周期配置 |
| 🔍 **Index & Log** | 管理索引、执行日志检索、直方图预览、SQL 分析和大结果导出 |
| 📈 **Metric Topic** | 创建指标主题，并通过 PromQL 兼容接口查询指标 |
| 🚨 **Alarm** | 配置告警策略、模板和通知渠道 |
| 🔄 **Consumer / Shipper** | 管理消费组并把数据投递到下游存储系统 |
| 🛠 **ETL / Processor** | 清洗、富化、分发和转换日志 |
| 🌐 **Host / Collector** | 管理机器组和 LogCollector 采集规则 |

---

## 安装与快速开始

### 准备工作

开始前请确认：

- 已具备可用的终端环境
- 已准备好火山引擎 AK（Access Key ID）与 SK（Secret Access Key）
- 已明确目标 `region`，例如 `cn-beijing`
- 已明确对应的 TLS endpoint，例如 `https://tls-cn-beijing.volces.com`

`region` 必须显式提供。CLI 不会从 endpoint 或域名反推 region。

### 快速开始（AI Agent）

这是 Agent、CI 和自动化推荐的主路径。

#### 1. 安装

推荐直接安装 agent 版二进制：

```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download \
VOLCLOG_EDITION=agent \
bash scripts/install-binary.sh
```

也可以显式传 edition：

```bash
bash scripts/install-binary.sh --edition agent
```

`volclog-agent` 只暴露：

- `configure`
- `doctor`
- `skill`
- `tool`
- `workflow`
- `raw`

如果你通过 npm 安装，现在可以按 edition 选择：

```bash
npm install -g @volcengine-tls/volclog
npm install -g @volcengine-tls/volclog-agent
```

#### 2. 配置并验证

先建立本地 profile，或者注入一次性凭证，然后用 `doctor` 验证：

```bash
volclog configure set \
  --profile default \
  --ak <ak> \
  --sk <sk> \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com

volclog doctor
```

对于无状态执行，优先使用一次性的 `--secrets-file`，不要把大范围环境变量直接灌进整个会话。`--profile` 和 `--secrets-file` 不应在同一条命令里同时出现。

#### 3. 先发现契约，再执行

先用 `tool` 和 `workflow` 读契约，不要先猜输入：

```bash
volclog tool list
volclog tool list project
volclog tool describe project.create

volclog workflow list
volclog workflow describe log.export
```

只有在你已经明确 `method/path` 时，才直接进入 `raw`：

```bash
volclog raw --method POST --path /CreateProject --body file://req.json
```

`raw` 同时接受 `--input` 作为 `--body` 的兼容别名；但 `--body` 和 `--input` 不能同时传。

#### 4. 预执行、执行与过滤

先 dry-run，再正式执行：

```bash
cat >ctx.json <<'EOF'
{
  "region": "cn-beijing",
  "execution": { "dry_run": true }
}
EOF

cat >req.json <<'EOF'
{
  "ProjectName": "demo",
  "Region": "cn-beijing"
}
EOF

volclog tool exec project.create --context file://ctx.json --input file://req.json
```

面对大结果时，优先使用 file delivery：

```bash
volclog --output-mode file --output-dir ./out \
  workflow exec log.export-analysis --input file://req.json
```

如果要对 envelope 做投影，再用 `--jmes-filter`：

```bash
volclog tool exec project.describe-projects \
  --jmes-filter "data.Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}"
```

说明：

- `--jmes-filter` 作用于完整 CLI envelope，所以 `data.*`、`summary.*`、`error.*` 都是合法路径。
- 如果目标字段真实存在但值为 `null`，stdout 会直接输出字面量 `null`，命令仍算成功。
- `--jmes-filter` 不能和 file delivery 同时使用。

### 快速开始（人类用户）

如果你是在终端里直接操作，并且希望使用 shortcut 层，请安装 full 版 `volclog`。

#### 1. 安装

**方式一：二进制下载（推荐）**

```bash
VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download \
bash scripts/install-binary.sh
```

**方式二：npm 全局安装**

```bash
npm install -g @volcengine-tls/volclog
npm install -g @volcengine-tls/volclog-agent
```

**方式三：Go 安装**

需要 Go 1.22+。

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

```bash
volclog configure
```

#### 3. 开始使用

```bash
volclog project list
volclog topic list --project-id <your-project-id>
```

如果你要使用 full 版 shortcut，请看 [docs/cli-human-shortcuts.md](docs/cli-human-shortcuts.md)。

---

## Agent Skills

仓库内置了一套 agent skill，放在 `skills/` 目录下：

| 技能 | 描述 |
| --- | --- |
| **volclog-core** | 仅补充 `tool describe` / `workflow describe` 之外的 agent 增量知识：意图路由、跨 group SOP、运行时语义、恢复策略，以及无状态凭证接入方式 |

安装到你的 agent 技能目录：

```bash
volclog skill install --dir <agent-skills-dir>
```

如果只想临时安装一次，也可以直接用 `npx`：

```bash
npx @volcengine-tls/volclog skill install --dir <agent-skills-dir>
```

---

## 高级与最佳实践

- **Agent 流程优先使用 `tool / workflow / raw`** — 人类 shortcut 仍保留在 full 版，但不是 agent 默认主路径。
- **先看契约再执行** — 从 `tool describe` 或 `workflow describe` 开始，再准备 `context` 与 `input`。
- **写操作优先 `--dry-run`** — 先预览请求形状和运行时选择，再真正发出变更请求。
- **大结果优先 file delivery** — 当 stdout 可能过大时，优先使用 `--output-mode file --output-dir <writable-dir>`。
- **理解运行时信号** — `summary.deliveryMode` 会告诉你结果最终是留在 stdout，还是已自动/强制落文件。
- **失败时读取平铺的错误对象** — 按 `error.kind`、`error.code`、`error.message`、`error.details` 的顺序读取。
- **保持 region 显式** — 不要依赖 endpoint 反推 region。

进一步阅读：

- [docs/cli-practical-guide.md](docs/cli-practical-guide.md)
- [docs/cli-best-practices.md](docs/cli-best-practices.md)
- [docs/cli-human-shortcuts.md](docs/cli-human-shortcuts.md)

---

## 贡献与安全

- **安全** — 避免把明文 AK/SK 直接写进命令参数。优先使用本地 profile、一次性的 `--secrets-file`，或有边界的环境变量注入。
- **region / endpoint 纪律** — 始终显式设置 `region`。CLI 不会从 endpoint 或域名反推。
- **贡献** — 如果你修改了公开 tool catalog，请先重新生成：

  ```bash
  go run ./internal/openapigen --spec repos/docs/swagger.json
  ```

  然后执行：

  ```bash
  go test ./...
  ```
