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
  set     Set a profile
  project Manage project defaults (show/set)
  profile Alias commands: add/use/show/list/delete
  cred    Manage shared credentials (delete)
  use     Set default profile
  show    Show a profile
  list    List profiles
  delete  Delete a profile (or batch delete by prefix)

Examples:
  tlsctl configure set --profile default --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile tenant-a-sg --ak <ak> --sk <sk> --endpoint https://tls-ap-singapore-1.volces.com
  tlsctl configure set --profile abc-bj --cred-ref ma-abc-root --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure set --profile abc-sg --cred-ref ma-abc-root --endpoint https://tls-ap-singapore-1.volces.com
  tlsctl configure profile add tenant-a --ak <ak> --sk <sk> --endpoint https://tls-cn-beijing.volces.com
  tlsctl configure profile use tenant-a
  tlsctl configure use default
  tlsctl configure show --profile default
  tlsctl configure project show
  tlsctl configure project set --output json --output-mode file --output-dir ./out
  tlsctl --profile tenant-a-sg project list
  tlsctl configure list
  tlsctl configure delete tenant-a-sg
  tlsctl configure delete --prefix tenant-a --yes
  tlsctl configure cred delete ma-abc-root

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
  list      List bundled skills available in this volclog build
  install   Install bundled skills into a user-provided agent skills directory

Notes:
  - --dir is required for install and should point to the target agent's skills directory
  - If --name is omitted, install copies all bundled skills
  - Use --force to overwrite an existing installed skill directory
  - This command installs from the CLI's bundled skills; it does not require the source repo checkout

Examples:
  tlsctl skill list
  tlsctl skill install --dir /path/to/agent/skills
  tlsctl skill install --dir /path/to/agent/skills --name volclog-core
  tlsctl skill install --dir /path/to/agent/skills --force

Exit Code:
  0 success
  1 usage / invalid args
 2 runtime failure
`)
}

func usageAPI() string {
	return u(`Usage:
  tlsctl api <legacy surface removed>

Notes:
  - This legacy surface is no longer routed from the main CLI entry.
  - Use tlsctl tool ... / tlsctl raw ... instead.
`)
}

func usageAPICall() string {
	return u(`Usage:
  tlsctl api call <legacy surface removed>

Notes:
  - This legacy surface is no longer routed from the main CLI entry.
  - Use tlsctl raw --method <METHOD> --path <PATH> instead.
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
