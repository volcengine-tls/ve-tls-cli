# 3. 配置

[← 上一篇：认证](2-Authentication_zh.md) | [English](3-Configuration.md) | [下一篇：使用 →](4-Usage_zh.md)

本指南介绍 `volclog` 如何解析配置：文件存放位置、配置档如何选择、`configure set` 如何更新字段、运行时值如何优先，以及当前目录的项目配置如何工作。关于各认证方式的登录、TTL、刷新和登出行为，请参阅 [认证](2-Authentication_zh.md)。

## 1. 配置模型与文件位置

`volclog` 维护两类本地状态：一个共享的配置文件（存放配置档和共享凭证），以及一个可选的、按目录划分的项目配置（存放非敏感的运行时默认值）。

| 文件 | 默认位置 | 用途 | 创建时权限 |
| --- | --- | --- | --- |
| 配置文件 | `$HOME/.volclog/config.json` | 配置档、共享凭证、SSO 会话、`current_profile` | 目录 `0700`，文件 `0600` |
| 项目配置 | `.volclog/cli.config.json`（当前工作目录） | 当前目录的非敏感运行时默认值 | 目录 `0700`，文件 `0600` |

CLI 在创建新路径时请求 `0700` 目录和 `0600` 文件。已有的目录和文件可能保留其当前权限，因为 `MkdirAll`、`WriteFile` 和追加打开不会修复已有的、权限过宽的模式。必要时请检查并收紧已有路径的权限。

配置文件路径可用 `VOLCLOG_CONFIG` 环境变量覆盖：

```bash
VOLCLOG_CONFIG=/path/to/config.json volclog configure list
```

项目配置仅从当前工作目录加载。CLI **不会**向上遍历父目录查找 `.volclog/cli.config.json`；如果工作目录中不存在该文件，则不应用任何项目配置。

项目配置不得包含凭证。如果顶层键中出现 `access_key_id`、`secret_access_key`、`security_token`、`ak`、`sk` 或 `token` 中的任意一个，加载将失败并报错 `project config must not contain credentials`。

## 2. 配置档选择与管理

一个配置档存储一个身份及其 TLS 运行时配置。业务命令按以下顺序选择配置档：

1. 命令行上显式传入的 `--profile NAME`；
2. 配置文件中的 `current_profile`；
3. 名为 `default` 的配置档。

### 2.1 列出、查看、切换与删除

```bash
volclog configure list
volclog configure list --prefix prod
volclog configure show --profile default
volclog configure use prod
volclog configure delete --profile scratch
volclog configure delete --prefix temp- --yes
```

`configure list` 和 `configure show` 永远不会打印密钥。访问密钥 ID 会被掩码（例如 `AKT****XYZ`）；秘密访问密钥则完全不输出。`configure show --profile NAME` 查看单个配置档；不带 `--profile` 时回退到 `current_profile`，再回退到 `default`。

`configure use NAME` 将 `current_profile` 设为 `NAME`。`configure delete` 按名称删除单个配置档，或删除名称以 `--prefix` 开头的所有配置档（按前缀删除需要 `--yes`）。删除后，如果 `current_profile` 指向被删除的配置档，会自动调整。

`configure profile` 子命令组是一组别名：

| 别名 | 等价于 |
| --- | --- |
| `configure profile add NAME [flags...]` | `configure set --profile NAME [flags...]` |
| `configure profile use NAME` | `configure use NAME` |
| `configure profile show [NAME]` | `configure show --profile NAME` |
| `configure profile list [args...]` | `configure list [args...]` |
| `configure profile delete [NAME]` | `configure delete [NAME]` |

### 2.2 显式验证

在验证阶段，建议在每条命令上显式传入 `--profile`，而不是切换 `current_profile`，这样一次误操作的 `configure use` 不会悄悄改变后续命令的身份：

```bash
volclog --profile default doctor
volclog --profile default tool exec project.describe-projects
```

## 3. 配置档字段与模式更新语义

一个配置档可以携带认证模式字段（AK/SK、SSO 会话绑定、OIDC 令牌文件等）以及通用运行时字段：`region`、`endpoint` 和 `timeout_seconds`。

`disable-ssl` 字段不是通用的运行时开关。它仅适用于 RAM Role ARN 和 OIDC 的 STS 凭证交换请求：当为 `true` 时，这些认证材料会通过明文 HTTP 发送到固定的 STS 主机。它不会改变 TLS 业务端点的方案。其他模式（静态 AK/SK、SSO、Console Login、ECS Role）在凭证交换时不使用它。在不可信网络上请避免使用；详见 [认证](2-Authentication_zh.md)。

