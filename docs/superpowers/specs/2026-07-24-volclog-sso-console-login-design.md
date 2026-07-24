# volclog 独立支持 SSO 与 Console Login 设计

日期：2026-07-24

状态：已完成交互式设计确认，待用户复核本文档

## 1. 背景与结论

`volclog` 当前支持长期 AK/SK 和手工注入的 STS 临时凭证，但不会自行完成登录、换取或刷新临时凭证。`volcengine-cli` 已支持 `sso`、`console-login`、`ramrolearn`、`oidc`、`ecsrole` 等模式；这些模式最终都向业务请求提供 AK/SK/SessionToken，并继续使用标准 SignV4。

Phase 1 只为 `volclog` 增加 SSO 和 Console Login，并遵循以下结论：

1. `volclog` 必须独立运行，不依赖 `ve` 命令、`~/.volcengine/config.json` 或 `~/.volcengine` 下的登录缓存。
2. 用户命令、认证模式名称和新增 profile 字段尽量对齐 `volcengine-cli`，但所有状态保存在 `~/.volclog`。
3. 现有 AK/SK、cred-ref、环境变量、secrets-file 和手工 STS 行为必须保持不变。
4. 认证核心与 TLS 业务层隔离，为未来把认证能力并入 `volcengine-cli` 保留清晰边界。
5. Phase 1 不修改 `log-service`。SSO 和 Console Login 最终仍以 AK/SK/SessionToken 对 TLS HTTP API 做 `Service=TLS` 的 SignV4。

## 2. 目标与非目标

### 2.1 目标

- `volclog` 自带 SSO Device Authorization、账号/角色选择、登录、重新登录和登出能力。
- `volclog` 自带 Console OAuth Authorization Code + PKCE、远程登录、刷新和登出能力。
- 每次 TLS 请求签名前获取当前有效凭证，支持过期前刷新。
- 命令和 profile 认证语义与 `volcengine-cli` 尽量一致。
- 登录状态只写入 `~/.volclog`，目录权限、文件权限和日志脱敏满足安全要求。
- 为现有 AK/SK 路径建立行为锁定测试，证明新功能未改变旧行为。

### 2.2 非目标

Phase 1 不包含：

- OIDC、ECS Role、AssumeRole/RamRoleArn；
- SDK 默认凭证链迁移；
- Kafka STS；
- TLS 服务端直接接受 OAuth/OIDC Bearer Token；
- 私有化或 inner 环境认证适配；
- 将代码直接提交或合并到 `volcengine-cli`；
- 抽取和发布新的跨仓 Go module。

## 3. 方案选择

### 3.1 采用：模块化移植

从 `volcengine-cli` 中识别并移植 SSO、Console Login、OAuth/PKCE、缓存和刷新逻辑，在 `ve-tls-cli` 内形成独立认证模块。命令适配层仍使用 `volclog` 的输出和错误契约，认证核心不依赖 TLS tool/workflow。

选择原因：

- 满足完全独立运行；
- 能复用已经验证过的协议和边界；
- 避免复制整个 `cmd` 包；
- 未来可将认证核心迁回或并入 `volcengine-cli`；
- 不需要在 Phase 1 协调两个仓库的版本和发布。

### 3.2 不采用：直接复制命令代码

直接复制 `volcengine-cli/cmd` 中的命令可以较快形成表面功能，但会把 CLI Context、配置写回、输出格式和网络逻辑一起带入，导致两边快速分叉，不利于测试和未来合并。

### 3.3 暂不采用：先创建共享认证库

独立共享库是长期可选方向，但现在就建设会扩大为跨仓 API、版本和发布治理问题。Phase 1 先在仓内形成可提取的边界，待能力稳定后再评估。

## 4. 总体架构

```text
volclog login / sso login / configure
                 |
                 v
internal/auth
|- profile       认证字段、模式校验和存储接口
|- oauth         Authorization Code + PKCE
|- sso           Device Authorization、账号/角色选择
|- cache         OAuth/SSO/STS 缓存、锁和原子写入
`- provider      获取及自动刷新 AK/SK/SessionToken
                 |
                 v
