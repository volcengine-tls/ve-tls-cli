# Configure And Doctor

这个 reference 解决凭证和环境诊断问题。

## When To Run Doctor First

遇到这些情况，先跑 `volclog doctor`:

- 用户说“明明配了 AK/SK 但还是鉴权失败”
- endpoint / region 不确定
- profile 切换后行为异常
- 怀疑 `cred_ref` 没被正确解析

## What Doctor Checks

- 配置文件是否能加载
- 选中的 profile 是谁
- 凭证来源是环境变量、profile inline，还是 `cred_ref`
- region / endpoint / timeout 是否存在
- `--online` 场景下是否能真正拿解析后的凭证发请求

## Practical Rules

- `configure list` 适合看有哪些 profile 和凭证来源
- `doctor` 适合看当前实际生效配置
- 如果 profile 使用 `cred_ref`，要相信解析后的有效凭证状态，而不是只看 profile 里是否直接内联 AK/SK

## Escalation

如果 `doctor` 看起来正常但业务请求仍失败，再切到:

1. `volclog <group> <command> --describe`
2. `volclog --dry-run api <group> <action> ...`
3. 必要时再检查服务端报错 envelope
