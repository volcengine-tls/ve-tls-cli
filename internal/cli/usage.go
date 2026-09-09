package cli

import "strings"

func u(s string) string {
	s = strings.ReplaceAll(s, ".tlsctl", ".volclog")
	s = strings.ReplaceAll(s, "TLSCTL_", "VOLCLOG_")
	s = strings.ReplaceAll(s, "tlsctl", "volclog")
	return s
}

func usageConfigure() string {
	return u(`Usage:
  tlsctl configure <command> [args]

Commands:
  set          Set a profile
  sso-session  Configure an enterprise SSO entry
  sso          Bind a profile and complete the first SSO login
  project      Manage project defaults (show/set)
  profile      Alias commands: add/use/show/list/delete
  cred         Manage shared credentials (delete)
  use          Set default profile
  show         Show a profile
  list         List profiles
  delete       Delete a profile (or batch delete by prefix)

Authentication:
  - Static AK/SK, RAM Role ARN, OIDC, or ECS Role: tlsctl configure set --help
  - Console Login: tlsctl login --help
  - First-time SSO: tlsctl configure sso-session --help, then tlsctl configure sso --help
  - SSO re-login or logout: tlsctl sso --help

Configure Set Flags:
  --mode <ak|ramrolearn|oidc|ecsrole>  Auth mode (omit for legacy static AK/SK)
  --account-id <id>                    Account ID for ramrolearn
  --role-name <name>                   Role name for ramrolearn/ecsrole
  --oidc-token-file <path>             OIDC token file path for oidc
  --role-trn <trn>                     Role TRN for oidc
  --disable-ssl[=true|false]           Switch RAM/OIDC STS scheme to HTTP (tri-state)
  --ak <ak> --sk <sk>                  Static access key / secret key
  --token <token>                      Source session/security token for ramrolearn
  --cred-ref <name>                    Reference a shared credential
  --region <region> --endpoint <url>   Target region and endpoint
  --timeout-seconds <seconds>          Per-request TLS business timeout

  For an existing profile, a command containing only --profile plus region,
  endpoint, and/or timeout-seconds patches those runtime fields without changing
  its auth mode or identity. Other commands without --mode preserve the legacy
  static AK/SK behavior. When
  --mode is supplied only ak/ramrolearn/oidc/ecsrole are accepted; sso and
  console-login must use their dedicated flows (configure sso / login). Explicit
  --mode merges only the supplied flags into the existing profile and validates
  identity fields: ak needs AK/SK or cred-ref; ramrolearn needs source AK/SK or
  cred-ref plus account-id+role-name; oidc needs oidc-token-file+role-trn;
  ecsrole needs role-name. TLS region/endpoint may be supplied later.

  WARNING: --disable-ssl only switches the RAM/OIDC STS assumption scheme to
  HTTP. The endpoint URL scheme remains authoritative: an https:// endpoint still
  uses TLS regardless of this flag. With --disable-ssl=true, RAM/OIDC
  authentication materials are sent over plaintext HTTP to the fixed STS host
  sts.volcengineapi.com (the STS host is not configurable). Do not enable this on
  untrusted networks.

Examples:
  tlsctl configure set --profile default --ak <ak> --sk <sk> --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile tenant-a-sg --ak <ak> --sk <sk> --region ap-southeast-1 --endpoint https://tls-ap-singapore-1.volces.com
  tlsctl configure set --profile abc-bj --cred-ref ma-abc-root --ak <ak> --sk <sk> --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile abc-sg --cred-ref ma-abc-root --region ap-southeast-1 --endpoint https://tls-ap-singapore-1.volces.com
  tlsctl configure set --profile abc-sg --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile ram-1 --mode ramrolearn --ak <ak> --sk <sk> --account-id 2100000000 --role-name TLSAdminRole --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile oidc-1 --mode oidc --oidc-token-file /var/run/secrets/token --role-trn trn:iam::2100000000:role/TLSAdminRole --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile ecs-1 --mode ecsrole --role-name TLSAdminRole --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile default --mode ak --disable-ssl=false --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure profile add tenant-a --ak <ak> --sk <sk> --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure profile use tenant-a
  tlsctl configure use default
  tlsctl configure show --profile default
  tlsctl configure project show
  tlsctl configure project set --output json --output-mode file --output-dir ./out
  tlsctl --profile tenant-a-sg tool exec project.describe-projects --input '{"PageSize":20}'
  tlsctl configure list
  tlsctl configure delete tenant-a-sg
  tlsctl configure delete --prefix tenant-a --yes
  tlsctl configure cred delete ma-abc-root

Next:
  tlsctl --profile <name> doctor
  tlsctl tool list
  tlsctl tool describe project.describe-projects
  tlsctl --profile <name> tool exec project.describe-projects --input '{"PageSize":20}'

Exit Code:
  0 success
  1 usage / invalid args

Agent:
  - Use tlsctl doctor to verify config before running requests
  - Prefer env or --secrets-file in CI to avoid writing secrets to disk
`)
}

