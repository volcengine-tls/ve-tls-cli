# ve-tls-cli 安装指南

请按以下步骤安装并配置火山引擎日志服务 CLI。部分步骤需要用户在浏览器中配合完成。

## 环境要求
- Node.js 18+（含 npm）

## 第 1 步 安装
```shell
npm install -g @volcengine-tls/volclog@latest --registry https://registry.npmjs.org/
volclog --version
```

## 第 2 步 登录
确认用户的 TLS Region 和 Endpoint，替换下面的占位符；用户已经提供时直接使用。

Agent 应以后台任务或可轮询会话启动命令，实时读取 stdout 和 stderr，不要等待命令退出后才展示授权信息。
```shell
volclog login --profile tls-cli \
  --region '<region>' \
  --endpoint '<endpoint>' \
  --device-code --no-browser
```

收到授权提示后，立即在对话中展示以下信息：
- **授权链接**：命令实际输出的 URL，展示为可点击链接。
- **用户授权码**：`Then enter the code:` 后的内容，使用代码格式展示，方便复制。
- **有效期**：命令实际输出的有效秒数。

请用户打开链接，在浏览器中输入用户授权码并完成授权，不要让用户自行寻找终端输出。

保持登录进程运行并持续轮询。以登录命令成功退出为准，再进入验证步骤；如果失败或过期，及时展示错误和当前状态。

## 第 3 步 验证
执行在线只读检查，验证当前身份能否访问 TLS：
```shell
volclog --profile tls-cli doctor --online
```
检查成功后报告验证通过；失败时保留错误信息，说明尚未完成验证。

## 第 4 步 安装 Skill
确认当前 Agent 实际使用的 Skill 目录，替换下面的占位符：
```shell
volclog skill install --name volclog-core --dir '<agent-skills-dir>'
```
安装后读取该 Skill 的 SKILL.md，并按照其中的指引使用 CLI。

完成后简要报告 CLI 版本、登录结果、在线验证结果和 Skill 安装目录。
