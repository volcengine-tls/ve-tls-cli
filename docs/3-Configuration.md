# 3. Configuration

[← Previous: Authentication](2-Authentication.md) | [中文](3-Configuration_zh.md) | [Next: Usage →](4-Usage.md)

This guide covers how `volclog` resolves configuration: where files live, how profiles are selected, how `configure set` updates fields, how runtime values take precedence, and how the per-directory project config works. For provider-specific login, TTL, refresh, and logout behavior, see [Authentication](2-Authentication.md).

## 1. Configuration model and file locations

`volclog` keeps two kinds of local state: a single shared config file that holds profiles and shared credentials, and an optional per-directory project config that holds non-secret runtime defaults.

| File | Default location | Purpose | Permissions on creation |
| --- | --- | --- | --- |
| Config file | `$HOME/.volclog/config.json` | Profiles, shared credentials, SSO sessions, `current_profile` | dir `0700`, file `0600` |
| Project config | `.volclog/cli.config.json` (current working directory) | Non-secret runtime defaults for the current directory | dir `0700`, file `0600` |

The CLI requests `0700` directories and `0600` files when it creates new paths. Existing directories and files may retain their current permissions, because `MkdirAll`, `WriteFile`, and append-open do not repair an existing overly permissive mode. Inspect and tighten existing paths when needed.

The config file path can be overridden with the `VOLCLOG_CONFIG` environment variable:

```bash
VOLCLOG_CONFIG=/path/to/config.json volclog configure list
```

The project config is loaded from the current working directory only. The CLI does **not** traverse parent directories looking for `.volclog/cli.config.json`; if the file is absent in the working directory, no project config is applied.

The project config must never contain credentials. If any of `access_key_id`, `secret_access_key`, `security_token`, `ak`, `sk`, or `token` appear as top-level keys, loading fails with `project config must not contain credentials`.

## 2. Profile selection and management

A profile stores one identity together with its TLS runtime configuration. Business commands select a profile in this order:

1. The `--profile NAME` explicitly passed on the command;
2. The `current_profile` in the configuration file;
3. The profile named `default`.

### 2.1 List, show, use, and delete

```bash
volclog configure list
volclog configure list --prefix prod
volclog configure show --profile default
volclog configure use prod
volclog configure delete --profile scratch
volclog configure delete --prefix temp- --yes
```

`configure list` and `configure show` never print secrets. The access key ID is masked (for example `AKT****XYZ`); the secret access key is omitted entirely. `configure show --profile NAME` shows one profile; without `--profile` it falls back to `current_profile`, then `default`.

`configure use NAME` sets `current_profile` to `NAME`. `configure delete` removes a single profile by name, or every profile whose name starts with `--prefix` (prefix deletion requires `--yes`). After deletion, `current_profile` is adjusted if it pointed at a removed profile.

The `configure profile` subcommand group is a set of aliases:

| Alias | Equivalent |
| --- | --- |
| `configure profile add NAME [flags...]` | `configure set --profile NAME [flags...]` |
| `configure profile use NAME` | `configure use NAME` |
| `configure profile show [NAME]` | `configure show --profile NAME` |
| `configure profile list [args...]` | `configure list [args...]` |
| `configure profile delete [NAME]` | `configure delete [NAME]` |

### 2.2 Verify explicitly

During verification, prefer passing `--profile` explicitly on each command rather than switching `current_profile`, so a mistaken `configure use` cannot silently change the identity of subsequent commands:

```bash
volclog --profile default doctor
volclog --profile default tool exec project.describe-projects
```

## 3. Profile fields and mode-update semantics

A profile can carry auth-mode fields (AK/SK, SSO session binding, OIDC token file, and so on) together with common runtime fields: `region`, `endpoint`, and `timeout_seconds`.

The `disable-ssl` field is not a general runtime toggle. It applies only to RAM Role ARN and OIDC STS credential-exchange requests: when `true`, those authentication materials are sent over plaintext HTTP to the fixed STS host. It does not change the TLS business endpoint scheme. Other modes (static AK/SK, SSO, Console Login, ECS Role) do not use it for their credential exchange. Avoid it on untrusted networks; see [Authentication](2-Authentication.md) for details.