### 3.1 省略 `--mode`（传统静态路径）

当省略 `--mode` 时，`configure set` 走传统静态路径。它始终将配置档模式设为 `ak`，并在每次调用时**覆盖**标准静态配置档字段：`access_key_id`、`secret_access_key`、`security_token`、`region`、`endpoint`、`timeout_seconds`、`cred_ref` 和 `mode`。它要求提供 `--ak --sk`（或 `--cred-ref`）、`--region` 和 `--endpoint`。

由于省略的标志被视为空值，不带 `--token` 重新运行 `configure set` 会清除之前存储的 `security_token`。此路径的存在是为了精确保留原有的静态 AK/SK 行为。

### 3.2 提供 `--mode`（显式补丁路径）

当提供 `--mode` 时，`configure set` 加载现有配置档并**仅补丁显式提供的标志**。未提及的字段保持不变，因此之前模式留下的休眠字段（例如切换到动态模式后遗留的静态 AK/SK）在模式切换间会被保留。补丁完成后，合并后的配置档会根据所选模式的要求进行校验。

`configure set` 不接受 `--mode sso` 和 `--mode console-login`；这两种模式使用 [认证](2-Authentication_zh.md) 中描述的专用 `login` 和 `configure sso` 流程。

### 3.3 动态提供者与休眠字段

对于 Console Login 和 SSO，专用流程会补丁登录绑定，而任何休眠的静态字段可以保留在配置档中。失败关闭的动态提供者（SSO、Console Login、RAM Role ARN、OIDC、ECS Role）永远不会使用休眠的静态字段：当配置档模式为提供者模式时，环境 AK/SK 会被忽略，提供者由其自身的绑定构建。如果提供者无法获取凭证，请求会失败关闭，而不是回退到静态 AK/SK。

### 3.4 清理与存储指导

没有字段级别的擦除命令。要从配置档中移除休眠的静态字段，请删除并重新创建该配置档，或使用经批准的安全配置工具管理配置文件。在删除共享凭证引用之前（见第 4 节），请确认没有其他配置档引用它。

## 4. 共享凭证引用

配置档可以通过 `--cred-ref` 按名称引用共享凭证。凭证在配置文件的 `creds` 下存储一次，被每个引用它的配置档复用。`region` 和 `endpoint` 仍然是配置档专属的，不会从凭证继承。

没有独立的凭证创建命令。要创建或更新共享凭证，请在配置档上同时使用 `--cred-ref` 和完整的 `--ak`、`--sk`：

```bash
volclog configure set --profile app \
  --cred-ref shared-creds \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

然后第二个配置档可以仅通过引用 `--cred-ref` 来复用同一凭证，并提供自己的 `region` 和 `endpoint`：

```bash
volclog configure set --profile app-backup \
  --cred-ref shared-creds \
  --region cn-shanghai \
  --endpoint https://tls-cn-shanghai.volces.com
