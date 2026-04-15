# Configure And Doctor

## 适用场景

- 凭证、profile、region、endpoint 看起来不对
- 用户说“明明配了 AK/SK 但还是鉴权失败”
- 怀疑 `cred_ref`、环境变量或 profile 切换有问题

## 必填输入

- 无固定业务参数
- 需要知道当前想检查的 profile 或环境

## 可选参数触发词

- 说“看有哪些 profile”时，先跑 `configure list`
- 说“检查当前实际生效配置”时，先跑 `doctor`
- 说“需要真实联机验证”时，再考虑 `doctor --online`

## 字段联动/限制

- `configure list` 用于看有哪些 profile 和凭证来源
- `doctor` 用于看当前实际生效配置
- profile 使用 `cred_ref` 时，要看解析后的有效凭证状态，不要只看 profile 是否直接内联 AK/SK
- 先确认配置层，再判断是不是业务接口本身的问题

## 常见误用

- 业务请求失败后直接改 API 参数，不先查配置
- 只看 profile 文件，不看实际生效配置
- 配置问题还没定位清楚就先退到 `api call`

## 下一步命令

```bash
volclog configure list
volclog doctor
```
