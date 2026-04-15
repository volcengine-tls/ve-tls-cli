# Error Quick Reference

## 适用场景

- 想先做一轮快速分诊，而不是直接深挖单个 action
- 常见问题是鉴权失败、过滤不对、大结果刷屏、是否该切 explorer

## 必填输入

- 无固定业务参数
- 至少先明确问题属于配置、过滤、输出还是接口探索

## 可选参数触发词

- 说“鉴权失败”“配置不对”时，先转配置诊断
- 说“过滤结果不对”时，先检查 `--jmes-filter`
- 说“结果太大”“stdout 太长”时，先补 `--output-mode file`
- 说“不知道该不该切 explorer”时，先看当前 domain skill 是否已给入口

## 字段联动/限制

- 配置问题优先 `doctor`
- 过滤作用在原始结果根，不是 envelope 根
- 大结果优先落文件，避免模型在大对象里反复试错
- 只有 shortcut 不覆盖或用户明确指定底层 action 时，才切 explorer

## 常见误用

- 把配置问题误当接口问题
- 在 envelope 上写 `data.Total` 这种过滤路径
- 明明只是结果太大，却继续让 stdout 刷屏

## 下一步命令

```bash
volclog doctor
volclog configure list
volclog --output-mode file <command>
```
