package cli

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

type Context struct {
	Stdout             io.Writer
	Stderr             io.Writer
	Format             output.Format
	FormatOverride     output.Format
	OutputExplicit     bool
	OutputMode         string
	OutputModeExplicit bool
	OutputDir          string
	OutputFile         string
	Profile            string
	RuntimeRegion      string
	RuntimeEndpoint    string
	GlobalSecretsFile  string
	Filter             string
	TraceDir           string
	TraceRedact        string
	TracePath          string
	RequestID          string
	StatusCode         int
	DryRun             bool
	Action             string
	PaginationMeta     map[string]any

	cfg       config.Config
	cfgPath   string
	profile   config.Profile
	client    *tlsapi.Client
	traceW    io.WriteCloser
	defaults  config.ProfileDefaults
	apiIOMeta apiIOMeta

	// profileResolved tracks whether ResolveProfile has run for the current
	// request. It replaces the old "AccessKeyID == \"\"" sentinel because
	// dynamic profiles legitimately have no static AK.
	profileResolved bool
	// forceStaticAuth is set when --secrets-file (global or context) loads
	// successfully. It forces the static EffectiveProfile path even when the
	// selected profile declares a dynamic mode.
	forceStaticAuth bool
	// authFactory constructs dynamic providers (SSO, Console Login, RAM Role
	// ARN, OIDC, ECS Role). Tests inject a fake; production uses
	// defaultAuthProviderFactory.
	authFactory authProviderFactory
}

func newContext(stdout, stderr io.Writer, format output.Format, profile, filter string) *Context {
	return &Context{
		Stdout:  stdout,
		Stderr:  stderr,
		Format:  format,
		Profile: profile,
		Filter:  filter,
	}
}

func (c *Context) LoadConfig() error {
	cfg, p, err := config.Load()
	if err != nil {
		return err
	}
	c.cfg = cfg
	c.cfgPath = p
	c.invalidateProfileCache()
	return nil
}

func (c *Context) SaveConfig() error {
	return config.Save(c.cfg, c.cfgPath)
}

func (c *Context) UpdateConfig(fn func(*config.Config) error) error {
	cfg, err := config.Update(c.cfgPath, fn)
	if err != nil {
		return err
	}
	c.cfg = cfg
	c.invalidateProfileCache()
	return nil
}

func (c *Context) ResolveProfile() error {
	if c.cfg.Version == 0 {
		if err := c.LoadConfig(); err != nil {
			return err
		}
	}
	// forceStaticAuth (set by --secrets-file) always uses the legacy static path,
	// even if the selected profile declares a dynamic mode.
	if c.forceStaticAuth {
		p, err := config.EffectiveProfile(c.cfg, c.Profile, c.staticProfileDefaults())
		if err != nil {
			return err
		}
		c.profile = applyRuntimeOverrides(p, c.RuntimeRegion, c.RuntimeEndpoint)
		c.profileResolved = true
		return nil
	}
	// Resolve the selected profile name without environment AK/SK override so we
	// can inspect its mode. Explicit --profile > current_profile > "default".
	name := c.cfg.SelectedProfileName(c.Profile)
	p, ok := c.cfg.GetProfile(name)
	if !ok {
		// No matching profile. Delegate to EffectiveProfile, which may still
		// succeed via environment AK/SK for the static path or return a clear
		// "profile not found" error.
		ep, err := config.EffectiveProfile(c.cfg, c.Profile, c.staticProfileDefaults())
		if err != nil {
			return err
		}
		c.profile = applyRuntimeOverrides(ep, c.RuntimeRegion, c.RuntimeEndpoint)
		c.profileResolved = true
		return nil
	}
	mode, err := config.NormalizeAuthMode(p.Mode)
	if err != nil {
		return err
	}
	if mode == config.AuthModeAK {
		// Static mode: unchanged behavior, full delegation to EffectiveProfile
		// (env AK/SK precedence, cred-ref, project defaults, timeout).
		ep, err := config.EffectiveProfile(c.cfg, c.Profile, c.staticProfileDefaults())
		if err != nil {
			return err
		}
		c.profile = applyRuntimeOverrides(ep, c.RuntimeRegion, c.RuntimeEndpoint)
		c.profileResolved = true
		return nil
	}
	if !config.IsProviderAuthMode(mode) {
		// Unknown or non-provider mode must not fall into the provider path.
		return errors.New("unsupported auth mode: " + mode)
	}
	// Provider mode (sso / console-login / ramrolearn / oidc / ecsrole): ignore
	// environment AK/SK and apply the fixed runtime settings precedence.
	// Provider construction is deferred to Client() so failures fail closed at
	// request time.
	c.profile = applyRuntimeOverrides(
		resolveDynamicRuntimeSettings(p, c.defaults),
		c.RuntimeRegion,
		c.RuntimeEndpoint,
	)
	c.profileResolved = true
	return nil
}