func usageSkill() string {
	return u(`Usage:
  tlsctl skill <command> [args]

Commands:
  list       List bundled skills available in this volclog build
  install    Install bundled skills into a user-provided agent skills directory
  status     Compare installed skills with bundled version and digests
  update     Update unmodified managed skills; protect local changes by default
  uninstall  Remove unmodified managed skills; protect local changes by default

Notes:
  - --dir is required for install/status/update/uninstall and points to the target agent's skills directory
  - If --name is omitted, the command applies to all bundled skills
  - update/uninstall skip modified, untracked, or invalid-manifest skills unless --force is explicit
  - install --force preserves its explicit overwrite behavior
  - This command installs from the CLI's bundled skills; it does not require the source repo checkout

Examples:
  tlsctl skill list
  tlsctl skill install --dir /path/to/agent/skills
  tlsctl skill install --dir /path/to/agent/skills --name volclog-core
  tlsctl skill install --dir /path/to/agent/skills --force
  tlsctl skill status --dir /path/to/agent/skills
  tlsctl skill update --dir /path/to/agent/skills
  tlsctl skill uninstall --dir /path/to/agent/skills --name volclog-core

Exit Code:
  0 success
  1 usage / invalid args
 2 runtime failure
`)
}

func usageRaw() string {
	return u(`Usage:
  tlsctl raw --method <GET|POST|PUT|DELETE> --path <path> [--query k=v] [--header k=v] [--body <json|file://...|->|--input <json|file://...|->] [--request-format <json|jsonl>]

概览:
  原始 transport 调用入口；需要显式提供 method/path。
  --jmes-filter 作用于完整 CLI envelope。

关键参数:
  --method <GET|POST|PUT|DELETE>
  --path <path>
  --query k=v
  --header k=v
  --body <json|file://...|-> (agent alias: --input)
  --request-format <json|jsonl>
  --output-mode <stdout|file>
  --output-dir <path>

调用方式:
  - path 必须是以 / 开头的 OpenAPI 路径
  - body 支持 inline JSON、file://...、-、裸文件路径
  - 为了兼容 tool/workflow 的迁移心智，raw 也接受 --input 作为 --body 的别名；不要同时传 --body 和 --input
  - raw 的 --input/--body 只是 literal request body；即使是 GET，也不会像 tool exec 那样把 JSON 自动映射到 query/path/header
  - raw --dry-run 只做 transport/local checks；它不会像 tool/workflow 那样校验 API 必填字段
  - 大结果优先使用 --output-mode file --output-dir <writable-dir>
  - --jmes-filter 命中 null 仍输出 null；缺字段或数组越界会报 filter matched no value

Examples:
  tlsctl raw --method GET --path /DescribeProjects
  tlsctl raw --method POST --path /CreateProject --body file://./req.json
  tlsctl raw --method POST --path /CreateProject --input file://./req.json
  tlsctl raw --method GET --path /DescribeProjects --jmes-filter "data.Total"
`)
}

func usageTool() string {
	return u(`Usage:
  tlsctl tool <command> [args]

概览:
  用统一 tool 契约面做发现、筛选与契约查看；执行能力请使用 tool exec。
  仅公开官网文档已发布接口；未公开接口不属于对外 CLI 契约面。

Commands:
  list      List groups or actions
  describe  Show a tool contract and execution hint
  exec      Execute a tool contract with JSON context/input

Use:
  volclog tool list
  volclog tool list <group> [--verb <verb>] [--format <text|json>]
  volclog tool describe <group.action>
  volclog tool exec <group.action> --context file://ctx.json [--input file://req.json]

说明:
  list 默认返回 group 摘要；指定 <group> 后返回 action 列表。

Exit Code:
  0 success
  1 usage / invalid args

Agent:
  - describe 结果是机器可读契约，包含输入/上下文/执行约束
  - shortcut 仍是人工入口，不属于 tool 默认流程
`)
}

