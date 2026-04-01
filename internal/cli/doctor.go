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

	envAK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_ID"))
	envSK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_SECRET"))
	envToken := strings.TrimSpace(os.Getenv("VOLCENGINE_TOKEN"))
	envRegion := strings.TrimSpace(os.Getenv("VOLCENGINE_REGION"))
	envEndpoint := strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT"))

	credMode := ""
	credSource := ""
	credPresent := false
	akPresent := false
	skPresent := false
	tokenPresent := false

	if envAK != "" || envSK != "" {
		credSource = "env"
		akPresent = envAK != ""
		skPresent = envSK != ""
		tokenPresent = envToken != ""
		credPresent = akPresent && skPresent
		if tokenPresent {
			credMode = "sts"
		} else {
			credMode = "aksk"
		}
	}

	var p config.Profile
	if !credPresent {
		if pp, ok := cfg.GetProfile(profileName); ok {
			p = pp
		}
		akPresent = strings.TrimSpace(p.AccessKeyID) != ""
		skPresent = strings.TrimSpace(p.SecretAccessKey) != ""
		tokenPresent = strings.TrimSpace(p.SecurityToken) != ""
		credPresent = akPresent && skPresent
		credSource = "profile"
		if tokenPresent {
			credMode = "sts"
		} else {
			credMode = "aksk"
		}
	}

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
	if strings.TrimSpace(region) == "" && strings.TrimSpace(endpoint) != "" {
		if r := config.DeriveRegionFromEndpoint(endpoint); strings.TrimSpace(r) != "" {
			region = r
			regionSource = "derived_endpoint"
		}
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

	checks := []map[string]any{
		{"name": "config_load", "ok": true, "detail": cfgPath},
		{"name": "credentials_present", "ok": credPresent, "detail": credSource},
		{"name": "region_present", "ok": strings.TrimSpace(region) != "", "detail": regionSource},
		{"name": "endpoint_present", "ok": strings.TrimSpace(endpoint) != "", "detail": endpointSource},
	}

	skewRisk := "unknown"
	localNowMS := localStartMS
	var serverMS int64
	var skewSeconds int64
	endpointValid := strings.TrimSpace(endpoint) != ""

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
			ak := envAK
			sk := envSK
			token := envToken
			if credSource != "env" {
				ak = strings.TrimSpace(p.AccessKeyID)
				sk = strings.TrimSpace(p.SecretAccessKey)
				token = strings.TrimSpace(p.SecurityToken)
			}
			timeout := time.Duration(timeoutSeconds) * time.Second
			cl, err := tlsapi.New(endpoint, region, profileName, ak, sk, token, timeout)
			if err != nil {
				detail = err.Error()
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
		if serverMS > 0 {
			okSkew := skewRisk != "high"
			checks = append(checks, map[string]any{"name": "online_time_skew", "ok": okSkew, "detail": "skew_seconds=" + strconv.FormatInt(skewSeconds, 10)})
		}
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
		"credentials": map[string]any{
			"mode":    credMode,
			"source":  credSource,
			"present": credPresent,
			"ak":      akPresent,
			"sk":      skPresent,
			"token":   tokenPresent,
		},
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

	if !credPresent || strings.TrimSpace(region) == "" || !endpointValid {
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

func detectProxyEnv() (string, string) {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v != "" {
			return k, v
		}
	}
	return "", ""
}
