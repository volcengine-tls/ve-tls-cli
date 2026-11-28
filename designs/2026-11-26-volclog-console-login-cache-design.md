# volclog Console Login 加密缓存方案

> 实施方案 · 2026-11-27

本方案适用于 `volclog login` 创建的 Console Login 会话。使用低权限 IAM 子用户登录。 [Console Login 前置条件](../docs/2-Authentication_zh.md#52-前置条件)

## 1. 目标和保护边界

Console Login 支持本机浏览器及远程设备码。授权后，同一系统用户可以在独立进程中使用和续期。STS 三元组及 OAuth 令牌在会话文件和写入临时文件里以密文保存。profile 保存会话绑定和 TLS 配置，CLI 输出与 trace 对会话凭证脱敏。 [设备码登录](../docs/2-Authentication_zh.md#5-console-login)

磁盘加密保护单独被复制的会话文件。Linux 上同时取得密钥和密文的进程有解密能力。macOS 和 Windows 的解密权限由系统凭据服务或当前用户保护上下文决定。Agent 拥有同一登录身份时，可以执行该身份获准的 TLS 操作。云端权限由 IAM 决定。 [TLS IAM 权限策略](https://docs.volcengine.com/docs/torch-log-service/iam-overview?lang=zh)

## 2. 缓存格式与会话生命周期

登录、业务请求、`doctor` 和 `logout` 使用 `ConsoleCache` 和同一个缓存目录解析规则。默认目录为 `~/.volclog/login/cache/`，也可通过 `VOLCLOG_LOGIN_CACHE_DIRECTORY` 选择私有目录。会话沿用旧版的跨进程锁，密文存为 `<会话哈希>.enc`。 [缓存实现](../internal/auth/console/login.go) · [业务与诊断入口](../internal/app/runtime/providers.go)

Linux 和 macOS 用每个目录独立生成的 256 位密钥加密完整会话。AES-GCM 每次写入使用随机 nonce，格式版本与会话标识参与认证。Windows 由用户级 DPAPI 保护完整会话，格式版本与会话标识作为附加参数绑定密文。 [加密实现](../internal/auth/console/cache_protection.go) · [Windows DPAPI](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata)

| 入口 | 缓存行为 |
| --- | --- |
| `login` | 授权前检查存储能力。在会话锁内写密文并更新 profile，成功后清理对应的旧明文。profile 更新失败时恢复先前的加密会话，或删除这次新建的密文，保留旧明文供升级重试。 |
| 业务请求与续期 | 在会话锁内重读缓存。接近过期时续期并原子回写密文，确认写入成功后再使用新凭证。 |
| `doctor` | 读取本地会话状态和过期时间，对旧明文、密钥不可用、密钥丢失和密文损坏给出恢复提示。 |
| `logout` | 在会话锁内清除密文、同一会话的旧明文文件和所有仍绑定它的 profile。目录密钥供其他会话继续使用。 |

## 3. 平台存储

| 平台 | 存储与会话复用 |
| --- | --- |
| Linux、SSH、Docker | 在 `0700` 缓存目录中保存 `0600` 的 `master.key`，不同进程用同一目录密钥打开会话。默认 Docker 使用普通文件操作。容器重建后，如需复用，私有卷需保留密钥、会话密文和 profile 的登录会话绑定，且运行身份对它们有私有访问权限。整个目录及备份均按凭据管理。 [飞书 Linux 后端](https://github.com/larksuite/cli/blob/main/internal/keychain/keychain_other.go) |
| macOS | 登录 Keychain 按缓存目录存储密钥。读取需要符合该凭据条目的访问控制。Keychain 锁定或换了可执行文件时，通过 Keychain Access 解锁或授权。`volclog` 的 macOS 发布构建带有 Keychain 支持。 [Apple Keychain](https://developer.apple.com/documentation/technotes/tn3137-on-mac-keychains) · [发布配置](../.github/workflows/release-volclog.yml) |
| Windows | 当前用户的 DPAPI 保护会话，密文文件使用私有 ACL。复用受 Windows 用户与系统保护上下文约束。 [Microsoft DPAPI](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata) |

## 4. 旧缓存升级

停止旧版进程后，使用新版在原配置和缓存目录执行 `volclog login --profile 原名称`，用原账号重新授权。新会话和绑定保存后再删除对应的旧 `.json`。共享同一会话的其他 profile 可以继续使用原绑定。旧文件清理失败时，已保存的加密会话可以使用，重新登录可以重试清理。已解绑且无需重登的旧文件需要确认归属后清理。旧版再次登录时仍会创建明文缓存。 [退出与迁移指引](../docs/2-Authentication_zh.md#58-退出)

## 5. 验收

- macOS 本机和 Linux 开发机使用模拟 OAuth 和 TLS 服务验证设备码登录、独立进程读取、连续两轮续期、离线诊断和退出。Linux 同时检查目录及密钥权限，在默认 Docker 中验证相同文件路径。
- 使用虚拟凭证覆盖同名 profile 升级、共享绑定、配置更新失败后的缓存回滚，以及密文篡改、跨会话替换、Linux 密钥丢失及并发首次创建。确认明文凭证未出现在新生成的磁盘缓存、临时文件及 CLI 输出中。
- 运行 `go test ./...`。Windows 验证 DPAPI 代码和测试可交叉编译，实际系统调用由 Windows 运行验证。
