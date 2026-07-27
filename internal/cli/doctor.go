package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/oidc"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runDoctor(ctx *Context, args []string) (any, int, error) {
	if hasHelp(args) {
		return nil, 0, &usageError{Text: usageDoctor(), ExitCode: 0}
	}
	online := false
	for len(args) > 0 {
		switch args[0] {
		case "--online":
			online = true
			args = args[1:]
		default:
			return nil, 0, &usageError{Text: usageDoctor(), ExitCode: 1}
		}
	}

	localStartMS := time.Now().UnixMilli()

	cfg, cfgPath, err := config.Load()
	if err != nil {
		return nil, 0, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, 0, err
	}
	projectCfg, _, err := config.LoadProjectConfig(wd)
	if err != nil {
		return nil, 0, err
	}

	profileName := strings.TrimSpace(ctx.Profile)
	profileSource := "flag"
	if profileName == "" {
		profileName = strings.TrimSpace(cfg.CurrentProfile)
		profileSource = "current"
	}
	if profileName == "" {
		profileName = "default"
		profileSource = "default"
	}

	envRegion := strings.TrimSpace(os.Getenv("VOLCENGINE_REGION"))
	envEndpoint := strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT"))

	var p config.Profile
	if pp, ok := cfg.GetProfile(profileName); ok {
		p = pp
	}
	mode, _ := config.NormalizeAuthMode(p.Mode)
	isDynamic := config.IsProviderAuthMode(mode)

	// credStatus holds the resolved credential status. Provider modes ignore
	// environment AK/SK. Cached login modes derive status from the read-only
	// authStatusReader; workload readiness is reported separately below.
	var credStatus config.CredentialStatus
	var dynStatus authStatus
	var dynReader authStatusReader
	if isDynamic {
		credStatus = config.CredentialStatus{Mode: mode, Source: "profile"}
		if reader, rerr := dynamicAuthStatusReader(mode, cfgPath, profileName, cfg, ctx.authFactory); rerr == nil && reader != nil {
			dynReader = reader
			if st, serr := reader.Status(context.Background(), profileName); serr == nil {
				dynStatus = st
				credStatus.Present = st.Present
			}
		}
	} else {
		profileCredStatus := config.ResolveProfileCredentialStatus(cfg, p)
		credStatus = profileCredStatus
		if envCredStatus := config.ResolveEnvCredentialStatus(); envCredStatus.Present {
			credStatus = envCredStatus
		}
	}
	credMode := credStatus.Mode
	credSource := credStatus.Source
	credPresent := credStatus.Present
	akPresent := credStatus.AK
	skPresent := credStatus.SK
	tokenPresent := credStatus.Token
	// sourceReady tracks workload local source readiness; only set for workload
	// modes and used in the offline exit gate.
	sourceReady := false

	region := ""
	regionSource := ""
	if envRegion != "" {
		region = envRegion
		regionSource = "env"
	} else if strings.TrimSpace(p.Region) != "" {
		region = strings.TrimSpace(p.Region)
		regionSource = "profile"
	} else if strings.TrimSpace(projectCfg.Region) != "" {
		region = strings.TrimSpace(projectCfg.Region)
		regionSource = "project"
	}

	endpoint := ""
	endpointSource := ""
	if envEndpoint != "" {
		endpoint = envEndpoint
		endpointSource = "env"
	} else if strings.TrimSpace(p.Endpoint) != "" {
		endpoint = strings.TrimSpace(p.Endpoint)
		endpointSource = "profile"
	} else if strings.TrimSpace(projectCfg.Endpoint) != "" {
		endpoint = strings.TrimSpace(projectCfg.Endpoint)
		endpointSource = "project"
	} else if region != "" {
		endpoint = config.DefaultEndpointForRegion(region)
		endpointSource = "derived"
	}

	timeoutSeconds := 0
	timeoutSource := ""
	if p.TimeoutSeconds > 0 {
		timeoutSeconds = p.TimeoutSeconds
		timeoutSource = "profile"
	} else if projectCfg.TimeoutSeconds > 0 {
		timeoutSeconds = projectCfg.TimeoutSeconds
		timeoutSource = "project"
	} else {
		timeoutSeconds = 60
		timeoutSource = "default"
	}

	credPresentCheck := map[string]any{"name": "credentials_present", "ok": credPresent, "detail": credSource}
	// Workload modes use source_ready as the readiness gate (added below), not
	// credentials_present, since credentials are on-demand and never cached.
	// Static/SSO/Console keep the credentials_present check unchanged.
	includeCredCheck := !isDynamic || config.IsCachedLoginAuthMode(mode)
	checks := []map[string]any{
		{"name": "config_load", "ok": true, "detail": cfgPath},
	}
	if includeCredCheck {
		checks = append(checks, credPresentCheck)
	}
	checks = append(checks, map[string]any{"name": "region_present", "ok": strings.TrimSpace(region) != "", "detail": regionSource})
	checks = append(checks, map[string]any{"name": "endpoint_present", "ok": strings.TrimSpace(endpoint) != "", "detail": endpointSource})

	skewRisk := "unknown"
	localNowMS := localStartMS
	var serverMS int64
	var skewSeconds int64
	endpointValid := strings.TrimSpace(endpoint) != ""
	// onlineOK records whether the online DescribeProjects check actually
	// succeeded. It drives the exit code when --online is set: a real online
	// success must be exit 0 even if pre-network credPresent was false (e.g.
	// first-time SSO with only an OAuth token); a real online failure must be
	// exit 2 even if credPresent was true.
	onlineOK := false

	if online {
		endpointParseOK := false
		u, parseErr := parseEndpointURL(endpoint)
		if parseErr == nil {
			endpointParseOK = true
			endpointValid = true
		} else {
			endpointValid = false
		}
		parseDetail := ""
		if parseErr != nil {
			parseDetail = parseErr.Error()
		} else {
			parseDetail = u.String()
		}
		checks = append(checks, map[string]any{"name": "online_endpoint_parse", "ok": endpointParseOK, "detail": parseDetail})

		proxyVar, proxyVal := detectProxyEnv()
		if proxyVar != "" {
			checks = append(checks, map[string]any{"name": "online_proxy_detected", "ok": true, "detail": proxyVar + "=" + proxyVal})
		}

		if endpointParseOK && proxyVar == "" {
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				if strings.EqualFold(u.Scheme, "http") {
					port = "80"
				} else {
					port = "443"
				}
			}
			ctxDNS, cancelDNS := context.WithTimeout(context.Background(), 2*time.Second)
			ips, err := net.DefaultResolver.LookupIPAddr(ctxDNS, host)
			cancelDNS()
			if err != nil {
				checks = append(checks, map[string]any{"name": "online_dns_resolve", "ok": false, "detail": err.Error()})
			} else {
				checks = append(checks, map[string]any{"name": "online_dns_resolve", "ok": true, "detail": strconv.Itoa(len(ips))})
				d := &net.Dialer{Timeout: 2 * time.Second}
				conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort(host, port))
				if err != nil {
					checks = append(checks, map[string]any{"name": "online_tcp_connect", "ok": false, "detail": err.Error()})
				} else {
					_ = conn.Close()
					checks = append(checks, map[string]any{"name": "online_tcp_connect", "ok": true, "detail": net.JoinHostPort(host, port)})
					if strings.EqualFold(u.Scheme, "https") {
						tc, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort(host, port), &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
						if err != nil {
							checks = append(checks, map[string]any{"name": "online_tls_handshake", "ok": false, "detail": err.Error()})
						} else {
							_ = tc.Close()
							checks = append(checks, map[string]any{"name": "online_tls_handshake", "ok": true, "detail": host})
						}
					}
				}
			}
		} else {
			skipReason := "skipped: missing endpoint"
			if proxyVar != "" {
				skipReason = "skipped: proxy configured"
			} else if !endpointParseOK {
				skipReason = "skipped: endpoint parse failed"
			}
			checks = append(checks, map[string]any{"name": "online_dns_resolve", "ok": true, "detail": skipReason})
			checks = append(checks, map[string]any{"name": "online_tcp_connect", "ok": true, "detail": skipReason})
			checks = append(checks, map[string]any{"name": "online_tls_handshake", "ok": true, "detail": skipReason})
		}

		ok := false
		detail := ""
		if strings.TrimSpace(region) != "" && strings.TrimSpace(endpoint) != "" {
			timeout := time.Duration(timeoutSeconds) * time.Second
			var cl *tlsapi.Client
			var clErr error
			if isDynamic {
				// Build the dynamic client with the doctor's resolved runtime
				// settings (endpoint/region/timeout), not the raw profile p which
				// may lack env/project-default overrides. This keeps the online
				// DescribeProjects request consistent with the reported values.
				resolvedP := p
				resolvedP.Endpoint = endpoint
				resolvedP.Region = region
				resolvedP.TimeoutSeconds = timeoutSeconds
				cl, clErr = buildDynamicClient(mode, cfgPath, profileName, cfg, resolvedP, ctx.authFactory)
			} else {
				cl, clErr = tlsapi.New(endpoint, region, profileName, credStatus.AccessKeyID, credStatus.SecretAccessKey, credStatus.SecurityToken, timeout)
			}
			if clErr != nil {
				detail = clErr.Error()
			} else {
				body, _ := util.MustJSON(map[string]any{})
				resp, err := cl.Do(context.Background(), "GET", "/DescribeProjects", map[string]string{
					"PageSize":   "1",
					"PageNumber": "1",
				}, nil, body)
				if err != nil {
					detail = err.Error()
				} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					ok = true
					detail = resp.Header.Get("x-tls-requestid")
					localNowMS = time.Now().UnixMilli()
					if t, ok := parseHTTPDate(resp.Header.Get("Date")); ok {
						serverMS = t.UnixMilli()
						skewSeconds = (localNowMS - serverMS) / 1000
						if skewSeconds < 0 {
							if -skewSeconds > 300 {
								skewRisk = "high"
							} else {
								skewRisk = "low"
							}
						} else {
							if skewSeconds > 300 {
								skewRisk = "high"
							} else {
								skewRisk = "low"
							}
						}
					}
				} else {
					ok = false
					detail = "status=" + strconv.Itoa(resp.StatusCode)
				}
			}
		} else {
			detail = "missing region/endpoint"
		}
		checks = append(checks, map[string]any{"name": "online_describe_projects", "ok": ok, "detail": detail})
		onlineOK = ok
		if serverMS > 0 {
			okSkew := skewRisk != "high"
			checks = append(checks, map[string]any{"name": "online_time_skew", "ok": okSkew, "detail": "skew_seconds=" + strconv.FormatInt(skewSeconds, 10)})
		}
		// After a successful online DescribeProjects for a dynamic profile, the
		// provider may have exchanged/refreshed and written fresh STS credentials
		// to the cache. Re-read the cache metadata with the same read-only status
		// reader (no network, no refresh) so the reported credentials reflect the
		// post-online state. This is what makes first-time SSO (OAuth-only) show
		// present=true after a successful online exchange.
		if ok && isDynamic {
			if config.IsCachedLoginAuthMode(mode) && dynReader != nil {
				// SSO/Console: re-read the cache metadata with the same read-only
				// status reader (no network, no refresh) so the reported
				// credentials reflect the post-online state.
				if st, serr := dynReader.Status(context.Background(), profileName); serr == nil {
					dynStatus = st
					credStatus.Present = st.Present
					credPresent = st.Present
					credPresentCheck["ok"] = credPresent
				}
			} else if !config.IsCachedLoginAuthMode(mode) {
				// Workload modes: a successful online DescribeProjects means the
				// provider retrieved temporary credentials in-memory. Mark them
				// present for this diagnostic run (they are not persisted to disk).
				credPresent = true
				credPresentCheck["ok"] = true
			}
		}
	}

	credentials := map[string]any{
		"mode":    credMode,
		"source":  credSource,
		"present": credPresent,
		"ak":      akPresent,
		"sk":      skPresent,
		"token":   tokenPresent,
	}
	if isDynamic {
		if config.IsCachedLoginAuthMode(mode) {
			// SSO/Console: disk cache status from the read-only status reader.
			// Safe defaults: provider name from mode; cache treated as
			// absent/expired until the status reader proves otherwise.
			providerName := dynamicProviderName(mode)
			refreshRequired := true
			if dynStatus.Provider != "" {
				providerName = dynStatus.Provider
			}
			if dynStatus.Present {
				refreshRequired = dynStatus.RefreshRequired
			}
			credentials["provider"] = providerName
			credentials["expires_at"] = formatExpiresAt(dynStatus.ExpiresAt)
			credentials["refresh_required"] = refreshRequired
		} else {
			// Workload modes (ramrolearn/oidc/ecsrole): on-demand, memory-only.
			// No disk cache exists; credentials are present only after a
			// successful online retrieval. The source type describes the local
			// configuration that will be used when credentials are requested.
			sourceReady = workloadSourceReady(mode, p, cfg)
			credentials["provider"] = dynamicProviderName(mode)
			credentials["source"] = workloadSourceType(mode, p)
			credentials["present"] = credPresent
			credentials["ak"] = credPresent
			credentials["sk"] = credPresent
			credentials["token"] = credPresent
			credentials["expires_at"] = ""
			credentials["on_demand"] = true
			credentials["memory_only"] = true
			credentials["source_ready"] = sourceReady
		}
	}
	// For workload modes, add a stable source_ready check that drives the offline
	// exit gate. The detail is only the source type, never role/TRN/path/credential.
	if isDynamic && !config.IsCachedLoginAuthMode(mode) {
		checks = append(checks, map[string]any{"name": "source_ready", "ok": sourceReady, "detail": workloadSourceType(mode, p)})
	}
	out := map[string]any{
		"profile": map[string]any{
			"selected": profileName,
			"source":   profileSource,
		},
		"endpoint": map[string]any{
			"value":  endpoint,
			"source": endpointSource,
		},
		"region": map[string]any{
			"value":  region,
			"source": regionSource,
		},
		"credentials": credentials,
		"time": map[string]any{
			"local_unix_ms":  localNowMS,
			"server_unix_ms": serverMS,
			"skew_seconds":   skewSeconds,
			"skew_risk":      skewRisk,
		},
		"timeout": map[string]any{
			"seconds": timeoutSeconds,
			"source":  timeoutSource,
		},
		"checks": checks,
	}

	// Warnings: disable-ssl for RAM/OIDC means STS will use HTTP and
	// credentials may be transmitted in plaintext. This never affects the TLS
	// business endpoint and never contains role/TRN/token/credential values.
	var warnings []map[string]any
	if insecureSSLCondition(mode, p.DisableSSL) {
		warnings = append(warnings, map[string]any{
			"name":   "disable-ssl",
			"detail": insecureSSLWarning,
		})
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}

	// Basic gates always apply: region and endpoint must be resolvable.
	basicOK := strings.TrimSpace(region) != "" && endpointValid
	if online {
		// --online: the real DescribeProjects result decides the exit code. A
		// successful online check is exit 0 (even if pre-network credPresent was
		// false, e.g. first-time SSO with only an OAuth token that got exchanged
		// online). A failed online check is exit 2 (even if credPresent was true
		// but the cache was stale and refresh/DescribeProjects failed).
		if basicOK && onlineOK {
			return out, 0, nil
		}
		return out, 2, nil
	}
	// Offline: exit 0 only when credentials are present and basic gates pass.
	// For workload modes, credentials are never present offline; exit 0 means the
	// local source is ready AND basic gates pass. A missing/bad source is exit 2.
	if isDynamic && !config.IsCachedLoginAuthMode(mode) {
		if !basicOK || !sourceReady {
			return out, 2, nil
		}
		return out, 0, nil
	}
	if !credPresent || !basicOK {
		return out, 2, nil
	}
	return out, 0, nil
}

