# Error Quick Reference

## 鉴权 / 配置

症状:

- 明明配置了凭证却仍失败
- endpoint / region 不确定
- profile 切换后行为异常

先做:

```bash
volclog doctor
volclog configure list
```

## 过滤结果不对

症状:

- 写了 `data.Total` 取不到值
- 结果结构和预期不一致

先做:

- 把过滤表达式改成原始结果根路径
- 例如写 `Total`，不要写 `data.Total`

## 大结果输出失控

症状:

- stdout 太大
- 模型在大对象里反复试错

先做:

```bash
volclog --output-mode file ...
```

## 不知道该不该切 explorer

先判断:

- 如果 domain skill 已有默认入口，不要切
- 只有 shortcut 不覆盖或用户明确指定 OpenAPI action 时才切