internal/tlsapi
每次请求前 provider.Get()
-> base.Credentials{Region, Service: "TLS"}
-> 现有 SignV4
-> log-service
```

### 4.1 边界原则

- `internal/auth` 不导入 `internal/tlsapi`，也不理解 TLS action、tool、workflow 或输出 envelope。
- `internal/tlsapi` 不理解 SSO、OAuth、账号或角色，只消费标准凭证值。
- 配置目录通过接口或构造参数注入，认证核心不硬编码 `~/.volclog` 或 `~/.volcengine`。
- 命令层负责参数解析、交互、结构化输出和错误映射；认证核心返回类型化结果和错误。
- 公共类型使用中性命名，避免在认证核心中嵌入 TLS 专属概念，降低未来上游合并成本。

### 4.2 Provider 抽象

认证请求路径使用可刷新 Provider。概念接口如下：

```go
type Value struct {
    AccessKeyID     string
    SecretAccessKey string
    SessionToken    string
    ProviderName    string
    ExpiresAt       time.Time
}

type Provider interface {
    Retrieve(ctx context.Context) (Value, error)
    IsExpired() bool
}
```

具体实现：

- `StaticProvider`：包装现有 AK/SK 或手工 STS；
- `SSOProvider`：读取 SSO token/profile，必要时刷新并换取角色 STS；
- `ConsoleLoginProvider`：读取 Console Login cache，必要时刷新 OAuth/STS。

Provider 的并发缓存和刷新需要串行化，避免同一进程内多个并发请求重复刷新或发生 RefreshToken rotation 冲突。

## 5. 命令设计

命令名称和主要参数与 `volcengine-cli` 对齐，仅使用 `volclog` 前缀。

### 5.1 SSO

```bash
volclog configure sso-session --name corp \
  --start-url https://example.volccloudidentity.com/userportal \
  --region cn-beijing

volclog configure sso --profile prod --sso-session corp
volclog sso login --profile prod
volclog sso logout --profile prod
```

语义：

- `configure sso-session` 保存企业 SSO 入口、region 和 scopes。
- `configure sso` 绑定 profile、SSO session、账号和角色，并完成首次授权。
- `sso login` 为已有 profile 重新登录或更新授权。
- `sso logout` 清理该 profile 对应的 SSO token 和临时 STS，不删除 TLS region、endpoint、timeout 等运行配置。

### 5.2 Console Login

```bash
volclog login --profile prod
volclog login --profile prod --remote
volclog logout --profile prod
```

语义：

- 默认使用 Authorization Code + PKCE，并在 loopback 随机端口接收回调。
- `--remote` 用于无浏览器或远程终端场景。
- 浏览器自动打开失败时，命令打印不含秘密的授权 URL 和操作提示。
- `logout` 清理 profile 对应的 Console Login cache，不删除 TLS 运行配置。

## 6. 配置与缓存

### 6.1 路径

```text
~/.volclog/config.json
~/.volclog/sso/cache/
~/.volclog/login/cache/
```

- 目录权限为 `0700`；
- 配置和缓存文件权限为 `0600`；
- 写入通过同目录临时文件加原子 rename 完成；
- 缓存文件名使用稳定、不可逆的 session 标识摘要，不直接暴露 Start URL、账号或角色。

### 6.2 Config 扩展

现有 Config 和 Profile 字段保持不变。新增字段使用 `omitempty`，旧配置不需要迁移。

Config 新增：

```text
sso-session
```

Profile 新增：

```text
mode
sso-session-name
account-id
role-name
login-session
sts-expiration
```

认证字段名称与 `volcengine-cli` 对齐；现有 `current_profile`、`access_key_id`、`secret_access_key`、`security_token`、`region`、`endpoint`、`timeout_seconds` 和 `cred_ref` 保持原样。

同一个 profile 同时代表 TLS 运行目标和认证身份。认证命令只更新认证字段，必须保留既有 region、endpoint、timeout、输出和其他 TLS 配置。

### 6.3 临时凭证存储

- SSO 可将当前临时 AK/SK/SessionToken 和 `sts-expiration` 写入 profile，以便新进程复用；刷新后原子更新。
- Console Login 的 OAuth/STS 材料存储在独立 login cache，profile 仅保存 `mode` 和 `login-session` 等索引字段。
- 所有显示命令只输出 provider、profile、掩码 AK 和过期时间，不输出 SK、SessionToken、AccessToken 或 RefreshToken。

## 7. 登录和请求数据流

### 7.1 SSO

```text
configure sso-session
-> 保存入口配置
-> configure sso / sso login
-> Device Authorization
-> 用户在浏览器确认
-> 获取可访问账号和角色
-> 用户选择账号和角色
-> Portal 换取 STS
-> 原子写入 SSO cache/profile
-> API 请求前 SSOProvider 检查并刷新
```

### 7.2 Console Login

```text
login
-> 生成 state、PKCE verifier/challenge
-> Authorization Code 授权
-> loopback callback 或 --remote 交互
-> 校验 state 并交换 token
-> 原子写入 login cache/profile
-> API 请求前 ConsoleLoginProvider 检查并刷新
```

### 7.3 TLS 请求

```text
解析 profile.mode
-> StaticProvider / SSOProvider / ConsoleLoginProvider
-> 获取当前 AK/SK/SessionToken
-> 生成 base.Credentials
-> Region 使用原有显式解析结果
-> Service 固定为 "TLS"
-> 执行现有 SignV4
```

动态凭证必须在每个请求签名前解析。只在 Client 创建时物化一次凭证不满足长分页、批量任务和常驻 Agent 的刷新要求。

## 8. AK/SK 零回归兼容设计

### 8.1 分流

```text
profile.mode 为空或 ak
-> 完整执行现有 AK/SK 路径