func parseHTTPDate(s string) (time.Time, bool) {
	v := strings.TrimSpace(s)
	if v == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC1123, v); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC1123Z, v); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func parseEndpointURL(endpoint string) (*url.URL, error) {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return nil, errors.New("empty endpoint")
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, errors.New("invalid endpoint: missing host")
	}
	return u, nil
}

// formatExpiresAt renders an expiration time for diagnostic output. A zero
// time (no cache entry) renders as an empty string so the field is absent of
// fake values; otherwise it is rendered in RFC3339 UTC. No secret material is
// ever included.
func formatExpiresAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// workloadSourceType returns a stable, safe label describing where the workload
// provider will source its credentials from. It never contains role names,
// TRNs, token paths, or credential values.
func workloadSourceType(mode string, p config.Profile) string {
	switch mode {
	case config.AuthModeRamRoleARN:
		if strings.TrimSpace(p.CredRef) != "" {
			return "profile_cred_ref"
		}
		return "profile_inline"
	case config.AuthModeOIDC:
		return "token_file"
	case config.AuthModeECSRole:
		return "instance_metadata"
	}
	return "on_demand"
}

// workloadSourceReady reports whether the local configuration for a workload
// mode is sufficient to attempt credential retrieval. It performs only local,
// read-only checks: RAM needs inline/cred-ref AK/SK plus account-id/role-name;
// OIDC needs role-trn and a readable, regular token file; ECS needs role-name.
// It never contacts STS or IMDS and never reads token file contents.
func workloadSourceReady(mode string, p config.Profile, cfg config.Config) bool {
	switch mode {
	case config.AuthModeRamRoleARN:
		ak := strings.TrimSpace(p.AccessKeyID)
		sk := strings.TrimSpace(p.SecretAccessKey)
		if credRef := strings.TrimSpace(p.CredRef); credRef != "" {
			if cred, ok := cfg.GetCred(credRef); ok {
				if ak == "" {
					ak = strings.TrimSpace(cred.AccessKeyID)
				}
				if sk == "" {
					sk = strings.TrimSpace(cred.SecretAccessKey)
				}
			} else {
				return false
			}
		}
		return ak != "" && sk != "" && strings.TrimSpace(p.AccountID) != "" && strings.TrimSpace(p.RoleName) != ""
	case config.AuthModeOIDC:
		if strings.TrimSpace(p.RoleTRN) == "" {
			return false
		}
		tokenFile := strings.TrimSpace(p.OIDCTokenFile)
		if tokenFile == "" {
			return false
		}
		// Use the secure OIDC token file inspector: it resolves symlinks, opens
		// with O_NOFOLLOW/O_NONBLOCK, and requires a regular file. It never reads
		// the token contents.
		return oidc.InspectTokenFile(tokenFile) == nil
	case config.AuthModeECSRole:
		return strings.TrimSpace(p.RoleName) != ""
	}
	return false
}

func detectProxyEnv() (string, string) {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v != "" {
			return k, v
		}
	}
	return "", ""
}
