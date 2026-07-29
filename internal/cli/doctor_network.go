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

	appruntime "github.com/volcengine-tls/ve-tls-cli/internal/app/runtime"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runDoctorNetworkChecks(ctx *Context, state *doctorRuntimeState) {
	endpointParseOK := false
	endpointURL, parseErr := parseEndpointURL(state.endpoint)
	if parseErr == nil {
		endpointParseOK = true
		state.endpointValid = true
	} else {
		state.endpointValid = false
	}
	parseDetail := ""
	if parseErr != nil {
		parseDetail = parseErr.Error()
	} else {
		parseDetail = endpointURL.String()
	}
	state.checks = append(state.checks, map[string]any{"name": "online_endpoint_parse", "ok": endpointParseOK, "detail": parseDetail})

	proxyVar, proxyVal := detectProxyEnv()
	if proxyVar != "" {
		state.checks = append(state.checks, map[string]any{"name": "online_proxy_detected", "ok": true, "detail": proxyVar + "=" + proxyVal})
	}

	if endpointParseOK && proxyVar == "" {
		host := endpointURL.Hostname()
		port := endpointURL.Port()
		if port == "" {
			if strings.EqualFold(endpointURL.Scheme, "http") {
				port = "80"
			} else {
				port = "443"
			}
		}
		dnsContext, cancelDNS := context.WithTimeout(context.Background(), 2*time.Second)
		ips, err := net.DefaultResolver.LookupIPAddr(dnsContext, host)
		cancelDNS()
		if err != nil {
			state.checks = append(state.checks, map[string]any{"name": "online_dns_resolve", "ok": false, "detail": err.Error()})
		} else {
			state.checks = append(state.checks, map[string]any{"name": "online_dns_resolve", "ok": true, "detail": strconv.Itoa(len(ips))})
			dialer := &net.Dialer{Timeout: 2 * time.Second}
			conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(host, port))
			if err != nil {
				state.checks = append(state.checks, map[string]any{"name": "online_tcp_connect", "ok": false, "detail": err.Error()})
			} else {
				_ = conn.Close()
				state.checks = append(state.checks, map[string]any{"name": "online_tcp_connect", "ok": true, "detail": net.JoinHostPort(host, port)})
				if strings.EqualFold(endpointURL.Scheme, "https") {
					tlsConn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, port), &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
					if err != nil {
						state.checks = append(state.checks, map[string]any{"name": "online_tls_handshake", "ok": false, "detail": err.Error()})
					} else {
						_ = tlsConn.Close()
						state.checks = append(state.checks, map[string]any{"name": "online_tls_handshake", "ok": true, "detail": host})
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
		state.checks = append(state.checks, map[string]any{"name": "online_dns_resolve", "ok": true, "detail": skipReason})
		state.checks = append(state.checks, map[string]any{"name": "online_tcp_connect", "ok": true, "detail": skipReason})
		state.checks = append(state.checks, map[string]any{"name": "online_tls_handshake", "ok": true, "detail": skipReason})
	}

	ok := false
	detail := ""
	if strings.TrimSpace(state.region) != "" && strings.TrimSpace(state.endpoint) != "" {
		timeout := time.Duration(state.timeoutSeconds) * time.Second
		var client *tlsapi.Client
		var clientErr error
		if state.isDynamic {
			// Build the dynamic client with the doctor's resolved runtime
			// settings, not the raw profile which may lack env/project defaults.
			resolvedProfile := state.profile
			resolvedProfile.Endpoint = state.endpoint
			resolvedProfile.Region = state.region
			resolvedProfile.TimeoutSeconds = state.timeoutSeconds
			client, clientErr = appruntime.BuildClient(appruntime.BuildClientRequest{
				Mode:        state.mode,
				ConfigPath:  state.cfgPath,
				ProfileName: state.profileName,
				Config:      state.cfg,
				Profile:     resolvedProfile,
				Factory:     ctx.authFactory,
			})
		} else {
			client, clientErr = tlsapi.New(
				state.endpoint,
				state.region,
				state.profileName,
				state.credStatus.AccessKeyID,
				state.credStatus.SecretAccessKey,
				state.credStatus.SecurityToken,
				timeout,
			)
		}
		if clientErr != nil {
			detail = clientErr.Error()
		} else {
			body, _ := util.MustJSON(map[string]any{})
			response, err := client.Do(context.Background(), "GET", "/DescribeProjects", map[string]string{
				"PageSize":   "1",
				"PageNumber": "1",
			}, nil, body)
			if err != nil {
				detail = err.Error()
			} else if response.StatusCode >= 200 && response.StatusCode < 300 {
				ok = true
				detail = response.Header.Get("x-tls-requestid")
				state.localNowMS = time.Now().UnixMilli()
				if serverTime, parsed := parseHTTPDate(response.Header.Get("Date")); parsed {
					state.serverMS = serverTime.UnixMilli()
					state.skewSeconds = (state.localNowMS - state.serverMS) / 1000
					if state.skewSeconds < 0 {
						if -state.skewSeconds > 300 {
							state.skewRisk = "high"
						} else {
							state.skewRisk = "low"
						}
					} else {
						if state.skewSeconds > 300 {
							state.skewRisk = "high"
						} else {
							state.skewRisk = "low"
						}
					}
				}
			} else {
				ok = false
				detail = "status=" + strconv.Itoa(response.StatusCode)
			}
		}
	} else {
		detail = "missing region/endpoint"
	}
	state.checks = append(state.checks, map[string]any{"name": "online_describe_projects", "ok": ok, "detail": detail})
	state.onlineOK = ok
	if state.serverMS > 0 {
		okSkew := state.skewRisk != "high"
		state.checks = append(state.checks, map[string]any{
			"name":   "online_time_skew",
			"ok":     okSkew,
			"detail": "skew_seconds=" + strconv.FormatInt(state.skewSeconds, 10),
		})
	}

	// A successful dynamic request may have refreshed/exchanged credentials.
	// Re-read cached metadata without networking so output reflects that state.
	if ok && state.isDynamic {
		if config.IsCachedLoginAuthMode(state.mode) && state.dynamicReader != nil {
			if status, err := state.dynamicReader.Status(context.Background(), state.profileName); err == nil {
				state.dynamicStatus = status
				state.credStatus.Present = status.Present
				state.credPresent = status.Present
				state.credentialPresentCheck["ok"] = state.credPresent
			}
		} else if !config.IsCachedLoginAuthMode(state.mode) {
			state.credPresent = true
			state.credentialPresentCheck["ok"] = true
		}
	}
}

func parseHTTPDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC1123, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC1123Z, value); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func parseEndpointURL(endpoint string) (*url.URL, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("empty endpoint")
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if parsed.Host == "" {
		return nil, errors.New("invalid endpoint: missing host")
	}
	return parsed, nil
}

func detectProxyEnv() (string, string) {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return key, value
		}
	}
	return "", ""
}
