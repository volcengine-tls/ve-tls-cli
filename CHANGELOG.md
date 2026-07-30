# Changelog

## volclog-v1.1.1-rc.1

- Unify public tool and ordinary human CRUD execution around one generated Operation catalog and reusable Executor while preserving all 125 legacy tool contract digests.
- Derive capabilities and request templates from the canonical catalog, removing duplicated generated metadata and stale static templates.
- Separate runtime selector, authentication provider, TLS client, transport, and tracing responsibilities from CLI parsing and presentation.
- Remove unreachable command islands, enforce default and human quality gates, and harden typed-nil runtime boundaries without changing legacy AK/SK behavior.

## volclog-v1.0.5-rc.2

- Let ECS Role refresh use the caller's timeout budget so the documented retry policy can complete.
- Restore the default SSO scopes when older configuration contains an empty scope list.
- Normalize secure-store and documentation roots on macOS before enforcing path boundaries.
- Publish the Unix and Windows binary installers with each GitHub Release, reject downloaded checksum mismatches on Windows, and document checkout-free installation.

## volclog-v1.0.5-rc.1

- Add standalone SSO and Console Login (`mode=sso` / `mode=console-login`) with no runtime dependency on `ve`, `~/.volcengine`, or `volcengine-cli`.
- Add standalone workload providers for RAM Role ARN, OIDC, and ECS Role.
- Store SSO and Console Login token/STS caches in `0600` files under `<state-root>/sso/cache/` and `<state-root>/login/cache/`; cache roots can be overridden with `VOLCLOG_SSO_CACHE_DIRECTORY` / `VOLCLOG_LOGIN_CACHE_DIRECTORY`.
- Dynamic mode never falls back to static AK/SK on failure; `ReauthRequired` errors recover with `volclog login --profile NAME` or `volclog sso login --profile NAME`.
- Legacy AK/SK, environment variables, `--secrets-file`, `cred-ref`, and manual STS behaviors are unchanged.
- Add `volclog login [--profile NAME] [--remote]`, `volclog logout [--profile NAME|--all]`, `volclog configure sso-session`, `volclog configure sso`, `volclog sso login|logout`.
- Unify explicit TLS region, endpoint, and timeout configuration across authentication modes without deriving endpoints from regions.
- Reject runtime and context fields placed in `tool exec --input`, including sectioned input, instead of silently ignoring them.

## volclog-v1.0.0

- First stable release.
- CLI structure stabilised around shortcut / api / raw-api flows.
- Add Agent-oriented capabilities discovery, `--describe`, templates and `--dry-run`.
- Add `doctor`, `--trace-dir`, `--output-mode file`, `--output-file`, and `--secrets-file`.
- Add bundled skill installer and npm bootstrap entry.

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