func usageWorkflow() string {
	return u(`Usage:
  tlsctl workflow <command> [args]

概览:
  CLI workflow 契约面，暴露少量高价值高层编排。
  这些能力不是官网公开 OpenAPI tool；tool 仍只暴露官网公开 API。

Commands:
  list      List workflow groups or workflow ids
  describe  Show a workflow contract and execution hint
  exec      Execute a workflow with JSON context/input
`)
}

func usageWorkflowList() string {
	return u(`Usage:
  tlsctl workflow list
  tlsctl workflow list [<group>] [--format <text|json>]

说明:
  - workflow 面只暴露 CLI workflow，不混入 public tool
  - 当前首批 workflow: log.ingest / log.export / log.export-analysis
  - tool 仍只暴露官网公开 API

Next:
  volclog workflow describe <group.command>
  volclog workflow exec <group.command> --input file://req.json

Filters:
  --format <text|json>
`)
}

func usageUpgrade() string {
	return u(`Usage:
  tlsctl upgrade [--check]
  tlsctl upgrade [--version <semver>] [--yes]

Behavior:
  - no flags / --check: check the latest version without changing files
  - --version <semver>: select a release; without --yes this is a no-write check
  - --yes: apply the selected or latest release
  - npm installations delegate updates to npm
  - standalone binaries require a release checksum and replace the executable atomically
  - volclog never performs background update checks
`)
}

func usageWorkflowDescribe() string {
	return u(`Usage:
  tlsctl workflow describe <group.command>

说明:
  - describe 返回 CLI workflow contract，而不是 public OpenAPI contract
  - 需要原子 API 契约时，回到 volclog tool describe <group.action>
  - workflow exec 使用 JSON input/context，不要求 agent 学 flags
`)
}

func usageWorkflowExec() string {
	return u(`Usage:
  tlsctl workflow exec <group.command> [--context file://ctx.json|-|'<inline-json>'] --input file://req.json|-|'<inline-json>'

Notes:
  - --context 可省略；省略时默认使用空对象 {}
  - --input 支持 file://...、-、inline JSON object
  - 业务请求字段放在 --input；运行时/鉴权/trace/output 控制放在 --context
  - input/context 校验失败时优先回到 workflow describe；不要先去 tool describe 找 workflow 契约
  - execution.* 一律放在 context.execution；不要传独立 execution 文件
  - execution.projection / execution.artifact / execution.dry_run 语义与 tool exec 一致
  - 大结果优先使用 --output-mode file --output-dir <writable-dir>
  - 未显式指定 stdout/file 且 stdout 结果过大时，workflow exec 也可能自动改走 file_auto；若没有可写 output_dir，会直接提示补 --output-dir <writable-dir>
  - --jmes-filter 命中 null 仍输出 null；缺字段或数组越界会报 filter matched no value
`)
}

func usageToolList() string {
	return u(`Usage:
  tlsctl tool list
  tlsctl tool list [<group>] [--verb <verb>] [--format <text|json>]

说明:
  - 默认返回 group 摘要
  - 指定 <group> 后返回该 group 下可执行的 action identity
  - 仅列出官网文档已发布接口
  - 只做发现与筛选，不执行请求

支持的发现方式:
  - 按 group 看有哪些 action: tlsctl tool list <group>
  - 按 verb 缩小范围: tlsctl tool list <group> --verb <verb>

常见 verb:
  create / get / list / describe / modify / delete / search

Next:
  tlsctl tool describe <group.action>
  tlsctl tool exec <group.action> [--context file://ctx.json] [--input file://req.json|-]

Filters:
  --verb <verb>
  --format <text|json>
`)
}

func usageToolDescribe() string {
	return u(`Usage:
  tlsctl tool describe <group.action> [--view <compact|full>]

说明:
  - 默认返回 compact 视图，优先保留最小可执行契约
  - 指定 --view full 或显式 --output json 时返回完整机器契约
  - 只展示官网文档已发布接口的契约
  - 只做契约查看，不执行请求
  - 通常与 volclog tool list <group> 配合使用
`)
}