func (c *Context) staticProfileDefaults() config.ProfileDefaults {
	defaults := c.defaults
	if region := strings.TrimSpace(c.RuntimeRegion); region != "" {
		defaults.Region = region
	}
	if endpoint := strings.TrimSpace(c.RuntimeEndpoint); endpoint != "" {
		defaults.Endpoint = endpoint
	}
	return defaults
}

func applyRuntimeOverrides(p config.Profile, region, endpoint string) config.Profile {
	if region = strings.TrimSpace(region); region != "" {
		p.Region = region
	}
	if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
		p.Endpoint = endpoint
	}
	return p
}

func (c *Context) SetProfileDefaults(d config.ProfileDefaults) {
	c.defaults = d
	c.invalidateProfileCache()
}

// invalidateProfileCache clears the resolved profile and cached client so the
// next ResolveProfile/Client call re-reads the current config and defaults.
// Any caller that replaces cfg, cfgPath, or defaults must invoke this to avoid
// serving stale cached state from a previous request.
func (c *Context) invalidateProfileCache() {
	c.profileResolved = false
	c.profile = config.Profile{}
	c.client = nil
}

func (c *Context) Client() (*tlsapi.Client, error) {
	if c.client != nil {
		return c.client, nil
	}
	if !c.profileResolved {
		if err := c.ResolveProfile(); err != nil {
			return nil, err
		}
	}
	mode, err := config.NormalizeAuthMode(c.profile.Mode)
	if err != nil {
		return nil, err
	}
	if c.forceStaticAuth || mode == config.AuthModeAK {
		t := time.Duration(c.profile.TimeoutSeconds) * time.Second
		cl, err := tlsapi.New(c.profile.Endpoint, c.profile.Region, c.Profile, c.profile.AccessKeyID, c.profile.SecretAccessKey, c.profile.SecurityToken, t)
		if err != nil {
			return nil, err
		}
		c.client = cl
		return cl, nil
	}
	if !config.IsProviderAuthMode(mode) {
		return nil, errors.New("unsupported auth mode: " + mode)
	}
	// Provider mode: build the provider-backed client. Provider construction or
	// Retrieve failures fail closed; never fall back to environment AK/SK.
	name := c.cfg.SelectedProfileName(c.Profile)
	cl, err := buildDynamicClient(mode, c.cfgPath, name, c.cfg, c.profile, c.authFactory)
	if err != nil {
		return nil, err
	}
	c.client = cl
	return cl, nil
}

func (c *Context) DoRaw(method, path string, query map[string]string, header map[string]string, body []byte) (tlsapi.Response, error) {
	client, err := c.Client()
	if err != nil {
		return tlsapi.Response{}, err
	}
	start := time.Now()
	c.traceRequest(method, path, query, body)
	resp, err := client.Do(context.Background(), method, path, query, header, body)
	if err != nil {
		c.traceResponse(0, "", time.Since(start), nil, err)
		return tlsapi.Response{}, err
	}
	c.traceResponse(resp.StatusCode, resp.Header.Get("x-tls-requestid"), time.Since(start), resp.Body, nil)
	c.RequestID = resp.Header.Get("x-tls-requestid")
	c.StatusCode = resp.StatusCode
	return resp, nil
}

func (c *Context) Do(method, path string, query map[string]string, header map[string]string, body []byte) (any, error) {
	meta := c.apiIOMeta
	if meta.Method == "" {
		meta.Method = method
	}
	if meta.Path == "" {
		meta.Path = path
	}
	if meta.RequestFormat == "" {
		meta.RequestFormat = requestFormatJSON
	}
	if meta.OutputFormat == "" {
		meta.OutputFormat = c.Format
	}
	if meta.OutputMode == "" {
		meta.OutputMode = c.OutputMode
	}
	previewBody := append([]byte(nil), body...)
	adaptedHeader, adaptedBody, state, specialHandled, err := prepareSpecialIORequest(meta, header, body)
	if err != nil {
		return nil, err
	}
	if specialHandled {
		header = adaptedHeader
		body = adaptedBody
	}
	if c.DryRun {
		plan := c.buildDryRunPlan(method, path, query, header, body, originalRequestPreview(previewBody, body, specialHandled), meta.RequestFormat, specialHandled)
		c.tracePlan(method, path, query, header, body, plan)
		return plan, nil
	}
	resp, err := c.DoRaw(method, path, query, header, body)
	if err != nil {
		return nil, err
	}
	if out, handled, err := decodeSpecialIOResponse(meta, state, resp); handled {
		return out, err
	}
	return decodeResponse(resp)
}

