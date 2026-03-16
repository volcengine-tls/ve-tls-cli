# tlsctl-runner

面向公众用户的本地可执行 skill：将“account+region+意图”转换为可审计的 `tlsctl` 调用，并返回结构化 JSON。

特性：
- 支持多账号（account）+ 多 region：自动从本地 profiles 中选择匹配的 profile
- 支持两种输入：stdin JSON（给智能体）与 `--text` 半结构化中文+参数键值（给人类）
- 不要求用户预装 `tlsctl`：发行版 runner 默认内置 `tlsctl`（单文件）；未内置时才会从 GitHub Release 下载并缓存（可配置镜像）
- 危险操作默认走 plan/apply：先 dry_run 返回计划与 confirm_token，再确认执行

## 前置配置（用户自行填写 AK/SK）

推荐为同一 account 的不同 region 建多个 profile：`<account>-<region>`（AK/SK 可以相同）。

```bash
tlsctl configure set --profile acctA-cn --ak <akA> --sk <skA> --region cn-beijing
tlsctl configure set --profile acctA-sg --ak <akA> --sk <skA> --region ap-singapore-1
tlsctl configure set --profile acctB-cn --ak <akB> --sk <skB> --region cn-beijing
```

## 安装（runner）

方式 A：从 GitHub Release 下载（推荐）
- Releases → Assets 下载 `tlsctl-runner_<os>_<arch>.tar.gz`（Windows 为 zip）

方式 B：Go 安装（需要本机 Go）
```bash
go install ./cmd/tlsctl-runner
```

## 输入模式

### 1) 智能体模式（stdin JSON）

示例：
```bash
echo '{
  "account":"acctA",
  "region":"cn-beijing",
  "action":"log.search",
  "args":{"topic_id":"xxx","query":"error","from_ms":"1710374400000","to_ms":"1710378000000"},
  "dry_run":true
}' | tlsctl-runner
```

### 2) 人类模式（--text 半结构化）

示例：
```bash
tlsctl-runner --text '帮我查日志 account=acctA region=cn-beijing action=log.search topic_id=xxx query="error" from_ms=1710374400000 to_ms=1710378000000'
```

说明：
- runner 只解析其中的 `key=value`，中文描述会忽略
- 对 create/modify/delete/log.export 等危险操作，如果不显式指定 `dry_run`，默认会先 `dry_run=true`

## 可配置环境变量

### runner 自身
- `TLSCTL_ACCOUNT_MAP`：可选映射文件，用于固定 account+region → profile
- `TLSCTL_RUNNER_SECRET`：用于生成/校验 confirm_token（不设置则不会生成 token，危险操作只能 dry_run）

### tlsctl 获取与下载
- `TLSCTL_BIN`：显式指定 tlsctl 路径
- `TLSCTL_BASE_URL`：tlsctl release 下载地址（默认 latest/download，可改成指定 tag 或企业镜像）
- `TLSCTL_CACHE_DIR`：tlsctl 下载缓存目录

## 输出

stdout 始终输出 JSON，包含：
- `plan`：dry_run 时的命令计划、confirm_required/confirm_token
- `data`：执行成功时的结构化结果（json/jsonl 自动解析）
- `error`：失败时的错误码与提示（如 profile_not_found / profile_ambiguous）
- `audit`：用于审计的字段（account/region/action/profile/commands/duration）

更多细节见规格：
- [spec.md](./spec.md)