### 3.1 Omitting `--mode`

For an existing profile, when the command contains only `--profile` plus one or more of `--region`, `--endpoint`, and `--timeout-seconds`, `configure set` patches only those runtime fields. It does not change the auth mode, identity fields, login binding, or credential TTL:

```bash
volclog configure set --profile NAME \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

Other invocations without `--mode` take the legacy static path. That path sets the profile mode to `ak`, overwrites the standard static fields, and requires `--ak --sk` (or `--cred-ref`), `--region`, and `--endpoint`.

Because omitted flags are treated as empty, re-running `configure set` without `--token` clears any previously stored `security_token`. This path exists to preserve the original static AK/SK behavior exactly.

### 3.2 Supplying `--mode` (explicit patch path)

When `--mode` is supplied, `configure set` loads the existing profile and **patches only the flags explicitly provided**. Fields you do not mention are left untouched. Validation covers the selected mode's identity requirements; TLS region and endpoint may be supplied later through the runtime-only patch, environment, or per-command flags.

`--mode sso` and `--mode console-login` are not accepted by `configure set`; those modes use the dedicated `login` and `configure sso` flows described in [Authentication](2-Authentication.md).

### 3.3 Dynamic providers and dormant fields

For Console Login and SSO, the dedicated flows patch the login bindings while any dormant static fields can remain in the profile. Fail-closed dynamic providers (SSO, Console Login, RAM Role ARN, OIDC, ECS Role) never use dormant static fields: when the profile mode is a provider mode, environment AK/SK are ignored and the provider is constructed from its own bindings. If the provider cannot retrieve credentials, the request fails closed rather than falling back to static AK/SK.

### 3.4 Cleanup and storage guidance

There is no field-level scrub command. To remove dormant static fields from a profile, delete and recreate the profile, or manage the config file with an approved secure configuration tool. Before deleting a shared credential reference (see section 4), confirm no other profile references it.

## 4. Shared credential references

A profile can reference a shared credential by name with `--cred-ref`. The credential is stored once under `creds` in the config file and reused by every profile that references it. `region` and `endpoint` remain profile-specific and are not inherited from the credential.

There is no independent credential create command. To create or update a shared credential, use `--cred-ref` together with complete `--ak` and `--sk` on a profile:

```bash
volclog configure set --profile app \
  --cred-ref shared-creds \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

A second profile can then reuse the same credential by referencing only `--cred-ref`, supplying its own `region` and `endpoint`:

```bash
volclog configure set --profile app-backup \
  --cred-ref shared-creds \
  --region cn-shanghai \
  --endpoint https://tls-cn-shanghai.volces.com
```

When `--cred-ref` is used together with `--ak --sk`, the supplied AK/SK are written into the named credential entry (creating or updating it), and the profile stores only the reference.

To delete a shared credential:

```bash
volclog configure cred delete shared-creds
```

`configure cred delete` first checks whether any profile references the credential. If profiles still reference it, the command aborts with `credential in use by profiles: <names>` and the credential is not removed. Reassign or delete those profiles first, then delete the credential.

## 5. Runtime precedence

Runtime values are resolved differently depending on whether the selected profile uses static AK/SK or a dynamic provider. Global `--region` and `--endpoint` are explicit one-command overrides and are never persisted. `context.region` and `context.endpoint` are only available through `tool`/`workflow` context and act as fallbacks ahead of project configuration. Endpoint is never derived from region.

### 5.1 Static mode

In static mode (`mode: ak`), when a complete set of environment AK/SK is present (`VOLCENGINE_ACCESS_KEY_ID` and `VOLCENGINE_ACCESS_KEY_SECRET`), static resolution constructs the identity from the environment values and **bypasses the selected profile entirely**. Region/endpoint precedence is: explicit CLI > environment > context default > project default. Timeout precedence is: project default > `60` seconds. If neither environment nor a fallback supplies the required region/endpoint, resolution fails.