```

当 `--cred-ref` 与 `--ak --sk` 一起使用时，提供的 AK/SK 会写入命名的凭证条目（创建或更新），配置档只存储引用。

删除共享凭证：

```bash
volclog configure cred delete shared-creds
```

`configure cred delete` 首先检查是否有任何配置档引用该凭证。如果仍有配置档引用它，命令将中止并报错 `credential in use by profiles: <names>`，凭证不会被删除。请先重新分配或删除这些配置档，然后再删除凭证。

## 5. 运行时优先级

运行时值的解析方式取决于所选配置档使用的是静态 AK/SK 还是动态提供者。`context.region` 和 `context.endpoint` 仅可通过 `tool`/`workflow` 上下文使用；它们成为每次执行的回退默认值，优先于项目 `region`/`endpoint`，但不会覆盖非空的所选配置档值或动态环境值。

### 5.1 静态模式

在静态模式（`mode: ak`）下，当存在完整的环境 AK/SK 集合（`VOLCENGINE_ACCESS_KEY_ID` 和 `VOLCENGINE_ACCESS_KEY_SECRET`）时，静态解析会从环境值构建身份并**完全绕过所选配置档**。region/endpoint 优先级为：环境 > 上下文默认值 > 项目默认值。timeout 优先级为：项目默认值 > `60` 秒。如果环境和回退值都未提供所需的 region/endpoint，解析将失败。

没有完整的环境 AK/SK 集合时，使用所选配置档。region/endpoint 优先级为：配置档 > 上下文默认值 > 项目默认值。timeout 优先级为：配置档 > 项目默认值 > `60` 秒。

上下文没有 timeout 字段。

### 5.2 动态提供者模式

对于动态提供者模式（SSO、Console Login、RAM Role ARN、OIDC、ECS Role），环境 AK/SK 会被有意忽略。region/endpoint 优先级为：环境 region/endpoint > 配置档 > 上下文默认值 > 项目默认值。

timeout 优先级为：配置档 > 项目默认值 > `60` 秒。没有全局或上下文的 timeout 覆盖。

### 5.3 运行时选择器冲突

显式选择器是可选的。如果没有选择器，则应用正常的配置档选择（`--profile` → `current_profile` → `default`）或静态环境解析。

冲突规则如下：

- 任何配置档选择器（全局 `--profile` 或 `context.profile`）与任何密钥文件选择器（全局 `--secrets-file` 或 `context.secrets_file`）组合都会冲突。
- 全局 `--secrets-file` 与 `context.secrets_file` 组合会冲突。
- 全局 `--profile` 与 `context.profile` 仅在配置档名称不同时才冲突。在两处重复相同的配置档名称是可接受的，但属于冗余；为清晰起见应避免这样做。

冲突会失败并报错 `conflicting runtime selectors`（当提供两个不同的配置档名称时为 `conflicting profile selectors`）。

`--secrets-file` 对 `login`、`logout`、`sso` 和 `configure sso` 是被拒绝的，这些命令管理其自身的动态身份。

### 5.4 密钥文件解析与作用域

`--secrets-file` 读取 dotenv 风格的文件，并仅将受支持的 `VOLCENGINE_*` 赋值应用到进程环境：

- `VOLCENGINE_ACCESS_KEY_ID`
- `VOLCENGINE_ACCESS_KEY_SECRET`
- `VOLCENGINE_TOKEN`
- `VOLCENGINE_REGION`
- `VOLCENGINE_ENDPOINT`

其他键会被忽略。以 `#` 开头的行是注释，`export ` 前缀可被接受。文件必须至少包含一个受支持的赋值，否则加载失败。

密钥文件的值是进程作用域的。可以准备一个权限受限的密钥文件并传递给单条命令。下面的示例在 `$HOME/.volclog` 不存在时也能工作：`umask 077` 使任何新创建的常规文件为 `0600`，目录被显式设为 `0700`，显式的 `chmod 600` 确保重新运行该示例时也能修复已存在的、权限过宽的文件：

```bash
secrets_dir="$HOME/.volclog"
(
  umask 077
  mkdir -p "$secrets_dir"
  chmod 700 "$secrets_dir"
  cat > "$secrets_dir/secrets.env" <<'EOF'
VOLCENGINE_ACCESS_KEY_ID='<access-key-id>'
VOLCENGINE_ACCESS_KEY_SECRET='<secret-access-key>'
VOLCENGINE_REGION=cn-beijing
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
EOF
  chmod 600 "$secrets_dir/secrets.env"
)
volclog --secrets-file "$secrets_dir/secrets.env" doctor --online
```

同样的值也可以在不使用文件的情况下为单个进程内联提供：

```bash
VOLCENGINE_ACCESS_KEY_ID='<access-key-id>' \
VOLCENGINE_ACCESS_KEY_SECRET='<secret-access-key>' \
VOLCENGINE_REGION=cn-beijing \
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog tool exec project.describe-projects
```

## 6. 项目配置

项目配置（当前工作目录中的 `.volclog/cli.config.json`）提供非敏感的运行时默认值。项目配置仅在当前工作目录中应用。

当显式传入时，`--output`、`--output-mode` 和 `--trace-redact` 全局标志优先于其对应的项目默认值。其他全局标志不会覆盖项目默认值。

当前运行时实际消费的字段如下：

| 字段 | 用作 | 说明 |
| --- | --- | --- |
| `output` | `--output` 的默认值 | 未传入 `--output` 时使用 |
| `output_mode` | `--output-mode` 的默认值 | 未传入 `--output-mode` 时使用 |
| `trace_redact` | `--trace-redact` 的默认值 | 未传入 `--trace-redact` 时使用 |
| `region` | region 的回退默认值 | 配置档（和上下文默认值）没有 region 时使用 |
| `endpoint` | endpoint 的回退默认值 | 配置档（和上下文默认值）没有 endpoint 时使用 |
| `timeout_seconds` | timeout 的回退默认值 | 配置档没有 timeout 时使用 |

