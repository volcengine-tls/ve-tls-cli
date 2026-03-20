package cli

import (
	"context"
	"io"
	"time"

	"volclog/internal/config"
	"volclog/internal/output"
	"volclog/internal/tlsapi"
)

type Context struct {
	Stdout      io.Writer
	Stderr      io.Writer
	Format      output.Format
	Profile     string
	Filter      string
	Debug       bool
	TraceDir    string
	TraceRedact string
	TracePath   string
	RequestID   string
	StatusCode  int

	cfg      config.Config
	cfgPath  string
	profile  config.Profile
	client   *tlsapi.Client
	traceW   io.WriteCloser
	defaults config.ProfileDefaults
}

func newContext(stdout, stderr io.Writer, format output.Format, profile, filter string, debug bool) *Context {
	return &Context{
		Stdout:  stdout,
		Stderr:  stderr,
		Format:  format,
		Profile: profile,
		Filter:  filter,
		Debug:   debug,
	}
}

func (c *Context) LoadConfig() error {
	cfg, p, err := config.Load()
	if err != nil {
		return err
	}
	c.cfg = cfg
	c.cfgPath = p
	return nil
}

func (c *Context) SaveConfig() error {
	return config.Save(c.cfg, c.cfgPath)
}

func (c *Context) ResolveProfile() error {
	if c.cfg.Version == 0 {
		if err := c.LoadConfig(); err != nil {
			return err
		}
	}
	p, err := config.EffectiveProfile(c.cfg, c.Profile, c.defaults)
	if err != nil {
		return err
	}
	c.profile = p
	return nil
}

func (c *Context) SetProfileDefaults(d config.ProfileDefaults) {
	c.defaults = d
}

func (c *Context) Client() (*tlsapi.Client, error) {
	if c.client != nil {
		return c.client, nil
	}
	if c.profile.AccessKeyID == "" {
		if err := c.ResolveProfile(); err != nil {
			return nil, err
		}
	}
	t := time.Duration(c.profile.TimeoutSeconds) * time.Second
	cl, err := tlsapi.New(c.profile.Endpoint, c.profile.Region, c.profile.AccessKeyID, c.profile.SecretAccessKey, c.profile.SecurityToken, t)
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
	resp, err := c.DoRaw(method, path, query, header, body)
	if err != nil {
		return nil, err
	}
	return decodeResponse(resp)
}

func (c *Context) Close() {
	if c.traceW != nil {
		_ = c.traceW.Close()
		c.traceW = nil
	}
}