Without a complete environment AK/SK set, the selected profile is used. Region/endpoint precedence is: explicit CLI > profile > context default > project default. Region/endpoint environment variables alone do not alter this legacy path. Timeout precedence is: profile > project default > `60` seconds.

Context has no timeout field.

### 5.2 Dynamic provider mode

For dynamic provider modes (SSO, Console Login, RAM Role ARN, OIDC, ECS Role), environment AK/SK are intentionally ignored. Region/endpoint precedence is: explicit CLI > environment region/endpoint > profile > context default > project default.

Timeout precedence is: profile > project default > `60` seconds. There is no global or context timeout override.

### 5.3 Runtime selector conflicts

An explicit selector is optional. With none, normal profile selection (`--profile` → `current_profile` → `default`) or static environment resolution applies.

The conflict rules are:

- Any profile selector (global `--profile` or `context.profile`) combined with any secrets-file selector (global `--secrets-file` or `context.secrets_file`) conflicts.
- Global `--secrets-file` combined with `context.secrets_file` conflicts.
- Global `--profile` combined with `context.profile` conflicts only when the profile names differ. Repeating the same profile name in both places is accepted but redundant; avoid it for clarity.

A conflict fails with `conflicting runtime selectors` (or `conflicting profile selectors` when two different profile names are supplied).

`--secrets-file` is rejected for `login`, `logout`, `sso`, and `configure sso`, which manage their own dynamic identity.

### 5.4 Secrets file parsing and scope

`--secrets-file` reads a dotenv-style file and applies only supported `VOLCENGINE_*` assignments to the process environment:

- `VOLCENGINE_ACCESS_KEY_ID`
- `VOLCENGINE_ACCESS_KEY_SECRET`
- `VOLCENGINE_TOKEN`
- `VOLCENGINE_REGION`
- `VOLCENGINE_ENDPOINT`

Other keys are ignored. Lines starting with `#` are comments, and the `export ` prefix is accepted. The file must contain at least one supported assignment, otherwise loading fails.

Secrets file values are process-scoped. A secrets file can be prepared with restricted permissions and passed to a single command. The example below works when `$HOME/.volclog` does not exist: `umask 077` causes any newly created regular file to be `0600`, the directory is explicitly set to `0700`, and the explicit `chmod 600` ensures that rerunning the example also repairs an existing overly permissive file:

```bash
secrets_dir="$HOME/.volclog"
(
  umask 077
  mkdir -p "$secrets_dir"
  chmod 700 "$secrets_dir"
  cat > "$secrets_dir/secrets.env" <<'EOF'
VOLCENGINE_ACCESS_KEY_ID='<access-key-id>'
VOLCENGINE_ACCESS_KEY_SECRET='<secret-access-key>'
VOLCENGINE_REGION=cn-beijing
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
EOF
  chmod 600 "$secrets_dir/secrets.env"
)
volclog --secrets-file "$secrets_dir/secrets.env" doctor --online
```

The same values can also be supplied inline for a single process without a file:

```bash
VOLCENGINE_ACCESS_KEY_ID='<access-key-id>' \
VOLCENGINE_ACCESS_KEY_SECRET='<secret-access-key>' \
VOLCENGINE_REGION=cn-beijing \
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog tool exec project.describe-projects
```

## 6. Project configuration

The project config (`.volclog/cli.config.json` in the current working directory) provides non-secret runtime defaults. The project config applies only in the current working directory.

The `--output`, `--output-mode`, and `--trace-redact` global flags override their corresponding project defaults when explicitly passed. Other global flags do not override project defaults.

The fields currently consumed by the runtime are:

| Field | Applied as | Notes |
| --- | --- | --- |
| `output` | Default for `--output` | Used when `--output` is not passed |
| `output_mode` | Default for `--output-mode` | Used when `--output-mode` is not passed |
| `trace_redact` | Default for `--trace-redact` | Used when `--trace-redact` is not passed |
| `region` | Fallback default for region | Used when the profile (and context default) has no region |
| `endpoint` | Fallback default for endpoint | Used when the profile (and context default) has no endpoint |
| `timeout_seconds` | Fallback default for timeout | Used when the profile has no timeout |