`region` 和 `endpoint` 可能存在于项目配置文件中并作为回退默认值被消费，但 `configure project set` 不暴露设置它们的标志。`output_dir` 和 `hints_file` 可被 `configure project set` 接受并存储在文件中，但当前运行时不会应用它们。它们可以被设置和查看，但对今天的命令执行没有影响。

```bash
volclog configure project set --output json --output-mode stdout --trace-redact on
volclog configure project show
```

`configure project set` 接受 `--output`、`--output-mode`、`--output-dir`、`--timeout-seconds`、`--trace-redact` 和 `--hints-file`。它不接受 `--region` 或 `--endpoint`；这些是用 `configure set` 管理的配置档字段。

## 7. 输出与追踪默认值

输出和追踪由全局标志控制。详细的执行语义属于 [使用](4-Usage_zh.md)；本节列出选择器及其默认值。

| 标志 | 值 | 默认值 |
| --- | --- | --- |
| `--output` | `json`、`jsonl`（默认 `volclog`）；`table`（仅 `volclog-human`） | `json` |
| `--output-mode` | `stdout`、`file` | `stdout` |
| `--output-dir` | 目录路径 | 无 |
| `--output-file` | 文件路径 | 无 |
| `--jmes-filter` | JMESPath 表达式 | 无 |
| `--trace-dir` | 目录路径 | 无（追踪关闭） |
| `--trace-redact` | `on`、`off`（及别名） | `on` |

在默认的 `volclog` agent 路径上，`--output table` 会被拒绝；请使用 `--output json` 或 `--output jsonl`。`table` 格式仅由 `volclog-human` 支持，且仅用于特定的快捷方式入口：`project`/`topic`/`metric-topic` 的 list 和 get、`index get`、`log search`。并非每个人类快捷方式都支持 table。

`--trace-redact` 接受 `on`、`true`、`1`、`yes`、`enabled`、`strict`、`default`（均映射为 `on`）以及 `off`、`false`、`0`、`no`、`disabled`（均映射为 `off`）。任何无法识别的值默认为 `on`。

当前追踪写入始终将结构化的头和查询字段保留为键，将体保留为哈希。`--trace-redact off` 会被接受和规范化，但不会禁用该强制的结构化字段脱敏，也不会输出原始的头、查询或体值。传输 `error_message` 字段仍然是单独的敏感例外：它直接存储传输错误字符串，其中可能包含 URL 和查询值。

这些全局标志可以放在命令组之前，或者对于 `raw`、`tool`、`workflow`、`project`、`topic`、`metric-topic`、`index`、`log`、`host-group` 和 `collector`，作为尾部全局标志放在组之后。`--profile` 和 `--secrets-file` 只能作为前导标志，不能用作尾部全局标志。

`login`、`logout`、`sso` 和 `configure sso` 始终将其精确的 JSON 结果形状写入 stdout。`--output jsonl|table` 可能会被解析，但不会改变冻结的 JSON 形状（JSON 被强制）。以下选项在任何认证副作用运行之前被拒绝：非 `stdout` 的 `--output-mode`、`--output-file`、`--jmes-filter`、`--trace-dir` 和 `--secrets-file`。单独的 `--output-dir` 和不带 `--trace-dir` 的 `--trace-redact` 不会转移冻结的结果，因此不会被拒绝。

## 8. 安全检查、清理与示例

### 8.1 不泄露密钥地检查

`configure list`、`configure show` 和 `doctor` 永远不会打印密钥。用它们来验证配置和凭证：

```bash
volclog configure list
volclog configure show --profile default
volclog --profile default doctor
volclog --profile default doctor --online
```

### 8.2 清理边界

要删除配置档或共享凭证，请使用专用的删除命令。没有广泛的、破坏性的清理命令。删除配置档只删除该配置档；被其他配置档引用的共享凭证不受影响。当任何配置档引用共享凭证时，删除该凭证会被阻止。

对于休眠字段清理，边界与 [认证](2-Authentication_zh.md) 中的相同：删除并重新创建配置档，或使用经批准的安全配置工具管理配置文件。不要在 CLI 运行时手动编辑配置文件。

---

[← 上一篇：认证](2-Authentication_zh.md) | [English](3-Configuration.md) | [下一篇：使用 →](4-Usage_zh.md)
