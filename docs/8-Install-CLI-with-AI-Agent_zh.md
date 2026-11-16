# 使用 AI Agent 安装 TLS CLI

请在 AI Agent 后续调用 TLS CLI（`volclog`）的环境中完成安装。登录时由用户在浏览器中完成火山引擎授权。

## 环境要求

- 64 位 macOS、Linux 或 Windows，已安装 Node.js 18+（含 npm）。
- 安装环境可访问 npm 和 GitHub Releases。用户账号需具备目标日志服务资源的访问权限。

## 第 1 步 安装

```shell
npm install -g @volcengine-tls/volclog@latest --registry https://registry.npmjs.org/
volclog --version
```

## 第 2 步 登录

使用用户提供的 TLS Region 和 Endpoint，缺少时先向用户确认。选择专用配置档（profile），例如 `tls-cli-cn-beijing`。如果同名配置档已用于其他账号或任务，请选用新的名称。公网或私网 Endpoint 取决于 CLI 所在的网络。参见[日志服务地址](https://docs.volcengine.com/docs/torch-log-service/endpoint?lang=zh)。

替换下列占位符后，在用户可见、能持续运行的终端启动登录命令。实时显示标准错误（stderr），让用户在命令运行期间看到授权提示。

```shell
volclog login --profile "<profile>" --region "<region>" --endpoint "<endpoint>" --device-code --no-browser
```

终端会打印**授权链接、`Then enter the code:` 后的短期用户码和有效期**。提示出现后，立即告诉用户查看哪个终端，并请用户在官方授权页面输入用户码。保持原登录进程运行，直到命令返回结果。用户码仅在用户可见的终端展示。

如果 Agent 的终端对用户不可见，或无法在展示提示时保持进程运行，把填好参数的登录命令给用户，在后续使用 CLI 的同一台机器、同一系统账号和配置路径下用前台终端执行。登录失败或过期时，告知用户实际错误并参见[认证指南](2-Authentication_zh.md#54-device-code-与无浏览器登录)。

## 第 3 步 验证

登录命令成功退出后，用同一个 profile 执行在线只读检查。检查成功后再报告 TLS 访问验证通过。

```shell
volclog --profile "<profile>" doctor --online
```

## 第 4 步 安装 Skill

如果当前 Agent 支持 Skills，确认它实际读取的目录，再安装内置的 `volclog-core`。安装后读取该 Skill 的 `SKILL.md`，按其中的指引使用 CLI。

```shell
volclog skill install --name volclog-core --dir "<agent-skills-dir>"
```

完成后报告 CLI 版本、登录结果、在线验证结果和 Skill 安装目录。