func usageToolExec() string {
	return u(`Usage:
  tlsctl tool exec <group.action> [--context file://ctx.json|-|'<inline-json>'] [--input file://req.json|-|'<inline-json>'] [--page-all]

Notes:
  - 先根据 tool describe 准备 context/input 文件
  - --context 可省略；省略时默认使用空对象 {}
  - --context 和 --input 都支持 file://...、-、inline JSON object
  - 当 tool describe 的 input_schema 为空时，可省略 --input
  - 业务请求字段放在 --input；运行时/鉴权/trace/output 控制放在 --context
  - tool exec 既支持显式嵌套的 {query,path,header,body}，也支持扁平 JSON；当字段能唯一映射到某个 section 时会自动归位
  - --page-all 是 execution.page.all 的 CLI 入口（等价于 execution.page.all=true）
  - execution.* 一律放在 context.execution；不要传独立 execution 文件
  - 大结果优先使用 --output-mode file --output-dir <writable-dir> 或 execution.artifact
  - 未显式指定 stdout/file 且 stdout 结果过大时，tool exec 会自动改走 file_auto；若没有可写 output_dir，会直接提示补 --output-dir <writable-dir>
  - --jmes-filter 命中 null 仍输出 null；缺字段或数组越界会报 filter matched no value
  - execution.projection 支持 "expr"、["expr"]、{"jmes":"expr"}
  - execution.artifact 支持 true、"/tmp/out.json"、{"path":"/tmp/out.json"}
  - execution.page.all 只在 tool describe 返回 execution.supports_all=true 时可用；它提高完整性，可能增加 payload 大小
  - context.trace 支持 true、"/tmp/traces"、{"dir":"/tmp/traces","redact":"on"}；当前不区分 strict/default 模式
`)
}

func usageDoctor() string {
	return u(`Usage:
  tlsctl doctor [--online]

Flags:
  --online   Run minimal online checks (optional)

Examples:
  tlsctl doctor
  tlsctl doctor --online

Output:
  - time.local_unix_ms, time.server_unix_ms, time.skew_seconds, time.skew_risk

Exit Code:
  0 success
  2 missing required config/credentials

Agent:
  - Use doctor output to decide whether to proceed or reconfigure
`)
}

func usageLogin() string {
	return u(`Usage:
  tlsctl login [-p|--profile NAME] [-r|--region REGION] [--endpoint URL] [--login-endpoint URL] [--device-code] [--no-browser] [--remote]

概览:
  通过 Console Login 获取临时 STS 凭证。
  默认使用本机浏览器回调 + PKCE；--device-code 显式使用跨设备 Device Code。
  --no-browser 与历史兼容别名 --remote 均隐含 --device-code。
  登录态只写入 ~/.volclog（或 VOLCLOG_CONFIG 对应目录）的 login/cache，不依赖 ve 或 ~/.volcengine。

Flags:
  -p, --profile <name>       目标 profile（与全局 --profile 冲突时报错）
  -r, --region <region>      保存到 profile 的 TLS region
  --endpoint <url>           保存到 profile 的 TLS 业务 endpoint
  --login-endpoint <url>     Console OAuth 根地址（默认 https://signin.volcengine.com）
  --device-code              使用跨设备 Device Code，而非默认本机浏览器回调
  --no-browser               使用 Device Code 且不自动打开浏览器
  --remote                   --no-browser 的历史兼容别名

Examples:
  # 本机桌面：浏览器回调 + PKCE，不输入 user code
  tlsctl login --profile NAME --region REGION --endpoint URL

  # Device Code：不监听本地回调端口，仍尝试自动打开浏览器
  tlsctl login --device-code --profile NAME --region REGION --endpoint URL

  # SSH / Trae / 人工参与的 Agent：复制终端打印的 URL，在任意浏览器的官方页面输入 user code
  tlsctl login --device-code --no-browser --profile NAME --region REGION --endpoint URL

输出:
  - stdout 仅输出最终 JSON（profile/provider/region/endpoint/expires_at/masked_access_key）
  - 授权 URL、短期 user code、prompt、浏览器提示、进度只写 stderr
  - 保持原终端运行并等待轮询完成；不需要把任何 code 粘贴回终端

安全:
  - Console 密码只在官方浏览器页面输入；CLI 不读取密码
  - raw AK/SK、SecurityToken、OAuth token、authorization code、PKCE verifier 和内部 device_code 不写 stdout/stderr/trace
  - user code 只用于用户在授权页输入，短期有效且不落盘；不要复制到聊天、工单或持久化日志
  - 动态令牌只写入 0600 私有缓存；失败不切换流程，也不回退到环境 AK/SK
  - Console Login 只使用当前登录账号已有权限，不提升权限；账号应遵循最小权限原则
  - Console Login 需要人工确认，不适合无人值守 CI；CI 优先使用 OIDC 或 ECS Role

Next:
  tlsctl --profile <name> doctor
  tlsctl --profile <name> tool exec project.describe-projects --input '{"PageSize":20}'

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure

注意:
  - --login-endpoint 只影响登录授权；--endpoint 始终表示 TLS 业务地址
  - 登录地址必须是干净的 HTTPS 根地址；登录成功后会随缓存保存并用于后续刷新
  - 新 profile 应同时提供 region 和 endpoint；已有 profile 可省略以保留原值
  - 不接受 --secrets-file；不要把长期静态凭证注入交互登录进程
  - 任一授权流程失败都不会自动切换到另一流程，也不会回退到环境 AK/SK
`)
}

