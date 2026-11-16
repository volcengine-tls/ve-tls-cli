# 8. Install TLS CLI with an AI Agent

[← Previous: Human Shortcuts](7-Human-Shortcuts.md) | [中文](8-Install-CLI-with-AI-Agent_zh.md) | [Back to README →](../README.md)

Install TLS CLI (`volclog`) in the environment where the AI agent will run it. During sign-in, the user completes Volcengine authorization in a browser.

## Prerequisites

- A 64-bit macOS, Linux, or Windows environment with Node.js 18+ and npm.
- Access to npm and GitHub Releases from the installation environment. The user's account needs access to the target TLS resources.

## Step 1. Install

```shell
npm install -g @volcengine-tls/volclog@latest --registry https://registry.npmjs.org/
volclog --version
```

## Step 2. Sign in

Use the TLS Region and Endpoint provided by the user. Ask for either value if it is missing. Choose a dedicated profile such as `tls-cli-cn-beijing`. If another account or task already uses that name, choose a new one. Use the public or private Endpoint that the CLI can reach. See [TLS service endpoints](https://docs.volcengine.com/docs/torch-log-service/endpoint?lang=en).

Fill in the placeholders, then run the sign-in command in a terminal that can keep running and show stderr to the user as it arrives.

```shell
volclog login --profile "<profile>" --region "<region>" --endpoint "<endpoint>" --device-code --no-browser
```

The terminal prints **the authorization link, the short-lived user code after `Then enter the code:`, and the validity period**. As soon as the prompt appears, tell the user which terminal to check and ask them to enter the code on the official authorization page. Keep the original process running until the command exits. Show the user code only in the user-visible terminal.

If the user cannot see the agent's terminal or it cannot keep running while showing the prompt, give the user the filled-in command to run in a foreground terminal. Use the same machine, system account, and configuration path that the CLI will use later. If sign-in fails or expires, report the error. See [Authentication](2-Authentication.md#54-device-code-and-no-browser-login).

## Step 3. Verify access

After sign-in succeeds, run the online read-only check with the same profile. Report TLS access as verified when the check succeeds.

```shell
volclog --profile "<profile>" doctor --online
```

## Step 4. Install the Skill

If the agent supports Skills, find the directory it uses and install the bundled `volclog-core` Skill. Read its `SKILL.md` and follow it when using the CLI.

```shell
volclog skill install --name volclog-core --dir "<agent-skills-dir>"
```

Report the CLI version, sign-in and online verification results, and the Skill installation path.

---

[← Previous: Human Shortcuts](7-Human-Shortcuts.md) | [中文](8-Install-CLI-with-AI-Agent_zh.md) | [Back to README →](../README.md)
