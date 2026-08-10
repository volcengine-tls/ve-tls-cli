# 2. 认证

[← 上一篇：快速开始](1-Getting-Started_zh.md) | [English](2-Authentication.md) | [下一篇：配置 →](3-Configuration_zh.md)

本文介绍 `volclog` 支持的全部鉴权方式，以及如何选择、配置、验证、刷新和清理登录状态。

无论使用哪种方式，最终都需要得到一组可用于 TLS 请求签名的凭证。区别主要在于：凭证由谁提供、是否需要交互登录、是否自动刷新、是否写入本地缓存，以及适合个人终端还是工作负载。

## 1. 如何选择鉴权方式

| 方式 | 推荐场景 | 用户需要提供 | 凭证保存与刷新 |
| --- | --- | --- | --- |
| 静态 AK/SK | 兼容已有配置、本地固定身份 | AK、SK | 保存到 profile，或通过环境变量、secrets file 临时注入；不自动轮换 |
| 手工 STS | 已经从其他系统取得临时凭证 | 临时 AK、临时 SK、Session Token | 由用户注入；过期后需要重新取得完整三元组 |
| Console Login | 个人终端、没有长期 AK/SK、希望通过控制台授权 | 浏览器登录授权 | 登录态写入本地安全缓存；接近过期时自动刷新 |
| SSO | 企业统一身份、账号和角色由 SSO 分配 | SSO Start URL、账号和角色 | OAuth 与 STS 状态写入本地安全缓存；接近过期时自动刷新 |
| RAM Role ARN | 使用一个已有身份扮演目标账号中的角色 | 源 AK/SK、目标账号 ID、角色名 | 临时 STS 仅缓存在当前进程内；接近过期时自动重新 AssumeRole |
| OIDC | VKE、CI 或其他提供 OIDC Token 的工作负载 | OIDC Token 文件、角色 TRN | Token 文件在每次刷新时重新读取；临时 STS 仅缓存在当前进程内 |
| ECS Role | 运行在已绑定实例角色的 ECS 实例上 | ECS 角色名 | 通过实例元数据服务获取；临时凭证仅缓存在当前进程内 |

简单选择建议：

- 已有稳定 AK/SK，且需要保持原有使用方式：继续使用静态 AK/SK。
- 已经拿到一组短期 AK/SK/Session Token：使用手工 STS。
- 个人在本地或远程终端交互使用：优先考虑 Console Login。
- 企业通过统一身份入口分配账号和角色：使用 SSO。
- 需要从一个 RAM 身份切换到另一个账号或角色：使用 RAM Role ARN。
- 工作负载可以获得 OIDC Token：使用 OIDC。
- 程序直接运行在绑定了实例角色的 ECS 上：使用 ECS Role。

不要根据示例猜测账号 ID、角色名、Start URL、Token 路径、Region 或 Endpoint。这些值必须由用户或环境管理员提供。

## 2. 开始前先理解 Profile

Profile 用于保存一套身份与 TLS 运行配置。业务命令选择 profile 的顺序是：

1. 命令中显式指定的 `--profile NAME`；
2. 配置文件中的 `current_profile`；
3. 名为 `default` 的 profile。

查看和切换 profile：

```bash
volclog configure list
volclog configure show --profile NAME
volclog configure use NAME
```

也可以只让一条命令使用指定 profile：

```bash
volclog --profile NAME doctor
volclog --profile NAME tool exec project.describe-projects
```

除非你确实希望改变后续命令的默认身份，否则建议在验证阶段始终显式传入 `--profile`。

### 2.1 Region 和 Endpoint

TLS 请求必须能够确定 Region 和 Endpoint。配置示例：

```bash
--region cn-beijing \
--endpoint https://tls-cn-beijing.volces.com
```

对于 SSO、Console Login、RAM Role ARN、OIDC 和 ECS Role，运行配置按以下顺序解析：

1. 本次调用的全局 `--region`、`--endpoint`；
2. `VOLCENGINE_REGION`、`VOLCENGINE_ENDPOINT`；
3. 所选 profile 中的 `region`、`endpoint`；
4. 当前 `tool` 或 `workflow` 执行的 `context.region`、`context.endpoint` 默认值；
5. 当前目录的项目配置。