func usageLogout() string {
	return u(`Usage:
  tlsctl logout [-p|--profile NAME] [--all]

概览:
  清除 Console Login 登录态：删除 login cache 并清除 profile 中的 login-session 绑定。
  不删除 profile，保留 TLS 配置和休眠的静态 AK/SK 字段。

Flags:
  -p, --profile <name>       清除指定 profile 的登录态
  --all                      清除所有 mode=console-login 的 profile（不扫描 cache 目录）

行为:
  - 按 login-session 分组并稳定排序；每个 session 获取与 Provider.Retrieve 相同的 cache lock
  - 在锁内先删除 cache，再用 config.Update 清除所有匹配 profile 的 login-session
  - 锁序固定为 console cache -> config path；logout 返回前不释放 cache lock
  - config patch 失败时返回可分类的 partial-failure，cache 保持已删除（fail closed）
  - 删除不存在 cache 是幂等成功

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure

注意:
  - 不接受 --secrets-file
  - --all 不影响 AK/SK、default-chain 或其他 mode 的 profile
`)
}

func usageSSO() string {
	return u(`Usage:
  tlsctl sso <command> [args]

Commands:
  login   Re-authorize an SSO session (by --profile or --sso-session)
  logout  Clear SSO token and STS caches for a session (by --profile or --sso-session)

Examples:
  tlsctl sso login --profile prod
  tlsctl sso login --sso-session corp --no-browser
  tlsctl sso logout --profile prod
  tlsctl sso logout --sso-session corp

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure

注意:
  - 不接受 --secrets-file；不要把长期静态凭证注入交互登录进程
  - 登录失败必须失败关闭，不会回退到环境 AK/SK
`)
}

func usageSSOLogin() string {
	return u(`Usage:
  tlsctl sso login [--profile NAME | --sso-session NAME] [--no-browser]

概览:
  通过 SSO Device Authorization 重新授权并获取 OAuth token。
  --profile 从 profile 解析 session/account/role；--sso-session 直接登录 session。
  两者互斥；登录态只写入 ~/.volclog（或 VOLCLOG_CONFIG 对应目录）的 sso/cache。

Flags:
  --profile <name>           从 profile 解析 SSO session（与 --sso-session 互斥）
  --sso-session <name>       直接登录指定 SSO session（与 --profile 互斥）
  --no-browser               不自动打开浏览器，仅打印授权 URL

输出:
  - stdout 仅输出最终 JSON（profile/provider/sso_session/region/endpoint/sso_region/expires_at）
  - region/endpoint 是 TLS 运行值；sso_region 是 CloudIdentity 鉴权区域
  - 直接按 --sso-session 登录时没有目标 profile，TLS region/endpoint 为空
  - 授权 URL、prompt、浏览器提示、进度只写 stderr
  - 此阶段没有真实 STS AK，绝不输出 masked_access_key 或任何 OAuth token 片段

Next:
  tlsctl configure sso --help
  tlsctl --profile <name> doctor
  tlsctl --profile <name> tool exec project.describe-projects --input '{"PageSize":20}'

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure

注意:
  - --profile 与 --sso-session 同时出现必须在任何副作用前拒绝
  - 不接受 --secrets-file
  - 只有显式 CLI 命令可运行 DeviceFlow；普通业务命令不得触发浏览器
`)
}