`region` and `endpoint` may be present in the project config file and are consumed as fallback defaults, but `configure project set` does not expose flags to set them. `output_dir` and `hints_file` are accepted by `configure project set` and stored in the file, but the current runtime does not apply them. They can be set and inspected, but they have no effect on command execution today.

```bash
volclog configure project set --output json --output-mode stdout --trace-redact on
volclog configure project show
```

`configure project set` accepts `--output`, `--output-mode`, `--output-dir`, `--timeout-seconds`, `--trace-redact`, and `--hints-file`. It does not accept `--region` or `--endpoint`; those are profile fields managed with `configure set`.

## 7. Output and trace defaults

Output and trace are controlled by global flags. Detailed execution semantics belong in [Usage](4-Usage.md); this section lists the selectors and their defaults.

| Flag | Values | Default |
| --- | --- | --- |
| `--output` | `json`, `jsonl` (default `volclog`); `table` (`volclog-human` only) | `json` |
| `--output-mode` | `stdout`, `file` | `stdout` |
| `--output-dir` | directory path | none |
| `--output-file` | file path | none |
| `--jmes-filter` | JMESPath expression | none |
| `--trace-dir` | directory path | none (tracing off) |
| `--trace-redact` | `on`, `off` (and aliases) | `on` |

On the default `volclog` agent path, `--output table` is rejected; use `--output json` or `--output jsonl`. The `table` format is supported only by `volclog-human` and only for specific shortcut surfaces: `project`/`topic`/`metric-topic` list and get, `index get`, and `log search`. Not every human shortcut supports table.

`--trace-redact` accepts `on`, `true`, `1`, `yes`, `enabled`, `strict`, `default` (all map to `on`) and `off`, `false`, `0`, `no`, `disabled` (all map to `off`). Any unrecognized value defaults to `on`.

Current trace writing always keeps structured header and query fields to keys and bodies to hashes. `--trace-redact off` is accepted and normalized but does not disable that forced structured-field redaction and does not emit raw header, query, or body values. The transport `error_message` field remains the separate sensitive exception: it stores the transport error string directly, which can include a URL and query values.

These global flags can be placed before the command group or, for `raw`, `tool`, `workflow`, `project`, `topic`, `metric-topic`, `index`, `log`, `host-group`, and `collector`, after the group as trailing globals. `--profile` and `--secrets-file` are leading-only and cannot be used as trailing globals.

`login`, `logout`, `sso`, and `configure sso` always emit their exact JSON result shape to stdout. `--output jsonl|table` may parse but does not change the frozen JSON shape (JSON is forced). The following are rejected before any auth side effect runs: non-`stdout` `--output-mode`, `--output-file`, `--jmes-filter`, `--trace-dir`, and `--secrets-file`. `--output-dir` alone and `--trace-redact` without `--trace-dir` do not divert the frozen result and are not rejected.

## 8. Safe inspection, cleanup, and examples

### 8.1 Inspect without leaking secrets

`configure list`, `configure show`, and `doctor` never print secrets. Use them to verify configuration and credentials:

```bash
volclog configure list
volclog configure show --profile default
volclog --profile default doctor
volclog --profile default doctor --online
```

### 8.2 Cleanup boundaries

To remove a profile or a shared credential, use the dedicated delete commands. There is no broad, destructive cleanup command. Deleting a profile removes only that profile; shared credentials referenced by other profiles are untouched. Deleting a shared credential is blocked while any profile references it.

For dormant field cleanup, the boundary is the same as in [Authentication](2-Authentication.md): delete and recreate the profile, or manage the config file with an approved secure configuration tool. Do not hand-edit the config file while the CLI is running.

---

[← Previous: Authentication](2-Authentication.md) | [中文](3-Configuration_zh.md) | [Next: Usage →](4-Usage.md)
