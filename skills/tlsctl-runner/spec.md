# tlsctl-runner Spec

面向公众用户的本地可执行 “skills runner”：支持两种输入模式：

- 结构化模式（推荐给智能体平台）：通过 stdin 读取 JSON 请求
- 半结构化自然语言模式（推荐给人类输入）：通过 `--text` 读取“中文 + 参数键值”请求

runner 会自动选择 Profile（account+region → profile），执行白名单动作，并通过 stdout 输出 JSON 响应；默认发行版 runner 内置 `tlsctl`（单文件分发），仅在未内置时才会走自动下载。

目标运行环境：
- Trae/IDE 等本地智能体（工具调用执行本机命令）
- 用户脚本/CI（同样以 stdin/stdout 的方式调用）

## 1. 目标与非目标

目标：
- 支持多账号（account）+ 多 region 的稳定选择与执行
- 不依赖特定 IDE/平台配置，可独立分发一个二进制 runner
- 不要求用户预装 `tlsctl`（发行版 runner 内置；未内置时可自动下载 GitHub Release 并校验）
- 不在对话/日志中处理或回显明文 AK/SK；密钥由用户通过本地 profile 或环境变量自行配置
- 输出结构化 JSON，便于智能体消费与二次处理

非目标：
- 不提供“托管执行”（不代用户保管密钥）
- 不提供任意命令执行（仅白名单 action）
- 不负责创建/同步用户密钥（只引导用户配置 profile/env）

## 2. 用户侧配置约定（多账号多 region）

推荐：
- 同一 account 的不同 region 建多个 profile：`<account>-<region>`（AK/SK 可相同）

示例：
```bash
tlsctl configure set --profile acctA-cn --ak <akA> --sk <skA> --region cn-beijing
tlsctl configure set --profile acctA-sg --ak <akA> --sk <skA> --region ap-singapore-1
tlsctl configure set --profile acctB-cn --ak <akB> --sk <skB> --region cn-beijing
```

说明：
- profile 默认存储：`~/.tlsctl/config.json`
- 可指定：`TLSCTL_CONFIG=/path/to/config.json`

## 3. tlsctl 可用性策略

优先级：
1) `TLSCTL_BIN=/abs/path/to/tlsctl`（用户指定）
2) runner 内置 `tlsctl`（发行版默认）
3) PATH 中存在 `tlsctl`
4) 自动下载 Release 并缓存

自动下载规则：
- `TLSCTL_BASE_URL` 默认 `https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download`
- 可固定版本：`TLSCTL_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/download/tlsctl-v0.0.1`
- 下载文件名：
  - macOS/Linux：`tlsctl_<os>_<arch>.tar.gz` + `.sha256`
  - Windows：`tlsctl_windows_<arch>.zip` + `.sha256`
- 校验：若存在同名 `.sha256` 则必须校验通过，否则拒绝执行
- 缓存目录（可配置）：
  - 默认：`~/.cache/tlsctl-runner/<tag>/`
  - 可配置：`TLSCTL_CACHE_DIR=/path/to/cache`

## 4. Profile 自动匹配（account + region → profile）

runner 输入提供 `account` 与 `region`，runner 需要选出唯一 `profile`。

不强制映射文件，但支持可选映射文件以固化选择：
- `TLSCTL_ACCOUNT_MAP=~/.tlsctl/account_map.json`
```json
{
  "acctA": {
    "cn-beijing": "acctA-cn",
    "ap-singapore-1": "acctA-sg"
  }
}
```

匹配优先级（高→低）：
1) 映射文件命中：`map[account][region]`
2) 读取本地 profiles（建议用 `tlsctl configure list` 输出）并筛选：
   - 候选条件：
     - `profile.region == region`
     - 且 profile name 与 account 匹配（以下任意满足）：
       - `profile == account`
       - `profile` 以 `account-` 或 `account_` 开头
       - `profile` 包含 `account`（弱匹配）
3) 若 `profile.region` 缺失，可 fallback 基于 endpoint 推断 region（例如 `https://tls-<region>.volces.com`）

冲突处理：
- `0` 个候选：返回 `profile_not_found`，并提供创建 profile 的建议命令
- `>1` 个候选：返回 `profile_ambiguous`，并返回候选列表供用户选择/重命名/配置映射文件

## 5. Runner I/O 协议（stdin/stdout + --text）

runner 输出始终为 stdout JSON，便于上层智能体消费。

### 5.1 Request（stdin JSON）

```json
{
  "account": "acctA",
  "region": "cn-beijing",
  "action": "log.search",
  "args": {
    "topic_id": "xxx",
    "query": "error",
    "from_ms": 1710374400000,
    "to_ms": 1710378000000
  },
  "output": "json",
  "dry_run": true,
  "confirm_token": "",
  "idempotency_key": "optional-uuid",
  "requester": {
    "source": "trae|cli|feishu",
    "user_id": "optional",
    "trace_id": "optional"
  }
}
```

