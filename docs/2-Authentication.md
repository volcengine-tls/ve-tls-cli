# 2. Authentication

[← Previous: Getting Started](1-Getting-Started.md) | [中文](2-Authentication_zh.md) | [Next: Configuration →](3-Configuration.md)

This guide describes every authentication method supported by `volclog`, and how to choose, configure, verify, refresh, and clean up login state for each.

Regardless of the method, the goal is always the same: obtain a set of credentials that can sign TLS requests. The methods differ in who supplies the credentials, whether interactive login is required, whether credentials refresh automatically, whether they are written to a local cache, and whether they suit a personal terminal or a workload.

## 1. How to choose an authentication method

| Method | Recommended scenario | User must supply | Storage and refresh |
| --- | --- | --- | --- |
| Static AK/SK | Compatible with existing setups, fixed local identity | AK, SK | Saved to a profile, or injected via environment variables / secrets file; not rotated automatically |
| Manual STS | Temporary credentials already obtained from another system | Temporary AK, temporary SK, Session Token | Injected by the user; a fresh full triple must be obtained after expiry |
| Console Login | Personal terminal, no long-term AK/SK, willing to authorize through the console | Browser login authorization | Login state written to a local secure cache; refreshed automatically near expiry |
| SSO | Enterprise unified identity; accounts and roles assigned by SSO | SSO Start URL, account and role | OAuth and STS state written to a local secure cache; refreshed automatically near expiry |
| RAM Role ARN | Use an existing identity to assume a role in a target account | Source AK/SK, target account ID, role name | Temporary STS cached only within the current process; re-AssumeRole automatically near expiry |
| OIDC | VKE, CI, or other workloads that provide an OIDC Token | OIDC Token file, role TRN | Token file re-read on every refresh; temporary STS cached only within the current process |
| ECS Role | Running on an ECS instance that has an instance role attached | ECS role name | Obtained through the instance metadata service; temporary credentials cached only within the current process |

Simple selection guidance:

- You already have stable AK/SK and want to keep using them the same way: keep using static AK/SK.
- You already have a short-lived AK/SK/Session Token triple: use manual STS.
- You are an individual working interactively on a local or remote terminal: prefer Console Login.
- Your enterprise assigns accounts and roles through a unified identity portal: use SSO.
- You need to switch from one RAM identity to another account or role: use RAM Role ARN.
- Your workload can obtain an OIDC Token: use OIDC.
- Your program runs directly on an ECS instance with an attached instance role: use ECS Role.

Do not guess account IDs, role names, Start URLs, token paths, regions, or endpoints from examples. These values must be provided by the user or the environment administrator.

## 2. Understand profiles before you start

A profile stores one identity together with its TLS runtime configuration. Business commands select a profile in this order:

1. The `--profile NAME` explicitly passed on the command;
2. The `current_profile` in the configuration file;
3. The profile named `default`.

View and switch profiles:

```bash
volclog configure list
volclog configure show --profile NAME
volclog configure use NAME
```

You can also make a single command use a specific profile:

```bash
volclog --profile NAME doctor
volclog --profile NAME tool exec project.describe-projects
```

Unless you genuinely want to change the default identity for subsequent commands, always pass `--profile` explicitly during the verification phase.

### 2.1 Region and endpoint

Every TLS request must be able to determine a region and an endpoint. Configuration example:

```bash
--region cn-beijing \
--endpoint https://tls-cn-beijing.volces.com
```

For SSO, Console Login, RAM Role ARN, OIDC, and ECS Role, the runtime configuration is resolved in this order:

1. `VOLCENGINE_REGION`, `VOLCENGINE_ENDPOINT`;
2. The `region` and `endpoint` in the selected profile;
3. The `context.region` and `context.endpoint` defaults for the current `tool` or `workflow` execution;
4. The project configuration in the current directory;
5. When timeout is not configured, 60 seconds is used.

If a dynamic-login profile does not carry TLS runtime configuration, you can supply the environment variables explicitly for a single command:

```bash
VOLCENGINE_REGION=cn-beijing \
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog --profile NAME doctor --online
```