func usageSSOLogout() string {
	return u(`Usage:
  tlsctl sso logout [--profile NAME | --sso-session NAME]

概览:
  清除 SSO 登录态：删除 token cache 和所有关联 STS cache，并清除绑定该 session 的
  profile 的 sts-expiration。不删除 profile，保留 SSO session 配置、TLS 配置和休眠
  静态字段。同时支持 --profile 与 --sso-session（有意修复上游帮助/解析偏差）。

Flags:
  --profile <name>           清除指定 profile 绑定的 SSO session
  --sso-session <name>       清除指定 SSO session（影响所有绑定该 session 的 profile）

行为:
  - 从配置解析该 session 关联的全部 profile 和 STS key，去重并按摘要排序
  - 获取 session token lock，再按摘要排序依次获取全部 STS locks（锁序与 Provider 相同）
  - 持有 token lock 时尽力 revoke RefreshToken；无论远端是否成功都清本地 cache
  - 在锁内通过 config.Update 清除所有绑定该 session 的 profile 的 sts-expiration
  - 逆序释放 STS locks，最后释放 token lock；logout 返回后并发 Provider 不得重建 cache
  - 远端 revoke 失败但本地清理成功：返回明确可分类的 partial failure

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure

注意:
  - 不接受 --secrets-file
  - 错误文本不得含 token/secret
`)
}

func usageConfigureSSOSession() string {
	return u(`Usage:
  tlsctl configure sso-session --name NAME --start-url URL --region REGION [--registration-scopes scope1,scope2]

概览:
  保存或更新企业 SSO 入口配置（name/start-url/region/scopes）。
  sso-session.region 是 CloudIdentity region，与 profile.region（TLS SignV4 region）
  绝不互相覆盖。

Flags:
  --name <name>              SSO session 名称（必填）
  --start-url <url>          SSO 用户入口 URL（必填，必须是干净的 HTTPS URL）
  --region <region>          CloudIdentity region（必填）
  --registration-scopes <s>  逗号分隔的 scope 列表（可选，默认 cloudidentity:account:access,offline_access）

行为:
  - 默认 scopes 使用冻结的允许值；拒绝未知 scope；空元素视为 malformed 列表并拒绝
  - --name/--start-url/--region 每次必填；仅 --registration-scopes 省略时保留旧值

Next:
  tlsctl configure sso --help

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure
`)
}

func usageConfigureSSO() string {
	return u(`Usage:
  tlsctl configure sso --profile NAME --sso-session SESSION [--account-id ID] [--role-name NAME] [--region REGION] [--endpoint URL] [--no-browser]

概览:
  绑定 profile 到 SSO session 并完成首次 Device Authorization 登录。
  profile 可以不存在；首次创建时应同时提供 TLS region 和 endpoint。
  已有 profile 可省略对应参数以保留原值。
  不改变 CurrentProfile。

Flags:
  --profile <name>           目标 profile（与全局 --profile 冲突时报错）
  --sso-session <name>       已配置的 SSO session 名称（必填）
  --account-id <id>          显式指定账号（可选，省略时交互选择）
  --role-name <name>         显式指定角色（可选，省略时交互选择）
  --region <region>          TLS SignV4 region（不是 SSO session region）
  --endpoint <url>           TLS 业务 endpoint
  --no-browser               不自动打开浏览器，仅打印授权 URL

Examples:
  tlsctl configure sso --profile sso-dev --sso-session corp --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure sso --profile sso-dev --sso-session corp --account-id ACCOUNT_ID --role-name ROLE_NAME --region cn-beijing --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure sso --profile sso-dev --sso-session corp --region cn-beijing --endpoint https://tls-cn-beijing.volces.com --no-browser

行为:
  - 把 profile 切到 mode=sso；只更新显式提供的 TLS Region/Endpoint
  - SSO session region 仅用于 CloudIdentity，绝不写入 TLS region
  - 保留休眠静态字段 AccessKeyID/SecretAccessKey/SecurityToken/CredRef
  - 清除且仅清除 Console Login 的 LoginSession
  - 写入 SSOSessionName/AccountID/RoleName
  - 整个事务在 token lock 内完成：快照旧 token -> Login -> binding -> config commit
  - 任意步骤失败在同一锁内精确恢复旧快照

Next:
  tlsctl --profile <name> doctor
  tlsctl --profile <name> tool exec project.describe-projects --input '{"PageSize":20}'

Exit Code:
  0 success
  1 usage / invalid args
  2 runtime failure

注意:
  - 不接受 --secrets-file
  - 登录失败必须失败关闭，不会回退到环境 AK/SK
`)
}