字段约束：
- `account` 必填
- `region` 必填
- `action` 必填，必须在白名单中
- `output` 可选：`json|jsonl`，默认 `json`
- `dry_run` 可选：默认 `false`
- `confirm_token`：当 `confirm_required=true` 时，第二次执行必须携带

时间约定：
- 对外输入/输出优先使用毫秒时间戳（`*_ms`），兼容其它格式但不对外展示

### 5.2 Request（--text 半结构化中文 + 参数键值）

目的：
- 让人类可以用更自然的方式输入
- 同时保持足够确定性，避免误解析导致危险操作

语法约定：
- `key=value` 形式的参数对，使用空格分隔
- 允许在参数对之间插入中文描述性文本（runner 可忽略）
- value 支持：
  - 普通字符串（不含空格）
  - 引号字符串：`query="error and timeout"`
  - 数字：`from_ms=1710374400000`
- 关键参数建议都用 `key=value` 提供：`account`、`region`、`action`

示例（推荐写法，稳定）：
```bash
tlsctl-runner --text 'account=acctA region=cn-beijing action=log.search topic_id=xxx query="error" from_ms=1710374400000 to_ms=1710378000000 dry_run=true'
```

示例（允许混入中文）：
```bash
tlsctl-runner --text '帮我查日志 account=acctA region=cn-beijing action=log.search topic_id=xxx query="timeout" from_ms=1710374400000 to_ms=1710378000000'
```

字段映射规则：
- 解析到的 `key=value` 合并为与 stdin JSON 相同的 Request 结构：
  - `account/region/action` → 顶层字段
  - 其它字段默认进入 `args`
  - `dry_run/output/confirm_token/idempotency_key` 仍属于顶层字段

危险操作限制（默认策略）：
- 当 `action` 属于 `delete/modify/create/log.export` 等非只读操作时：
  - 若 `dry_run` 未显式提供，则默认 `dry_run=true`
  - runner 返回 plan，并提示需要二次确认（confirm_token）

### 5.3 Response（stdout JSON）

成功（dry_run=true）：
```json
{
  "data": null,
  "plan": {
    "profile": "acctA-cn",
    "commands": [
      ["tlsctl","--profile","acctA-cn","--output","json","log","search","--topic-id","xxx","--query","error","--from","1710374400000","--to","1710378000000"]
    ],
    "confirm_required": false,
    "confirm_token": ""
  },
  "audit": {
    "account": "acctA",
    "region": "cn-beijing",
    "action": "log.search",
    "duration_ms": 12
  }
}
```

成功（dry_run=false）：
```json
{
  "data": {},
  "plan": null,
  "audit": {
    "account": "acctA",
    "region": "cn-beijing",
    "action": "log.search",
    "profile": "acctA-cn",
    "commands": ["tlsctl ..."],
    "tls_request_id": "optional",
    "status_code": 200,
    "duration_ms": 120
  }
}
```

错误：
```json
{
  "error": {
    "code": "profile_not_found|profile_ambiguous|validation_error|tlsctl_error|internal_error",
    "message": "human readable",
    "details": {},
    "candidates": [],
    "hint": ""
  },
  "audit": {
    "account": "acctA",
    "region": "cn-beijing",
    "action": "log.search"
  }
}
```

## 6. Action 白名单（v0）

覆盖 `tlsctl-v0.0.1` 现有能力，action 命名统一使用小写与下划线/点：

### 6.1 Project
- `project.list` → `tlsctl project list`
- `project.get` → `tlsctl project get --project-id`
- `project.create` → `tlsctl project create ...`
- `project.modify` → `tlsctl project modify ...`（confirm_required）
- `project.delete` → `tlsctl project delete --project-id ...`（confirm_required）

### 6.2 Topic
- `topic.list|get|create|modify|delete`

### 6.3 MetricTopic
- `metric_topic.list|get|create|modify|delete`
- `metric_topic.search`（通过 `/SearchLogs`）
- `metric_topic.prom.query|query_range|series|labels|label_values`

### 6.4 Index
- `index.get|create|modify`

### 6.5 Log
- `log.search`
- `log.export`（建议 confirm_required + 默认 `--output jsonl`）

## 7. 危险操作与确认机制

规则：
- read-only：`list/get/search/prom.query*` → 不需要确认
- write：`create/modify/index.*` → 建议确认
- destructive：`delete`、批量导出 `log.export` → 必须确认

确认 token：
- `confirm_token = HMAC(secret, canonical_request)`（secret 来自环境变量 `TLSCTL_RUNNER_SECRET`）
- token 必须包含短期过期时间（例如 5 分钟），防止复用

## 8. 兼容性与版本

- 仅保证与 `tlsctl` v0.0.x 的 JSON 输出兼容
- runner 自身版本与 `tlsctl` 版本解耦，但默认下载 latest 或指定 tag