Execution-context defaults apply only to `tool` and `workflow` commands and do not override non-empty environment or profile values. If the current directory already has a project configuration, its region and endpoint can also be used. Project configuration follows the working directory; when you run commands outside that directory, make sure the profile, execution context, or environment variables still provide the correct values. See [Configuration](3-Configuration.md#5-runtime-precedence) for the complete static and dynamic precedence rules.

In static mode, when a complete set of environment AK/SK is present, those environment credentials take precedence over the profile; the corresponding region and endpoint can be supplied through environment variables at the same time. To avoid mixing identity and runtime environment, production calls should use one complete, explicit configuration set.

### 2.2 Configuration checks and live access verification

The offline check does not proactively request workload temporary credentials just to verify:

```bash
volclog --profile NAME doctor
```

It is suitable for checking:

- whether the profile exists;
- whether the auth mode and required fields are complete;
- whether region and endpoint can be resolved;
- whether the OIDC Token file is accessible;
- whether the login cache exists or needs refresh;
- whether any insecure authentication options are enabled.

For live verification, use:

```bash
volclog --profile NAME doctor --online
```

`doctor --online` performs network checks and sends a minimal read-only TLS request. Only a successful online check proves that the current identity can actually access TLS.

You can also run a read-only command directly:

```bash
volclog --profile NAME tool exec project.describe-projects
```

Never print real AK, SK, Session Token, OAuth Token, or OIDC Token during verification.

## 3. Static AK/SK

### 3.1 When to use

- Compatible with existing AK/SK usage;
- A fixed identity used locally over a long period;
- An automation environment that already has a secure secret injection mechanism;
- No need for automatic login or rotation for now.

### 3.2 Write to a profile

```bash
volclog configure set \
  --profile default \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

`--mode ak` can be supplied explicitly, or omitted:

```bash
volclog configure set \
  --profile prod \
  --mode ak \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

Omitting `--mode` preserves the legacy static AK/SK behavior exactly. Adding new providers does not change existing AK/SK access semantics.

If there is no current profile yet, the first successfully configured profile becomes the current one. When a current profile already exists, configuring another profile does not switch automatically; switch on demand:

```bash
volclog configure use prod
```

### 3.3 Use a shared credential reference

When the same AK/SK needs to access multiple regions or endpoints, you can save the credentials as a shared reference:

```bash
volclog configure set \
  --profile tls-bj \
  --cred-ref shared-account \
  --ak '<access-key-id>' \
  --sk '<secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

Subsequent profiles only reference that credential:

```bash
volclog configure set \
  --profile tls-sg \
  --cred-ref shared-account \
  --region ap-southeast-1 \
  --endpoint https://tls-ap-southeast-1.volces.com
```

### 3.4 Use a secrets file

For CI, agents, or one-off execution, prefer a permission-controlled secrets file instead of writing credentials into a profile.

For example `/secure/path/volclog.env`:

```dotenv
VOLCENGINE_ACCESS_KEY_ID=<access-key-id>
VOLCENGINE_ACCESS_KEY_SECRET=<secret-access-key>
VOLCENGINE_REGION=cn-beijing
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
```

Restrict permissions and use it explicitly:

```bash
chmod 600 /secure/path/volclog.env
volclog --secrets-file /secure/path/volclog.env doctor --online
```

`--profile` and `--secrets-file` are mutually exclusive runtime selectors; do not use both in the same command.

### 3.5 Use environment variables

```bash
export VOLCENGINE_ACCESS_KEY_ID='<access-key-id>'
export VOLCENGINE_ACCESS_KEY_SECRET='<secret-access-key>'
export VOLCENGINE_REGION='cn-beijing'
export VOLCENGINE_ENDPOINT='https://tls-cn-beijing.volces.com'

volclog doctor --online
```

Environment variables should only be injected into processes that genuinely need them. Avoid echoing secrets in shared shells, build logs, or debug output.

### 3.6 Verify and clean up

```bash
volclog --profile prod doctor
volclog --profile prod doctor --online
```

Delete a profile you no longer use:

```bash
volclog configure delete prod
```

Before deleting a shared credential, confirm that no other profile still references it:

```bash
volclog configure cred delete shared-account
```

Static AK/SK is not rotated automatically. When a key is invalidated or disabled, the user must update the profile, secrets file, or environment variables.

## 4. Manual STS temporary credentials

### 4.1 When to use

Use manual STS when the user has already obtained temporary credentials from another authorization system, but does not need `volclog` to request or refresh them.

A valid STS credential triple must contain all of:

- A temporary Access Key ID;
- A temporary Secret Access Key;
- A Session Token.

All three must come from the same issuance; do not mix a Session Token with a different AK/SK pair.

### 4.2 Write to a temporary profile

```bash
volclog configure set \
  --profile temp-sts \
  --ak '<temporary-access-key-id>' \
  --sk '<temporary-secret-access-key>' \
  --token '<session-token>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

Verify:

```bash
volclog --profile temp-sts doctor --online
```

This method saves the temporary triple to the profile. After the credentials expire, obtain a new triple and overwrite it.

### 4.3 One-off injection

Secrets file:

```dotenv
VOLCENGINE_ACCESS_KEY_ID=<temporary-access-key-id>
VOLCENGINE_ACCESS_KEY_SECRET=<temporary-secret-access-key>
VOLCENGINE_TOKEN=<session-token>
VOLCENGINE_REGION=cn-beijing
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com
```

```bash
chmod 600 /secure/path/volclog-sts.env
volclog --secrets-file /secure/path/volclog-sts.env doctor --online
```

Or inject for a single command only:

```bash
VOLCENGINE_ACCESS_KEY_ID='<temporary-access-key-id>' \
VOLCENGINE_ACCESS_KEY_SECRET='<temporary-secret-access-key>' \
VOLCENGINE_TOKEN='<session-token>' \
VOLCENGINE_REGION='cn-beijing' \
VOLCENGINE_ENDPOINT='https://tls-cn-beijing.volces.com' \
volclog tool exec project.describe-projects
```

`volclog` does not renew manual STS credentials. After expiry, you must obtain a new complete triple from the issuer.

## 5. Console Login

### 5.1 When to use

- An individual working interactively on a local terminal;
- Does not want to configure long-term AK/SK;
- Can complete console authorization through a browser;
- Working on a remote development machine, but can complete cross-device authorization in a local browser.

### 5.2 Prerequisites

- Confirm the user account has the target TLS resource permissions;
- Know the TLS region and endpoint;
- In remote mode, copy the authorization URL shown by the terminal to a local browser, then paste the authorization code back into the terminal.

`login --region` writes the region into the target profile, but it does not guess the TLS endpoint for the user. If the target profile originally has no endpoint, supply it explicitly for verification and business commands:

```bash
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog --profile console-dev doctor --online
```

### 5.3 Local browser login

```bash
volclog login \
  --profile console-dev \
  --region cn-beijing
```

Local mode uses a loopback callback to receive the browser authorization result. After a successful login, the profile switches to `mode=console-login` and stores the login session binding. The login flow patches only the mode, login-session, and region fields; any existing static `AccessKeyID`, `SecretAccessKey`, `SecurityToken`, and `CredRef` remain stored on disk as dormant fields. The Console Login provider does not use them, and the provider path is fail-closed, but the dormant values are still present in the configuration file.

If no region is specified explicitly, the profile's existing region is used first; if that is also empty, `cn-beijing` is used.

The optional `--endpoint-url` flag overrides the Console authorization/token service endpoint. This is the Console sign-in endpoint, not the TLS business endpoint. It must be a clean HTTPS URL (no userinfo, query, fragment, or non-root path) and must be supplied by the user or administrator; do not invent a value.

```bash
volclog login \
  --profile console-dev \
  --region cn-beijing \
  --endpoint-url 'https://<console-sign-in-host>'
```

### 5.4 Remote or cross-device login

Use this when the development machine cannot display a browser page directly:

```bash
volclog login \
  --profile console-dev \
  --region cn-beijing \
  --remote
```

Follow the terminal prompts:

1. Open the authorization URL on a device that can display the page;
2. Complete login and authorization;
3. Paste the authorization code back into the terminal running `volclog`.

### 5.5 Use and verify

```bash
volclog configure use console-dev
volclog --profile console-dev doctor
volclog --profile console-dev doctor --online
```

A successful login does not mean the target TLS permissions are already granted. Only a successful `doctor --online` confirms that the current temporary identity can complete TLS read-only access.

### 5.6 Refresh and re-login

Console Login hard expiry is derived from the cached token's `IssuedAt` plus `ExpiresIn` (the lifetime returned by the Console service). When the temporary credentials are within 60 seconds of that hard expiry, business commands attempt to use the cached refresh material to automatically obtain new credentials. These lifetimes are set by the Console service and are not user-configurable CLI parameters.

Re-login is required when:

- The local cache is missing, corrupt, or has been cleaned up;
- The refresh material is missing, expired, or revoked;
- The server returns an error that cannot continue silent refresh;
- The CLI returns `ReauthRequired`.

Recovery command:

```bash
volclog login --profile console-dev --region cn-beijing
```

Business commands never open a browser in the background.

### 5.7 Log out

Console logout is session-scoped. `volclog logout --profile NAME` uses the named profile only to resolve its `login-session`; it then deletes that session's cache and clears the `login-session` binding on every console-login profile still bound to that same session, not only the named profile. If multiple profiles share a login session, logging out one of them clears the shared session for all of them.

Log out of a specific profile (resolves and clears its session):

```bash
volclog logout --profile console-dev
```

`logout --all` iterates every known console-login profile, groups them by login-session, and clears each session.

```bash
volclog logout --all
```

Logging out deletes the local login cache and clears the login-session binding on affected profiles, but does not delete the profiles or the TLS runtime configuration retained in them. It also does not remove any dormant static `AccessKeyID`, `SecretAccessKey`, `SecurityToken`, or `CredRef` fields that may remain from a previous static configuration.

There is currently no dedicated field-level CLI command that scrubs only dormant static fields. If your security policy requires physical removal of these values, do not assume that `logout` is sufficient. Preserve the non-secret runtime values you still need (region, endpoint, timeout), then either delete and recreate the profile through a clean dynamic setup, or remove the fields through your approved secure configuration-management process. A shared credential referenced by other profiles must not be deleted; only delete an unneeded shared credential after confirming it is no longer referenced. See [Configuration](3-Configuration.md) for the full storage and cleanup model.

## 6. SSO

### 6.1 When to use

- Enterprise unified identity portal;
- Need to select an account and role after login;
- Multiple profiles can reuse the same SSO Session;
- Want daily business commands to automatically refresh OAuth and STS temporary credentials.

### 6.2 Information to prepare

- An SSO Session name, chosen by the user;
- The SSO Start URL, provided by the enterprise identity administrator;
- The region where the SSO service runs;
- An already existing target profile;
- The target account ID and role name; these can also be selected interactively during first configuration;
- The region and endpoint used for TLS.

The SSO Session's region is used for SSO login; the TLS profile's region is used for TLS request signing. They have different meanings and must not be substituted for each other.

### 6.3 Configure an SSO Session

```bash
volclog configure sso-session \
  --name corp \
  --start-url 'https://example.volccloudidentity.com/userportal' \
  --region cn-beijing
```

Optional registration scopes:

```bash
volclog configure sso-session \
  --name corp \
  --start-url 'https://example.volccloudidentity.com/userportal' \
  --region cn-beijing \
  --registration-scopes cloudidentity:account:access,offline_access
```

When scopes are not supplied explicitly, a new Session uses the default scopes. When updating an existing Session, omitting this parameter keeps the previous value.

### 6.4 Bind a profile and first login

`configure sso` binds an existing profile to an SSO Session. It preserves the profile's existing TLS region, endpoint, and timeout, and switches the auth mode to `sso`. The binding updates the mode, SSO Session name, account ID, and role name fields; it also clears the profile's Console Login `login-session` binding and resets old `sts-expiration` metadata. This binding step does not itself mean a pre-existing Console Login cache was deleted. Any existing static `AccessKeyID`, `SecretAccessKey`, `SecurityToken`, and `CredRef` remain stored on disk as dormant fields. The SSO provider does not use them, and the provider path is fail-closed, but the dormant values are still present in the configuration file.

Select the account and role interactively:

```bash
volclog configure sso \
  --profile sso-dev \
  --sso-session corp
```

When the account and role are already known:

```bash
volclog configure sso \
  --profile sso-dev \
  --sso-session corp \
  --account-id '<account-id>' \
  --role-name '<role-name>'
```

A terminal without a graphical interface can disable automatic browser opening:

```bash
volclog configure sso \
  --profile sso-dev \
  --sso-session corp \
  --no-browser
```

The command prints an authorization URL and a device code prompt. Complete the authorization in a browser that can access the page.

`configure sso` does not switch the current profile automatically. Switch explicitly after completion:

```bash
volclog configure use sso-dev
```

If the profile does not yet have a TLS region or endpoint, supply them explicitly for verification and business commands:

```bash
VOLCENGINE_REGION=cn-beijing \
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog --profile sso-dev doctor --online
```

### 6.5 Use and verify

After the initial `configure sso`, the local state may contain only the OAuth token and no role STS credential yet. In this expected initial state, the offline `doctor` reports credentials not ready (`present=false`) and exits 2. This does not mean the browser login failed; it only means the on-demand role STS exchange has not happened yet.

Run the online check or a real business request to perform the on-demand STS exchange:

```bash
volclog --profile sso-dev doctor --online
```

After a successful online exchange, the STS credential is cached and a subsequent offline `doctor` can report it as present. You can also run a read-only business command directly:

```bash
volclog --profile sso-dev tool exec project.describe-projects
```

The first real business request obtains the STS temporary credentials for the target account and role on demand.

### 6.6 Automatic refresh and re-login

Both the SSO OAuth Token and STS credentials use a 60-second safety refresh window. Their hard expiry values come from the service responses (the OAuth token expiry and the STS `Expiration`); both refresh at hard expiry minus 60 seconds. These lifetimes are set by the SSO service and are not user-configurable CLI parameters:

- When the cache is still valid, it is reused directly;
- Near expiry, business commands silently refresh the OAuth Token using the refresh token;
- When STS is missing or near expiry, a valid OAuth Token is used to exchange for new role credentials;
- Ordinary business commands never start device authorization or open a browser.

Explicit re-login is only needed when silent refresh cannot continue:

```bash
volclog sso login --profile sso-dev
```

You can also re-login directly by Session:

```bash
volclog sso login --sso-session corp
```

Without a graphical interface:

```bash
volclog sso login --profile sso-dev --no-browser
```

`--profile` and `--sso-session` are mutually exclusive selectors.

### 6.7 Log out

SSO logout is session-scoped. `volclog sso logout --profile NAME` uses the named profile only to resolve its SSO Session; it then has the same Session-level cleanup scope as selecting `--sso-session` directly. It deletes the Session's OAuth token, all unique STS caches associated with that Session, and clears the STS-expiration state on every profile still bound to the Session. If multiple profiles share an SSO Session, logging out one of them clears the shared Session state for all of them.

Log out by profile (resolves and clears its Session):

```bash
volclog sso logout --profile sso-dev
```

Log out by Session:

```bash
volclog sso logout --sso-session corp
```

Logging out does not delete the SSO Session configuration, profiles, account ID, role name, or TLS runtime configuration. It also does not remove any dormant static `AccessKeyID`, `SecretAccessKey`, `SecurityToken`, or `CredRef` fields that may remain from a previous static configuration. The same dormant-field retention and cleanup guidance described in [Section 5.7](#57-log-out) applies here.

## 7. RAM Role ARN

### 7.1 When to use

- You already have a source RAM identity and need to assume another role;
- Access TLS resources in a target account;
- Want to avoid distributing the target role's long-term AK/SK to users;
- A local tool or automation task can safely provide the source credentials.

### 7.2 Prerequisites

- The source identity has `sts:AssumeRole` permission;
- The target role's trust policy allows this source identity to assume it;
- The user knows the target account ID and role name exactly;
- The target role has the required TLS permissions;
- The TLS region and endpoint have been determined.

### 7.3 Use inline source AK/SK

```bash
volclog configure set \
  --profile ram-tls-readonly \
  --mode ramrolearn \
  --account-id '<target-account-id>' \
  --role-name '<target-role-name>' \
  --ak '<source-access-key-id>' \
  --sk '<source-secret-access-key>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

If the source identity is itself a temporary identity, also supply the source Session Token:

```bash
volclog configure set \
  --profile ram-tls-readonly \
  --mode ramrolearn \
  --account-id '<target-account-id>' \
  --role-name '<target-role-name>' \
  --ak '<source-access-key-id>' \
  --sk '<source-secret-access-key>' \
  --token '<source-session-token>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

### 7.4 Use a credential reference

```bash
volclog configure set \
  --profile ram-tls-readonly \
  --mode ramrolearn \
  --account-id '<target-account-id>' \
  --role-name '<target-role-name>' \
  --cred-ref source-account \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

### 7.5 Verify, refresh, and clean up

```bash
volclog --profile ram-tls-readonly doctor
volclog --profile ram-tls-readonly doctor --online
```

Each process calls AssumeRole the first time it needs to sign, requesting a validity of 3600 seconds. The hard `ExpiresAt` is the earlier of the server-returned `ExpiredTime` and the first request start plus one hour. The obtained temporary credentials are cached only within the current process and refreshed no later than 60 seconds before that hard expiry. These lifetimes are not user-configurable CLI parameters.

After the command ends, the in-process temporary credentials disappear with the process. Delete the configuration:

```bash
volclog configure delete ram-tls-readonly
```

When AssumeRole fails, the CLI does not fall back to other static identities in the environment or profile to continue sending TLS requests.

## 8. OIDC

### 8.1 When to use

- VKE Pods using a projected ServiceAccount Token;
- CI platforms that provide an OIDC Token file;
- Workloads that assume a RAM role through OIDC federated identity;
- Do not want to distribute long-term AK/SK to the workload.

### 8.2 Prerequisites

- The OIDC identity provider has been established in the account;
- The target role's trust policy allows the corresponding OIDC identity;
- The token's issuer, audience, subject, and other claims satisfy the trust conditions;
- The workload can read the token file;
- The user knows the target role TRN exactly;
- The TLS region and endpoint have been determined.

### 8.3 Configure

```bash
volclog configure set \
  --profile oidc-tls \
  --mode oidc \
  --role-trn 'trn:iam::<account-id>:role/<role-name>' \
  --oidc-token-file /var/run/secrets/oidc/token \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

OIDC mode does not require AK/SK to be configured.

### 8.4 Token file requirements

`volclog` re-parses and reads the token file every time it needs to refresh STS credentials, so it supports rotation where the platform swaps the target file through a symbolic link.

The token file that is finally opened must:

- Be a regular file;
- Be readable by the current process;
- Have non-empty content;
- Contain no NUL bytes;
- Be no larger than 64 KiB.

Never write the token content into command-line arguments, logs, or diagnostic output.

### 8.5 Verify, refresh, and clean up

Offline check of the token file path and accessibility:

```bash
volclog --profile oidc-tls doctor
```

Verify that OIDC exchanges for temporary credentials and accesses TLS:

```bash
volclog --profile oidc-tls doctor --online
```

When OIDC exchanges for credentials, it requests a validity of 3660 seconds; the actual hard expiration is determined by the `Expiration` returned by the server. Temporary credentials are cached only within the current process and refreshed no later than 60 seconds before the hard expiration.

When refresh fails, the token file is unreadable, or the trust relationship does not match, the TLS request fails closed and does not try other AK/SK.

Delete the configuration:

```bash
volclog configure delete oidc-tls
```

## 9. ECS Role

### 9.1 When to use

- `volclog` runs directly on an ECS instance;
- The instance already has an instance role with TLS permissions attached;
- Do not want to store long-term AK/SK on the instance;
- The runtime environment allows access to the instance metadata service.

### 9.2 Prerequisites

- The ECS instance already has the target instance role attached;
- The user knows the role name exactly;
- The role has the target TLS resource permissions;
- The instance can reach the fixed metadata address `100.96.0.96`;
- The TLS region and endpoint have been determined.

Do not use ECS Role as a substitute for other authentication methods on an ordinary development machine or in a container without an attached role.

### 9.3 Configure

```bash
volclog configure set \
  --profile ecs-tls \
  --mode ecsrole \
  --role-name '<ecs-role-name>' \
  --region cn-beijing \
  --endpoint https://tls-cn-beijing.volces.com
```

ECS Role does not require AK/SK to be configured.

### 9.4 Verify, refresh, and clean up

Live verification must run inside the target ECS instance:

```bash
volclog --profile ecs-tls doctor
volclog --profile ecs-tls doctor --online
```

When obtaining credentials:

- Access the fixed instance metadata address;
- Obtain a new IMDSv2 Token before every credential refresh;
- The IMDSv2 Token request TTL is 21600 seconds;
- The hard expiration of the role's temporary credentials is taken from `ExpiredTime`;
- Refresh no later than 5 minutes before `ExpiredTime`;
- Temporary credentials are cached only within the current process.

If the runtime environment explicitly forbids access to instance metadata, you can set:

```bash
export VOLCENGINE_ECS_METADATA_DISABLED=true
```

After this is set, ECS Role fails closed before any network access.

Delete the configuration:

```bash
volclog configure delete ecs-tls
```

## 10. Cache, refresh, and expiry behavior

| Method | Temporary state location | Hard expiry source | Refresh boundary | Cross-process reuse |
| --- | --- | --- | --- | --- |
| Static AK/SK | Profile, secrets file, or environment variables | N/A | Not refreshed automatically | Depends on the storage method the user chose |
| Manual STS | Profile, secrets file, or environment variables | N/A | Not refreshed automatically | Depends on the storage method the user chose |
| Console Login | `login/cache/` | Cached `IssuedAt` + `ExpiresIn` | 60 seconds before hard expiry | Yes |
| SSO | `sso/cache/` | Service response (OAuth `ExpiresAt` and STS `Expiration`) | Both OAuth and STS are 60 seconds before hard expiry | Yes |
| RAM Role ARN | Current process memory | Earlier of server `ExpiredTime` and request start + 1 hour | 60 seconds before hard expiry | No |
| OIDC | Current process memory | Server `Expiration` (3660 seconds requested) | 60 seconds before hard expiry | No |
| ECS Role | Current process memory | Server `ExpiredTime` | 5 minutes before `ExpiredTime` | No |

Hard expiry values for Console Login, SSO, RAM Role ARN, OIDC, and ECS Role are determined by the issuing service or the server response; they are not exposed as user-configurable CLI TTL parameters.

### 10.1 State root and cache directories

Default state directory:

```text
~/.volclog/
├── config.json
├── login/
│   └── cache/
└── sso/
    └── cache/
```

If `VOLCLOG_CONFIG` is set, the state root is the directory containing that configuration file.

You can override the cache directories separately:

```bash
export VOLCLOG_LOGIN_CACHE_DIRECTORY=/secure/path/login-cache
export VOLCLOG_SSO_CACHE_DIRECTORY=/secure/path/sso-cache
```

On Unix, the state root and cache directories are created with `0700` permissions, cache files with `0600` permissions, and the configuration file (`config.json`, or the file selected by `VOLCLOG_CONFIG`) — which may contain static AK/SK — is written with `0600` permissions. This is the Unix permission boundary for stored credentials. Do not commit cache directories or the configuration file to version control, and do not copy cache files between users.

### 10.2 Fail closed

SSO, Console Login, RAM Role ARN, OIDC, and ECS Role all follow fail-closed behavior:

- When the selected provider cannot obtain valid credentials, no TLS request is sent;
- It does not automatically switch to environment AK/SK;
- It does not use static fields retained in the profile but not selected by the current mode;
- When refresh fails after the expiry boundary is reached, it does not continue returning stale credentials.

This prevents quietly using another identity to access TLS when there is a configuration error.

### 10.3 `disable-ssl`

RAM Role ARN and OIDC support `--disable-ssl=true`, but this option makes STS authentication requests use HTTP. To use it, append `--disable-ssl=true` to the corresponding complete `configure set` command.

It does not change the protocol of the TLS business endpoint itself. When enabled, authentication material may be transmitted over a plaintext network; unless you are in an explicitly controlled trusted network, do not use it.

The default is HTTPS.

## 11. Troubleshooting

### 11.1 `profile not found`

Check the profile name and current selection:

```bash
volclog configure list
volclog configure show --profile NAME
volclog configure use NAME
```

### 11.2 Missing region or endpoint

Workload modes should write region and endpoint explicitly during `configure set`. If a dynamic-login profile does not store these values, you can supply them for a single command:

```bash
VOLCENGINE_REGION=cn-beijing \
VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com \
volclog --profile NAME doctor --online
```

### 11.3 `ReauthRequired`

Console Login:

```bash
volclog login --profile NAME
```

SSO:

```bash
volclog sso login --profile NAME
```

Workload modes cannot be recovered through interactive login. Fix the source credentials, role trust, OIDC Token, or ECS metadata access instead.

### 11.4 `401 Unauthorized`

Check first:

- Whether the static AK/SK is correct;
- Whether the manual STS triple comes from the same issuance;
- Whether the temporary credentials have expired;
- Whether the region matches the signature and the target service;
- Whether the profile actually being used is the correct one.

### 11.5 `403 Forbidden`

The authentication identity may be valid but lacks permission for the target TLS resource. Check:

- RAM user or role policies;
- AssumeRole trust policies;
- OIDC trust conditions;
- ECS instance role policies;
- The account and role selected through SSO;
- The authorization scope of the target Project, Topic, or other resource.

Do not mask permission configuration problems by retrying repeatedly.

### 11.6 RAM Role AssumeRole failure

Check the two layers of permissions separately:

1. Whether the source identity is allowed to call `sts:AssumeRole`;
2. Whether the target role trusts this source identity.

Then confirm that `account-id` and `role-name` are exactly correct, and run:

```bash
volclog --profile NAME doctor --online
```

### 11.7 OIDC Token file unavailable

Run the offline check:

```bash
volclog --profile NAME doctor
```

Confirm:

- The path points to a regular file that can be resolved;
- The current process has read permission;
- The file is non-empty and no larger than 64 KiB;
- After the platform rotates the token, the symbolic link target is still valid;
- The token claims satisfy the role trust conditions.

### 11.8 ECS metadata access failure

Confirm:

- The command is indeed running inside the target ECS instance;
- The instance already has the role configured attached;
- `VOLCENGINE_ECS_METADATA_DISABLED` is not set to `true`;
- Network policy allows access to `100.96.0.96`;
- The role name matches the role attached to the instance.

### 11.9 Offline doctor succeeds, but online doctor fails

Offline success only means the local configuration looks usable. Continue checking the online result for:

- Endpoint URL parsing;
- DNS, TCP, and TLS connectivity;
- Proxy environment;
- Credential acquisition or refresh;
- The status code and Request ID returned by the TLS service;
- Whether the current identity has `DescribeProjects` permission.

Keep the Request ID for later troubleshooting, but do not record any plaintext credentials at the same time.

## 12. Recommended acceptance flow

Each time you add or switch an authentication method, accept it in this order:

```bash
# 1. Confirm the current configuration and target profile
volclog configure show --profile NAME

# 2. Run only the local configuration check
volclog --profile NAME doctor

# 3. Verify real TLS read-only access
volclog --profile NAME doctor --online

# 4. Then run the target business command
volclog --profile NAME tool exec project.describe-projects
```

Acceptance criteria:

- The expected profile and expected auth mode are being used;
- Region and endpoint are correct;
- Valid signing credentials can be obtained;
- `doctor --online` succeeds;
- The identity has only the minimum TLS permissions required by the scenario;
- No plaintext credentials appear in logs or command output;
- After login expiry, it can be refreshed or recovered according to the corresponding method;
- When a provider fails, it does not switch to another identity.

---

[← Previous: Getting Started](1-Getting-Started.md) | [中文](2-Authentication_zh.md) | [Next: Configuration →](3-Configuration.md)
