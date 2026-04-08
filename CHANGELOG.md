# Changelog

## Unreleased

## volclog-v0.0.2

- Add `doctor` command for offline/online diagnostics.
- Add `--trace-dir` to generate redacted trace artifacts (JSONL).
- Add `--output-mode file` and `--output-file` to write output to disk.
- Add `--secrets-file` (dotenv) and `./.volclog/cli.config.json` project defaults.
- Add stdin input support via `-` for `file://`-style parameters.

## volclog-v0.0.1

- Initial public release.
- Resource management: Project/Topic/MetricTopic/Index.
- Query: SearchLogs and MetricTopic Prometheus-compatible endpoints.
- Structured output: JSON / JSONL.