CLI 不会根据 Region 推导 Endpoint，必须通过其中一层显式提供两个值。timeout 未配置时使用 60 秒。

如果动态登录 profile 中没有 TLS 运行配置，可以在执行 TLS 命令时显式提供环境变量：

```bash
VOLCENGINE_REGION=cn-beijing \
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog --profile NAME doctor --online
```

执行上下文默认值仅适用于 `tool` 和 `workflow` 命令，不会覆盖非空的环境变量或 profile 值。如果当前目录已经有项目配置，也可以使用其中的 Region 和 Endpoint。项目配置跟随工作目录；离开该目录执行时，应确保 profile、执行上下文或环境变量仍能提供正确的值。完整的静态与动态优先级规则见[配置](3-Configuration_zh.md#5-运行时优先级)。

静态模式下，如果同时存在完整的环境 AK/SK，则这组环境凭证优先于 profile；相应的 Region、Endpoint 可同时通过环境变量提供。为了减少身份和运行环境混用，生产调用建议显式使用一套完整配置。

### 2.2 配置检查和真实访问验证

离线检查不会为了验证而主动申请工作负载临时凭证：

```bash
volclog --profile NAME doctor
```

它适合检查：

- profile 是否存在；
- 鉴权模式和必填字段是否齐全；
- Region 和 Endpoint 是否能够解析；
- OIDC Token 文件是否可访问；
- 登录缓存是否存在或是否需要刷新；
- 是否启用了不安全的鉴权选项。

真实验证使用：

```bash
volclog --profile NAME doctor --online
```

`doctor --online` 会进行网络检查并发送最小只读 TLS 请求。只有在线检查成功，才能证明当前身份确实可以访问 TLS。

也可以直接执行一个只读命令：

```bash
volclog --profile NAME tool exec project.describe-projects
```

验证过程中不要打印真实 AK、SK、Session Token、OAuth Token 或 OIDC Token。

## 3. 静态 AK/SK

### 3.1 适用场景

- 兼容已有的 AK/SK 使用方式；
- 本地长期使用一个固定身份；
- 自动化环境已经有安全的密钥注入机制；
- 暂时不需要自动登录或自动轮换。

### 3.2 写入 Profile

```bash
volclog configure set \
  --profile default \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

`--mode ak` 可以显式填写，也可以省略：

```bash
volclog configure set \
  --profile prod \
  --mode ak \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

省略 `--mode` 会完整保留原有静态 AK/SK 行为。新增其他 provider 不会改变已有 AK/SK 的访问语义。

如果还没有 current profile，首次成功配置的 profile 会成为 current。已有 current profile 时，配置另一个 profile 不会自动切换，按需执行：

```bash
volclog configure use prod
```

### 3.3 使用共享 Credential Reference

当同一套 AK/SK 需要访问多个 Region 或 Endpoint 时，可以把凭证保存为一个共享引用：

```bash
volclog configure set \
  --profile tls-bj \
  --cred-ref shared-account \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

后续 profile 只引用该凭证：

```bash
volclog configure set \
  --profile tls-sg \
  --cred-ref shared-account \
  --region ap-southeast-1 \
  --endpoint https://tls-ap-southeast-1.volces.com
```

### 3.4 使用 Secrets File

CI、Agent 或一次性执行建议使用权限受控的 secrets file，避免把凭证写入 profile。

例如 `/secure/path/volclog.env`：

```dotenv
VOLCENGINE_ACCESS_KEY_ID=<access-key-id>
VOLCENGINE_ACCESS_KEY_SECRET=<secret-access-key>
VOLCENGINE_REGION=cn-beijing
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
```

限制权限并显式使用：

```bash
chmod 600 /secure/path/volclog.env
volclog --secrets-file /secure/path/volclog.env doctor --online
```

`--profile` 与 `--secrets-file` 是互斥的运行时选择器，不要在同一条命令中同时使用。

### 3.5 使用环境变量

```bash
export VOLCENGINE_ACCESS_KEY_ID='<access-key-id>'
export VOLCENGINE_ACCESS_KEY_SECRET='<secret-access-key>'
export VOLCENGINE_REGION='cn-beijing'
export VOLCENGINE_ENDPOINT='https://tls-cn-beijing.volces.com'

volclog doctor --online
```

环境变量应只注入到确实需要使用它们的进程。避免在共享 shell、构建日志或调试输出中回显密钥。

### 3.6 验证和清理

```bash
volclog --profile prod doctor
volclog --profile prod doctor --online
```

删除不再使用的 profile：

```bash
volclog configure delete prod
```

删除共享 credential 前，先确认没有其他 profile 仍在引用它：

```bash
volclog configure cred delete shared-account
```

静态 AK/SK 不会自动轮换。密钥失效或被禁用后，需要由用户更新 profile、secrets file 或环境变量。

## 4. 手工 STS 临时凭证

### 4.1 适用场景

当用户已经从其他授权系统取得临时凭证，但不需要 `volclog` 负责申请和刷新时，使用手工 STS。

一组有效的 STS 凭证必须同时包含：

- 临时 Access Key ID；
- 临时 Secret Access Key；
- Session Token。

三者必须来自同一次签发，不能把 Session Token 与另一组 AK/SK 混用。

### 4.2 写入临时 Profile

```bash
volclog configure set \
  --profile temp-sts \
  --ak '<temporary-access-key-id>' \
  --sk '<temporary-secret-access-key>' \
  --token '<session-token>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

验证：

```bash
volclog --profile temp-sts doctor --online
```

这种方式会把临时三元组保存到 profile。凭证过期后，需要重新取得并覆盖。

### 4.3 一次性注入

Secrets file：

```dotenv
VOLCENGINE_ACCESS_KEY_ID=<temporary-access-key-id>
VOLCENGINE_ACCESS_KEY_SECRET=<temporary-secret-access-key>
VOLCENGINE_TOKEN=<session-token>
VOLCENGINE_REGION=cn-beijing
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
```

```bash
chmod 600 /secure/path/volclog-sts.env
volclog --secrets-file /secure/path/volclog-sts.env doctor --online
```

或者仅给单条命令注入：

```bash
VOLCENGINE_ACCESS_KEY_ID='<temporary-access-key-id>' \
VOLCENGINE_ACCESS_KEY_SECRET='<temporary-secret-access-key>' \
VOLCENGINE_TOKEN='<session-token>' \
VOLCENGINE_REGION='cn-beijing' \
VOLCENGINE_ENDPOINT='https://tls-cn-beijing.volces.com' \
volclog tool exec project.describe-projects
```

`volclog` 不会替手工 STS 续期。过期后必须从签发方获取新的完整三元组。

## 5. Console Login

### 5.1 适用场景

- 个人在本地终端交互使用；
- 不希望配置长期 AK/SK；
- 可以通过浏览器完成控制台授权；
- 在远程开发机上工作，但可以在本地浏览器完成跨设备授权。

### 5.2 前置条件

- 确认用户账号具备目标 TLS 资源权限；
- 明确 TLS Region 和 Endpoint；
- 远程模式下，可以把终端显示的授权 URL 复制到本地浏览器，并把授权码粘贴回终端。

`login --region --endpoint` 会把两个 TLS 运行值写入目标 profile。任一参数都可以省略以保留旧值，之后再通过 `configure set` 补充。`--login-endpoint` 用于选择 Console OAuth 服务，与 TLS 业务 Endpoint 相互独立。

### 5.3 本地浏览器登录

```bash
volclog login \
  --profile console-dev \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

本地模式使用 loopback callback 接收浏览器授权结果。登录成功后，profile 会切换为 `mode=console-login`，保存登录会话绑定，并且只修补显式提供的 TLS 运行字段。未提供的已有身份和运行字段保持不变，新 profile 不会自动写入默认 Region。

Console 授权服务默认使用 `https://signin.volcengine.com`。如需选择其他兼容的 Console OAuth 根地址，可使用 `--login-endpoint`。登录地址必须是干净的 HTTPS 根地址，不能包含用户信息、查询参数、片段或非根路径。地址会在规范化后随登录缓存保存，后续自动刷新会继续使用同一个授权服务；不含该字段的旧缓存仍使用默认地址。`--endpoint` 始终表示 TLS 业务 Endpoint。

### 5.4 远程或跨设备登录

开发机无法直接显示浏览器页面时使用：

```bash
volclog login \
  --profile console-dev \
  --region cn-beijing \
  --remote
```

按终端提示：

1. 在可以看到页面的设备上打开授权 URL；
2. 完成登录和授权；
3. 将授权码粘贴回运行 `volclog` 的终端。

### 5.5 使用和验证

```bash
volclog configure use console-dev
volclog --profile console-dev doctor
volclog --profile console-dev doctor --online
```

登录成功不等于已经具备目标 TLS 权限。`doctor --online` 成功才说明当前临时身份可以完成 TLS 只读访问。

### 5.6 刷新和重新登录

Console Login 的硬过期时间由缓存令牌的 `IssuedAt` 加上 `ExpiresIn`（Console 服务返回的有效期）计算得出。当临时凭证距离该硬过期时间不足 60 秒时，业务命令会尝试使用缓存中的刷新材料自动换取新凭证。这些有效期由 Console 服务设定，不是用户可配置的 CLI 参数。

以下情况需要重新登录：

- 本地缓存不存在、损坏或已被清理；
- 刷新材料不存在、过期或被撤销；
- 服务端返回无法继续静默刷新的错误；
- CLI 返回 `ReauthRequired`。

恢复命令：

```bash
volclog login --profile console-dev --region cn-beijing
```

业务命令不会在后台主动打开浏览器。

### 5.7 退出

Console 退出是会话范围的。`volclog logout --profile NAME` 仅使用指定 profile 来解析其 `login-session`；随后删除该会话的缓存，并清除仍绑定到同一会话的每个 console-login profile 的 `login-session` 绑定，而不仅是指定的 profile。如果多个 profile 共享同一个登录会话，退出其中一个会为所有 profile 清除共享会话。

退出指定 profile（解析并清除其会话）：

```bash
volclog logout --profile console-dev
```

`logout --all` 遍历所有已知的 console-login profile，按 login-session 分组，并清除每个会话。

```bash
volclog logout --all
```

退出会删除本地登录缓存并清除受影响 profile 的登录会话绑定，但不会删除 profile，也不会删除其中保留的 TLS 运行配置。它也不会移除可能因之前的静态配置而残留的任何休眠静态 `AccessKeyID`、`SecretAccessKey`、`SecurityToken` 或 `CredRef` 字段。

目前没有专门的字段级 CLI 命令可以仅清除休眠的静态字段。如果你的安全策略要求物理删除这些值，不要假设 `logout` 就足够了。请保留你仍然需要的非密运行时值（region、endpoint、timeout），然后通过干净的动态设置删除并重建 profile，或通过你批准的安全配置管理流程移除这些字段。被其他 profile 引用的共享凭证不能删除；只有在确认不再被引用后，才能删除不需要的共享凭证。完整的存储和清理模型见[配置](3-Configuration_zh.md)。

## 6. SSO

### 6.1 适用场景

- 企业统一身份入口；
- 登录后需要选择账号和角色；
- 多个 profile 可以复用同一个 SSO Session；
- 希望日常业务命令自动刷新 OAuth 和 STS 临时凭证。

### 6.2 需要准备的信息

- SSO Session 名称，由用户自行命名；
- SSO Start URL，由企业身份管理员提供；
- SSO 服务所在 Region；
- 一个已经存在的目标 profile；
- 目标账号 ID 和角色名；也可以在首次配置时交互选择；
- TLS 使用的 Region 和 Endpoint。

SSO Session 的 Region 用于 SSO 登录，TLS profile 的 Region 用于 TLS 请求签名，两者含义不同，不要相互替代。

### 6.3 配置 SSO Session

```bash
volclog configure sso-session \
  --name corp \
  --start-url 'https://example.volccloudidentity.com/userportal' \
  --region cn-beijing
```

可选的 registration scopes：

```bash
volclog configure sso-session \
  --name corp \
  --start-url 'https://example.volccloudidentity.com/userportal' \
  --region cn-beijing \
  --registration-scopes cloudidentity:account:access,offline_access
```

未显式提供 scopes 时，新 Session 使用默认 scopes。更新已有 Session 时，省略该参数会保留原值。

### 6.4 绑定 Profile 并首次登录

`configure sso` 可以创建或更新 profile，并把它绑定到 SSO Session。它会把鉴权模式切换为 `sso`，只修补显式提供的 TLS Region/Endpoint，并保留 timeout 和休眠身份字段。绑定同时清除 Console Login 绑定并重置旧的 STS 过期元数据。

交互选择账号和角色：

```bash
volclog configure sso \
  --profile sso-dev \
  --sso-session corp \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

账号和角色已经明确时：

```bash
volclog configure sso \
  --profile sso-dev \
  --sso-session corp \
  --account-id '<account-id>' \
  --role-name '<role-name>'
```

无图形界面的终端可以禁止自动打开浏览器：

```bash
volclog configure sso \
  --profile sso-dev \
  --sso-session corp \
  --no-browser
```

命令会输出授权 URL 和设备码提示。请在可以访问页面的浏览器中完成授权。

`configure sso` 不会自动切换 current profile。完成后显式切换：

```bash
volclog configure use sso-dev
```

如果登录时省略了 TLS Region 或 Endpoint，可以在不改变 SSO 绑定的情况下补充：

```bash
volclog configure set --profile sso-dev \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

### 6.5 使用和验证

首次 `configure sso` 之后，本地状态可能只包含 OAuth Token，尚未包含角色 STS 凭证。在这个预期的初始状态下，离线 `doctor` 会报告凭证未就绪（`present=false`）并以退出码 2 退出。这并不意味着浏览器登录失败；它仅表示按需的角色 STS 交换尚未发生。

运行在线检查或真实业务命令来执行按需 STS 交换：

```bash
volclog --profile sso-dev doctor --online
```

在线交换成功后，STS 凭证会被缓存，后续的离线 `doctor` 可以报告其已存在。也可以直接执行只读业务命令：

```bash
volclog --profile sso-dev tool exec project.describe-projects
```

首次真实业务请求会按需取得目标账号和角色对应的 STS 临时凭证。

### 6.6 自动刷新和重新登录

SSO OAuth Token 和 STS 凭证均使用 60 秒安全刷新窗口。它们的硬过期时间来自服务响应（OAuth 的 `ExpiresAt` 和 STS 的 `Expiration`）；两者均在硬过期前 60 秒刷新。这些有效期由 SSO 服务设定，不是用户可配置的 CLI 参数：

- 缓存仍然有效时直接复用；
- 接近过期时，业务命令使用 refresh token 静默刷新 OAuth Token；
- STS 缺失或接近过期时，使用有效的 OAuth Token 换取新的角色凭证；
- 普通业务命令不会启动设备授权，也不会打开浏览器。

只有静默刷新无法继续时才需要显式重新登录：

```bash
volclog sso login --profile sso-dev
```

也可以直接按 Session 重新登录：

```bash
volclog sso login --sso-session corp
```

无图形界面：

```bash
volclog sso login --profile sso-dev --no-browser
```

`--profile` 与 `--sso-session` 是互斥选择器。

### 6.7 退出

SSO 退出是会话范围的。`volclog sso logout --profile NAME` 仅使用指定 profile 来解析其 SSO Session；随后与直接选择 `--sso-session` 具有相同的会话级清理范围。它会删除该 Session 的 OAuth Token、与该 Session 关联的所有唯一 STS 缓存，并清除仍绑定到该 Session 的每个 profile 的 STS 过期状态。如果多个 profile 共享同一个 SSO Session，退出其中一个会为所有 profile 清除共享 Session 状态。

按 profile 退出（解析并清除其 Session）：

```bash
volclog sso logout --profile sso-dev
```

按 Session 退出：

```bash
volclog sso logout --sso-session corp
```

退出不会删除 SSO Session 配置、profile、账号 ID、角色名或 TLS 运行配置。它也不会移除可能因之前的静态配置而残留的任何休眠静态 `AccessKeyID`、`SecretAccessKey`、`SecurityToken` 或 `CredRef` 字段。[5.7 节](#57-退出)中描述的相同休眠字段保留和清理指导同样适用于此处。

## 7. RAM Role ARN

### 7.1 适用场景

- 已有源 RAM 身份，需要扮演另一个角色；
- 访问目标账号中的 TLS 资源；
- 希望避免把目标角色的长期 AK/SK 分发给使用者；
- 本地工具或自动化任务能够安全提供源凭证。

### 7.2 前置条件

- 源身份具有 `sts:AssumeRole` 权限；
- 目标角色的信任策略允许该源身份扮演；
- 用户明确知道目标账号 ID 和角色名；
- 目标角色具备所需 TLS 权限；
- 已确定 TLS Region 和 Endpoint。

### 7.3 使用内联源 AK/SK

```bash
volclog configure set \
  --profile ram-tls-readonly \
  --mode ramrolearn \
  --account-id '<target-account-id>' \
  --role-name '<target-role-name>' \
  --ak '<source-access-key-id>' \
  --sk '<source-secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

如果源身份本身是临时身份，同时提供源 Session Token：

```bash
volclog configure set \
  --profile ram-tls-readonly \
  --mode ramrolearn \
  --account-id '<target-account-id>' \
  --role-name '<target-role-name>' \
  --ak '<source-access-key-id>' \
  --sk '<source-secret-access-key>' \
  --token '<source-session-token>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

### 7.4 使用 Credential Reference

```bash
volclog configure set \
  --profile ram-tls-readonly \
  --mode ramrolearn \
  --account-id '<target-account-id>' \
  --role-name '<target-role-name>' \
  --cred-ref source-account \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

### 7.5 验证、刷新和清理

```bash
volclog --profile ram-tls-readonly doctor
volclog --profile ram-tls-readonly doctor --online
```

每个进程首次需要签名时调用 AssumeRole，请求有效期为 3600 秒。硬过期时间 `ExpiresAt` 取服务端返回的 `ExpiredTime` 与首次请求开始时间加一小时两者中较早的一个。取得的临时凭证只缓存在当前进程内，并在不晚于该硬过期前 60 秒重新获取。这些有效期不是用户可配置的 CLI 参数。

命令结束后，进程内临时凭证随进程退出而消失。删除配置：

```bash
volclog configure delete ram-tls-readonly
```

AssumeRole 失败时不会改用环境或 profile 中其他静态身份继续发送 TLS 请求。

## 8. OIDC

### 8.1 适用场景

- VKE Pod 使用投射的 ServiceAccount Token；
- CI 平台提供 OIDC Token 文件；
- 工作负载通过 OIDC 联邦身份扮演 RAM 角色；
- 不希望给工作负载分发长期 AK/SK。

### 8.2 前置条件

- OIDC 身份提供方已经在账号中建立；
- 目标角色的信任策略允许对应的 OIDC 身份；
- Token 的 issuer、audience、subject 等声明满足信任条件；
- 工作负载能够读取 Token 文件；
- 用户明确知道目标角色 TRN；
- 已确定 TLS Region 和 Endpoint。

### 8.3 配置

```bash
volclog configure set \
  --profile oidc-tls \
  --mode oidc \
  --role-trn 'trn:iam::<account-id>:role/<role-name>' \
  --oidc-token-file /var/run/secrets/oidc/token \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

OIDC 模式不需要配置 AK/SK。

### 8.4 Token 文件要求

`volclog` 在每次需要刷新 STS 凭证时重新解析并读取 Token 文件，因此支持由平台通过符号链接切换目标文件的轮换方式。

最终打开的 Token 文件必须：

- 是普通文件；
- 可以被当前进程读取；
- 内容非空；
- 不包含 NUL 字节；
- 大小不超过 64 KiB。

不要把 Token 内容写入命令行参数、日志或诊断输出。

### 8.5 验证、刷新和清理

离线检查 Token 文件路径和可访问性：

```bash
volclog --profile oidc-tls doctor
```

验证 OIDC 换取临时凭证并访问 TLS：

```bash
volclog --profile oidc-tls doctor --online
```

OIDC 换取凭证时请求 3660 秒有效期，实际硬过期时间以服务端返回的 `Expiration` 为准。临时凭证只缓存在当前进程内，并在不晚于硬过期前 60 秒刷新。

刷新失败、Token 文件不可读或信任关系不匹配时，TLS 请求失败关闭，不会尝试其他 AK/SK。

删除配置：

```bash
volclog configure delete oidc-tls
```

## 9. ECS Role

### 9.1 适用场景

- `volclog` 直接运行在 ECS 实例上；
- 实例已经绑定具备 TLS 权限的实例角色；
- 不希望在实例中保存长期 AK/SK；
- 运行环境允许访问实例元数据服务。

### 9.2 前置条件

- ECS 实例已经绑定目标实例角色；
- 用户明确知道角色名；
- 角色具备目标 TLS 资源权限；
- 实例可以访问固定元数据地址 `100.96.0.96`；
- 已确定 TLS Region 和 Endpoint。

不要在普通开发机或未绑定角色的容器中用 ECS Role 代替其他认证方式。

### 9.3 配置

```bash
volclog configure set \
  --profile ecs-tls \
  --mode ecsrole \
  --role-name '<ecs-role-name>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

ECS Role 不需要配置 AK/SK。

### 9.4 验证、刷新和清理

必须在目标 ECS 实例中执行真实验证：

```bash
volclog --profile ecs-tls doctor
volclog --profile ecs-tls doctor --online
```

获取凭证时：

- 访问固定实例元数据地址；
- 每次 credential refresh 都先获取新的 IMDSv2 Token；
- IMDSv2 Token 请求 TTL 为 21600 秒；
- 角色临时凭证的硬过期时间取自 `ExpiredTime`；
- 不晚于 `ExpiredTime` 前 5 分钟刷新；
- 临时凭证只缓存在当前进程内。

如果运行环境明确禁止访问实例元数据，可以设置：

```bash
export VOLCENGINE_ECS_METADATA_DISABLED=true
```

设置后 ECS Role 会在网络访问前失败关闭。

删除配置：

```bash
volclog configure delete ecs-tls
```

## 10. 缓存、刷新与过期行为

| 方式 | 临时状态保存位置 | 硬过期来源 | 刷新边界 | 跨进程复用 |
| --- | --- | --- | --- | --- |
| 静态 AK/SK | profile、secrets file 或环境变量 | 不适用 | 不自动刷新 | 取决于用户选择的保存方式 |
| 手工 STS | profile、secrets file 或环境变量 | 不适用 | 不自动刷新 | 取决于用户选择的保存方式 |
| Console Login | `login/cache/` | 缓存的 `IssuedAt` + `ExpiresIn` | 硬过期前 60 秒 | 是 |
| SSO | `sso/cache/` | 服务响应（OAuth `ExpiresAt` 和 STS `Expiration`） | OAuth 和 STS 均为硬过期前 60 秒 | 是 |
| RAM Role ARN | 当前进程内存 | 服务端 `ExpiredTime` 与请求开始加一小时两者中较早者 | 硬过期前 60 秒 | 否 |
| OIDC | 当前进程内存 | 服务端 `Expiration`（请求 3660 秒） | 硬过期前 60 秒 | 否 |
| ECS Role | 当前进程内存 | 服务端 `ExpiredTime` | `ExpiredTime` 前 5 分钟 | 否 |

Console Login、SSO、RAM Role ARN、OIDC 和 ECS Role 的硬过期值由签发服务或服务响应决定；它们不作为用户可配置的 CLI TTL 参数暴露。

### 10.1 State Root 和缓存目录

默认状态目录：

```text
~/.volclog/
├── config.json
├── login/
│   └── cache/
└── sso/
    └── cache/
```

`volclog` 独立管理这些状态。不要从其他工具或状态目录手工复制或复用鉴权缓存；缓存文件命名和生命周期属于 `volclog` 的内部实现。

如果设置了 `VOLCLOG_CONFIG`，state root 为该配置文件所在目录。

可以分别覆盖缓存目录：

```bash
export VOLCLOG_LOGIN_CACHE_DIRECTORY=/secure/path/login-cache
export VOLCLOG_SSO_CACHE_DIRECTORY=/secure/path/sso-cache
```

在 Unix 上，state root 和缓存目录以 `0700` 权限创建，缓存文件以 `0600` 权限写入，配置文件（`config.json`，或 `VOLCLOG_CONFIG` 指定的文件）——可能包含静态 AK/SK——以 `0600` 权限写入。这是存储凭证的 Unix 权限边界。不要把缓存目录或配置文件提交到版本控制，也不要在用户之间复制缓存文件。

### 10.2 失败关闭

SSO、Console Login、RAM Role ARN、OIDC 和 ECS Role 都遵循失败关闭：

- 选定的 Provider 无法取得有效凭证时，不发送 TLS 请求；
- 不自动切换为环境 AK/SK；
- 不使用 profile 中保留但当前模式未选中的静态字段；
- 过期边界到达后刷新失败时，不继续返回旧凭证。

这能避免配置错误时悄悄使用另一个身份访问 TLS。

### 10.3 `disable-ssl`

RAM Role ARN 和 OIDC 支持 `--disable-ssl=true`，但该选项会让 STS 鉴权请求使用 HTTP。使用时，将 `--disable-ssl=true` 追加到对应的完整 `configure set` 命令。

它不会改变 TLS 业务 Endpoint 自身的协议。启用后，鉴权材料可能通过明文网络传输，除非处于明确受控的可信网络，否则不要使用。

默认保持 HTTPS。

## 11. 常见问题

### 11.1 `profile not found`

检查 profile 名称和当前选择：

```bash
volclog configure list
volclog configure show --profile NAME
volclog configure use NAME
```

### 11.2 缺少 Region 或 Endpoint

工作负载模式应在 `configure set` 时显式写入 Region 和 Endpoint。动态登录 profile 如果没有保存这些值，可以给单条命令提供：

```bash
VOLCENGINE_REGION=cn-beijing \
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog --profile NAME doctor --online
```

### 11.3 `ReauthRequired`

Console Login：

```bash
volclog login --profile NAME
```

SSO：

```bash
volclog sso login --profile NAME
```

Workload 模式不会通过交互登录恢复。应修复源凭证、角色信任、OIDC Token 或 ECS 元数据访问。

### 11.4 `401 Unauthorized`

优先检查：

- 静态 AK/SK 是否正确；
- 手工 STS 三元组是否来自同一次签发；
- 临时凭证是否过期；
- Region 是否与签名和目标服务匹配；
- 当前实际使用的 profile 是否正确。

### 11.5 `403 Forbidden`

鉴权身份可能有效，但没有目标 TLS 资源权限。检查：

- RAM 用户或角色策略；
- AssumeRole 信任策略；
- OIDC 信任条件；
- ECS 实例角色策略；
- SSO 选择的账号和角色；
- 目标 Project、Topic 或其他资源的授权范围。

不要通过反复重试掩盖权限配置问题。

### 11.6 RAM Role AssumeRole 失败

分别检查两层权限：

1. 源身份是否允许调用 `sts:AssumeRole`；
2. 目标角色是否信任该源身份。

随后确认 `account-id` 和 `role-name` 完全正确，再执行：

```bash
volclog --profile NAME doctor --online
```

### 11.7 OIDC Token 文件不可用

运行离线检查：

```bash
volclog --profile NAME doctor
```

确认：

- 路径指向可以解析的普通文件；
- 当前进程拥有读取权限；
- 文件非空且不超过 64 KiB；
- 平台轮换 Token 后，符号链接目标仍然有效；
- Token 声明满足角色信任条件。

### 11.8 ECS 元数据访问失败

确认：

- 命令确实运行在目标 ECS 实例内；
- 实例已经绑定配置中的角色；
- `VOLCENGINE_ECS_METADATA_DISABLED` 没有被设置为 `true`；
- 网络策略允许访问 `100.96.0.96`；
- 角色名与实例绑定的角色一致。

### 11.9 离线 Doctor 成功，但在线 Doctor 失败

离线成功只说明本地配置看起来可用。继续检查在线结果中的：

- Endpoint URL 解析；
- DNS、TCP 和 TLS 连接；
- 代理环境；
- 凭证获取或刷新；
- TLS 服务返回的状态码和 Request ID；
- 当前身份是否具备 `DescribeProjects` 权限。

保留 Request ID 便于后续排查，但不要同时记录任何明文凭证。

## 12. 推荐的验收流程

每新增或切换一种鉴权方式，按以下顺序验收：

```bash
# 1. Confirm the current configuration and target profile
volclog configure show --profile NAME

# 2. Run only the local configuration check
volclog --profile NAME doctor

# 3. Verify real TLS read-only access
volclog --profile NAME doctor --online

# 4. Then run the target business command
volclog --profile NAME tool exec project.describe-projects
```

验收标准：

- 使用的是预期 profile 和预期鉴权模式；
- Region 与 Endpoint 正确；
- 能够取得有效签名凭证；
- `doctor --online` 成功；
- 身份只获得场景需要的最小 TLS 权限；
- 日志和命令输出中没有明文凭证；
- 登录过期后能够按对应方式刷新或恢复；
- Provider 失败时没有切换到其他身份。

---

[← 上一篇：快速开始](1-Getting-Started_zh.md) | [English](2-Authentication.md) | [下一篇：配置 →](3-Configuration_zh.md)