func (c *Context) buildDryRunPlan(method, path string, query map[string]string, header map[string]string, body []byte, previewBody []byte, previewFormat requestFormat, specialHandled bool) map[string]any {
	checks := make([]map[string]any, 0, 4)
	valid := true
	if err := c.ResolveProfile(); err != nil {
		checks = append(checks, map[string]any{
			"name":   "profile",
			"ok":     false,
			"detail": err.Error(),
		})
		valid = false
	} else {
		checks = append(checks, map[string]any{
			"name":   "endpoint",
			"ok":     strings.TrimSpace(c.profile.Endpoint) != "",
			"detail": strings.TrimSpace(c.profile.Endpoint),
		})
		if strings.TrimSpace(c.profile.Endpoint) == "" {
			valid = false
		}
		checks = append(checks, map[string]any{
			"name":   "region",
			"ok":     strings.TrimSpace(c.profile.Region) != "",
			"detail": strings.TrimSpace(c.profile.Region),
		})
		if strings.TrimSpace(c.profile.Region) == "" {
			valid = false
		}
	}
	if len(bytesTrimSpaceLocal(body)) > 0 && string(bytesTrimSpaceLocal(body)) != "{}" {
		if specialHandled {
			checks = append(checks, map[string]any{
				"name": "body_codec",
				"ok":   true,
			})
		} else {
			if _, err := util.UnmarshalJSON(body); err != nil {
				checks = append(checks, map[string]any{
					"name":   "body_json",
					"ok":     false,
					"detail": err.Error(),
				})
				valid = false
			} else {
				checks = append(checks, map[string]any{
					"name": "body_json",
					"ok":   true,
				})
			}
		}
	}

	queryKeys := make([]string, 0, len(query))
	for k := range query {
		queryKeys = append(queryKeys, strings.TrimSpace(k))
	}
	sort.Strings(queryKeys)
	headers := redactedHeaderKeys(header)
	plan := map[string]any{
		"type":             "plan",
		"method":           strings.ToUpper(strings.TrimSpace(method)),
		"path":             strings.TrimSpace(path),
		"query_keys":       queryKeys,
		"headers_redacted": headers,
		"body_sha256":      sha256Hex(body),
		"checks":           checks,
		"valid":            valid,
	}
	plan["request_preview"] = buildDryRunRequestPreview(query, previewBody, previewFormat, specialHandled)
	return plan
}

func originalRequestPreview(originalBody []byte, adaptedBody []byte, specialHandled bool) []byte {
	if specialHandled {
		return append([]byte(nil), originalBody...)
	}
	return append([]byte(nil), adaptedBody...)
}

func buildDryRunRequestPreview(query map[string]string, body []byte, format requestFormat, specialHandled bool) map[string]any {
	preview := map[string]any{
		"query": cloneStringMap(query),
	}
	if specialHandled {
		preview["body_source"] = "input_before_special_io"
	}
	if v, ok := decodeDryRunPreviewBody(body, format); ok {
		preview["body"] = v
	}
	return preview
}

func decodeDryRunPreviewBody(body []byte, format requestFormat) (any, bool) {
	trimmed := bytesTrimSpaceLocal(body)
	if len(trimmed) == 0 {
		return map[string]any{}, true
	}
	if normalizeRequestFormat(format) == requestFormatJSONL {
		rows, err := parseJSONLObjects(trimmed)
		if err != nil {
			return string(trimmed), true
		}
		return rows, true
	}
	v, err := util.UnmarshalJSON(trimmed)
	if err != nil {
		return string(trimmed), true
	}
	return v, true
}

func redactedHeaderKeys(header map[string]string) []string {
	seen := map[string]struct{}{
		"Authorization":    {},
		"X-Security-Token": {},
	}
	for k := range header {
		kk := strings.TrimSpace(k)
		if kk == "" {
			continue
		}
		seen[kk] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func bytesTrimSpaceLocal(b []byte) []byte {
	i := 0
	j := len(b)
	for i < j {
		if b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t' {
			i++
			continue
		}
		break
	}
	for j > i {
		if b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t' {
			j--
			continue
		}
		break
	}
	return b[i:j]
}

func (c *Context) validateDryRunScope(group string, rest []string) error {
	if !c.DryRun {
		return nil
	}
	switch strings.TrimSpace(group) {
	case "raw":
		return nil
	case "tool":
		if len(rest) > 0 && strings.TrimSpace(rest[0]) == "exec" {
			return nil
		}
	case "workflow":
		if len(rest) > 0 && strings.TrimSpace(rest[0]) == "exec" {
			return nil
		}
	}
	return errors.New("--dry-run currently supports raw, tool exec, and workflow exec only")
}

func (c *Context) Close() {
	if c.traceW != nil {
		_ = c.traceW.Close()
		c.traceW = nil
	}
}