profile.mode = sso
-> SSOProvider

profile.mode = console-login
-> ConsoleLoginProvider

其他 mode
-> 明确返回 unsupported mode
```

只有显式写入 `mode=sso` 或 `mode=console-login` 才启用新路径。

### 8.2 必须保持的旧行为

- `VOLCENGINE_ACCESS_KEY_ID`、`VOLCENGINE_ACCESS_KEY_SECRET`、`VOLCENGINE_TOKEN` 的名称和优先级不变；
- 完整环境 AK/SK 覆盖静态 profile 的现有语义不变；
- `--secrets-file` 行为及其与 `--profile` 的互斥关系不变；
- inline AK/SK、cred-ref 和手工 STS 三元组行为不变；
- region、endpoint、timeout 和 `Service=TLS` 行为不变；
- 缺少 `mode` 的旧 profile 自动等价于 `mode=ak`；
- 现有脚本、Agent skill、CI 环境变量和调用参数不需要修改。

`StaticProvider` 只统一接口，不重写现有凭证解析规则。实现时必须复用现有解析函数，并通过改造前后的签名等价测试证明行为未变化。

### 8.3 新模式的身份优先级

对 `mode=sso` 或 `mode=console-login`：

```text
显式 --profile
> current_profile
> 对应动态 Provider
```

环境 AK/SK 不得覆盖已明确选择的动态身份。

如果动态 Provider 失败，必须失败关闭，禁止回退到环境 AK/SK、其他 profile 或过期 STS。这避免用户认为自己在使用 SSO/Console 身份，实际却静默切换成长期 AK。

## 9. 错误处理

认证核心返回可分类错误，至少覆盖：

- profile 或 SSO session 配置不完整；
- 登录缓存不存在或损坏；
- AccessToken/STS 过期；
- RefreshToken 刷新失败或失效；
- OAuth state/PKCE 校验失败；
- callback 超时或端口监听失败；
- 账号或角色不可用；
- 服务端拒绝、网络错误和协议响应不完整。

命令层将错误映射到现有结构化错误契约，并给出可执行恢复提示，例如：

```text
authentication expired; run:
volclog sso login --profile prod
```

错误、日志、trace、doctor 和 envelope 中禁止包含：

- SecretAccessKey；
- SessionToken；
- OAuth AccessToken；
- RefreshToken；
- OIDC/SAML 原始断言；
- PKCE verifier。

## 10. 安全要求

- OAuth callback 只监听 loopback 随机端口；
- 必须校验 state 和 PKCE；
- 对授权、token、Portal endpoint 采用与上游相同的来源和校验规则；
- 登录命令可打开浏览器，因为这是用户显式发起的外部动作；
- Token cache、配置和临时文件不得使用宽松权限；
- RefreshToken rotation 的写回必须加锁且原子化；
- trace redaction 和错误序列化测试必须覆盖所有敏感字段；
- `doctor` 只报告认证类型、来源、是否可用、过期时间和掩码身份；
- SSO/Console profile 认证失败时不允许自动降级为长期 AK。

## 11. 测试与验收

### 11.1 旧行为锁定

先为当前行为补充测试，再修改请求链：

- profile 内联 AK/SK；
- `cred-ref`；
- 环境 AK/SK；
- `--secrets-file`；
- 手工 STS；
- 环境变量与静态 profile 的现有优先级；
- region、endpoint 和 timeout；
- 固定请求的 SignV4 header。

改造前后，同一固定请求应产生等价的 `Authorization`、`X-Content-Sha256` 和签名 scope；静态 STS 继续携带并签入 `X-Security-Token`。时间相关字段通过注入固定时钟进行比较。

### 11.2 认证核心

- 配置和 cache 的解析、权限、原子写入；
- OAuth state、PKCE、callback 和 `--remote`；
- SSO Device Authorization、账号/角色选择；
- 缓存命中、即将过期、已过期和刷新失败；
- RefreshToken rotation；
- 同进程并发刷新只发生一次；
- 新进程可读取已有 cache 并继续请求；
- 损坏 cache 和不完整 profile 明确失败；
- 动态身份失败时不回退到 AK/SK；
- 所有输出路径的敏感信息扫描。

### 11.3 请求集成

- 每个请求签名前调用 Provider；
- 长分页中间发生 STS 刷新后继续成功；
- `Service` 始终为精确的大写 `TLS`；
- region 与 endpoint 继续使用现有解析规则；
- SSO/Console 请求携带有效 `X-Security-Token`；
- 缺失或错误 token、错误 region、错误 service 负例。

### 11.4 真实环境验收

在公网测试账号完成：

1. SSO 首次登录并调用只读 `DescribeProjects`；
2. SSO 重新登录和 STS 刷新；
3. Console Login 本地浏览器流程；
4. Console Login `--remote` 流程；
5. 新进程复用 cache；
6. 服务端 IAM Identity、SessionPolicy、ClientType 和 RequestID 核对；
7. 日志、trace 和 CLI 输出的凭证泄漏检查。

## 12. 实施阶段和门禁

### 阶段 A：兼容基线与认证骨架

- 建立旧 AK/SK 行为锁定测试；
- 引入 Provider 接口和 StaticProvider；
- 证明静态路径签名等价；
- 建立独立配置目录和安全 cache 基础设施。

门禁：所有旧测试和新增兼容测试通过，AK/SK 行为无变化。

### 阶段 B：Console Login

- 移植 OAuth Authorization Code + PKCE；
- 实现本地 callback、`--remote`、login/logout；
- 实现 ConsoleLoginProvider 和刷新。

门禁：本地模拟协议、并发刷新和敏感信息测试通过。

### 阶段 C：SSO

- 实现 sso-session 配置；
- 移植 Device Authorization、账号/角色选择和 Portal STS；
- 实现 SSOProvider、login/logout 和刷新。

门禁：本地模拟协议、角色选择、刷新和失败关闭测试通过。

### 阶段 D：TLS 接入和公网验收

- 在每次请求签名前解析动态凭证；
- 更新 doctor、帮助和用户文档；
- 运行全量测试和公网只读验收。

门禁：SSO/Console 真实调用成功，AK/SK 回归测试通过，无敏感信息泄漏。

## 13. 未来并入 volcengine-cli 的原则

Phase 1 不直接改动 `volcengine-cli`，但认证核心必须满足：

- 不依赖 `volclog` 命令解析和输出格式；
- 不依赖 TLS endpoint、action 或业务请求；
- 配置目录和网络 client 可注入；
- profile、SSO session、OAuth cache 使用可映射到上游的数据模型；
- 协议客户端和 Provider 有独立单元测试；
- 未来优先把认证核心并入或抽取给 `volcengine-cli`，而不是把 TLS 业务代码带入上游。

是否真正并入、以复制、重构还是共享 module 的方式并入，需要在 Phase 1 稳定后基于上游最新代码重新评估。

## 14. 完成标准

Phase 1 只有同时满足以下条件才算完成：

- `volclog` 在未安装、未配置 `ve` 的环境中独立完成 SSO 和 Console Login；
- 所有登录态只存在于 `~/.volclog`；
- SSO/Console 能自动刷新并完成 TLS SignV4 请求；
- 原有 AK/SK、cred-ref、secrets-file、环境变量和手工 STS 行为无变化；
- 动态认证失败不会静默回退到 AK/SK；
- 全量测试、真实公网只读验收和敏感信息审计通过；
- 文档明确 Phase 1 范围、恢复命令和后续上游合并边界。
