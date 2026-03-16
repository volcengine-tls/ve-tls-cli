# tlsctl-runner

本目录用于承载面向公众用户的本地可执行 skills runner 设计与实现（规划）。

当前内容：
- 规格说明：[spec.md](./spec.md)
- skill 说明：[SKILL.md](./SKILL.md)

定位：
- stdin 输入 JSON，请求包含 `account + region + action + args`
- 或通过 `--text` 输入“中文 + 参数键值”的半结构化请求
- runner 负责选择本地 profile（或提示用户配置），并调用 `tlsctl` 执行
- 输出 stdout JSON，便于 Trae/IDE 智能体或脚本消费
