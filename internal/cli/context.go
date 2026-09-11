package cli

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"

	appruntime "github.com/volcengine-tls/ve-tls-cli/internal/app/runtime"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

type InvocationOptions struct {
	Stdout             io.Writer
	Stderr             io.Writer
	Format             output.Format
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
	DryRun             bool
}

type ResponseState struct {
	FormatOverride output.Format
	TracePath      string
	RequestID      string
	StatusCode     int
	Action         string
	PaginationMeta map[string]any
}

type configState struct {
	cfg     config.Config
	cfgPath string
}

type runtimeState struct {
	profile    config.Profile
	resolution appruntime.Resolution
	client     *tlsapi.Client
	defaults   config.ProfileDefaults

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
	// runtime.DefaultProviderFactory.
	authFactory authProviderFactory
}

type traceState struct {
	traceW io.WriteCloser
}

type Context struct {
	InvocationOptions
	ResponseState
	configState
	runtimeState
	traceState

	apiIOMeta apiIOMeta
}

func newContext(stdout, stderr io.Writer, format output.Format, profile, filter string) *Context {
	return &Context{
		InvocationOptions: InvocationOptions{
			Stdout:  stdout,
			Stderr:  stderr,
			Format:  format,
			Profile: profile,
			Filter:  filter,
		},
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
	resolution, err := appruntime.Resolve(appruntime.ResolveRequest{
		Config:          c.cfg,
		ExplicitProfile: c.Profile,
		Defaults:        c.defaults,
		RuntimeRegion:   c.RuntimeRegion,
		RuntimeEndpoint: c.RuntimeEndpoint,
		ForceStatic:     c.forceStaticAuth,
	})
	if err != nil {
		return err
	}
	c.resolution = resolution
	c.profile = resolution.Profile
	c.profileResolved = true
	return nil
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
	c.resolution = appruntime.Resolution{}
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
	resolution := c.resolution
	if strings.TrimSpace(resolution.ProfileName) == "" {
		mode, err := config.NormalizeAuthMode(c.profile.Mode)
		if err != nil {
			return nil, err
		}
		resolution = appruntime.Resolution{
			ProfileName: c.cfg.SelectedProfileName(c.Profile),
			Profile:     c.profile,
			Mode:        mode,
			Dynamic:     !c.forceStaticAuth && config.IsProviderAuthMode(mode),
			ForceStatic: c.forceStaticAuth,
		}
	}
	cl, err := appruntime.BuildClient(appruntime.BuildClientRequest{
		Mode:        resolution.Mode,
		ConfigPath:  c.cfgPath,
		ProfileName: resolution.ProfileName,
		Config:      c.cfg,
		Profile:     resolution.Profile,
		SDKProfile:  c.Profile,
		ForceStatic: resolution.ForceStatic,
		Factory:     c.authFactory,
	})
	if err != nil {
		return nil, err
	}
	c.client = cl
	return cl, nil
}

func (c *Context) DoRaw(method, path string, query map[string]string, header map[string]string, body []byte) (tlsapi.Response, error) {
	return c.doRaw(context.Background(), method, path, query, header, body)
}

func (c *Context) doRaw(ctx context.Context, method, path string, query map[string]string, header map[string]string, body []byte) (tlsapi.Response, error) {
	client, err := c.Client()
	if err != nil {
		return tlsapi.Response{}, err
	}
	transport := appruntime.NewTracingTransport(
		appruntime.NewTransport(func() (*tlsapi.Client, error) {
			return client, nil
		}),
		contextRuntimeTracer{context: c},
	)
	response, err := transport.Do(ctx, execution.Request{
		Method: method,
		Path:   path,
		Query:  query,
		Header: header,
		Body:   body,
	})
	if err != nil {
		return tlsapi.Response{}, err
	}
	resp := tlsapi.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header,
		Body:       response.Body,
	}
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
