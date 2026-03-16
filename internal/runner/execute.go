package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type Response struct {
	Data  any        `json:"data,omitempty"`
	Plan  *Plan      `json:"plan,omitempty"`
	Error *RespError `json:"error,omitempty"`
	Audit *Audit     `json:"audit,omitempty"`
}

type Plan struct {
	Profile         string     `json:"profile,omitempty"`
	Commands        [][]string `json:"commands,omitempty"`
	ConfirmRequired bool       `json:"confirm_required,omitempty"`
	ConfirmToken    string     `json:"confirm_token,omitempty"`
}

type RespError struct {
	Code       string        `json:"code"`
	Message    string        `json:"message"`
	Details    any           `json:"details,omitempty"`
	Candidates []ProfileInfo `json:"candidates,omitempty"`
	Hint       string        `json:"hint,omitempty"`
}

type Audit struct {
	Account    string   `json:"account,omitempty"`
	Region     string   `json:"region,omitempty"`
	Action     string   `json:"action,omitempty"`
	Profile    string   `json:"profile,omitempty"`
	Commands   []string `json:"commands,omitempty"`
	StatusCode int      `json:"status_code,omitempty"`
	DurationMs int64    `json:"duration_ms,omitempty"`
}

type Runner struct {
	Now func() time.Time
}

func (r Runner) Execute(ctx context.Context, req Request) Response {
	start := time.Now()
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	audit := &Audit{
		Account: req.Account,
		Region:  req.Region,
		Action:  req.Action,
	}

	spec, ok := GetActionSpec(req.Action)
	if !ok {
		return Response{Error: &RespError{Code: ErrValidation, Message: "unsupported action"}, Audit: finishAudit(audit, start)}
	}

	bin, err := EnsureTLSCTL(ctx)
	if err != nil {
		return Response{Error: &RespError{Code: ErrInternal, Message: err.Error()}, Audit: finishAudit(audit, start)}
	}

	profiles, err := listProfiles(ctx, bin)
	if err != nil {
		return Response{Error: &RespError{Code: ErrInternal, Message: err.Error()}, Audit: finishAudit(audit, start)}
	}

	accountMap, _ := loadAccountMap()
	res := ResolveProfile(req.Account, req.Region, accountMap, profiles)
	if res.Error != "" {
		return Response{
			Error: &RespError{
				Code:       res.Error,
				Message:    "cannot resolve profile",
				Candidates: res.Candidates,
				Hint:       profileHint(req.Account, req.Region),
			},
			Audit: finishAudit(audit, start),
		}
	}
	profile := res.Profile
	audit.Profile = profile

	output := strings.TrimSpace(req.Output)
	if output == "" {
		output = strings.TrimSpace(spec.OutputDefault)
	}
	if output == "" {
		output = "json"
	}

	confirmRequired := spec.ConfirmRequired
	if confirmRequired && !req.DryRun && strings.TrimSpace(req.ConfirmToken) == "" {
		return Response{
			Error: &RespError{
				Code:    ErrConfirmTokenMiss,
				Message: "confirm token required",
			},
			Audit: finishAudit(audit, start),
		}
	}

	cmdArgs, err := BuildCommand(req.Action, profile, output, req.Args)
	if err != nil {
		return Response{Error: &RespError{Code: ErrValidation, Message: err.Error()}, Audit: finishAudit(audit, start)}
	}

	plan := &Plan{
		Profile:         profile,
		Commands:        [][]string{cmdArgs},
		ConfirmRequired: confirmRequired,
	}

	if confirmRequired {
		secret := []byte(os.Getenv("TLSCTL_RUNNER_SECRET"))
		argsSig := hashArgs(req.Args)
		cReq := ConfirmRequest{Account: req.Account, Region: req.Region, Profile: profile, Action: req.Action, ArgsSig: argsSig}
		if req.DryRun {
			if len(secret) > 0 {
				tok, err := GenerateConfirmToken(secret, cReq, now, 5*time.Minute)
				if err == nil {
					plan.ConfirmToken = tok
				}
			}
		} else {
			if err := ValidateConfirmToken(secret, cReq, req.ConfirmToken, now); err != nil {
				code := err.Error()
				return Response{Error: &RespError{Code: code, Message: "confirm token invalid"}, Audit: finishAudit(audit, start)}
			}
		}
	}

	if req.DryRun {
		return Response{Plan: plan, Audit: finishAudit(audit, start)}
	}

	out, stderr, exitCode, err := runCmd(ctx, bin, cmdArgs[1:])
	audit.Commands = []string{strings.Join(cmdArgs, " ")}
	audit.StatusCode = exitCode
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return Response{Error: &RespError{Code: ErrTLSCTLError, Message: msg}, Audit: finishAudit(audit, start)}
	}
	data, err := parseTLSCTLOutput(out, output)
	if err != nil {
		return Response{Error: &RespError{Code: ErrTLSCTLError, Message: "invalid tlsctl output"}, Audit: finishAudit(audit, start)}
	}
	return Response{Data: data, Audit: finishAudit(audit, start)}
}

func finishAudit(a *Audit, start time.Time) *Audit {
	a.DurationMs = time.Since(start).Milliseconds()
	return a
}

func runCmd(ctx context.Context, bin string, args []string) (stdout []byte, stderr string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	e := cmd.Run()
	if e == nil {
		return outb.Bytes(), errb.String(), 0, nil
	}
	if ee := new(exec.ExitError); errors.As(e, &ee) {
		return outb.Bytes(), errb.String(), ee.ExitCode(), e
	}
	return outb.Bytes(), errb.String(), 1, e
}

func parseTLSCTLOutput(out []byte, format string) (any, error) {
	f := strings.ToLower(strings.TrimSpace(format))
	if f == "" || f == "json" {
		var data any
		if err := json.Unmarshal(out, &data); err != nil {
			return nil, err
		}
		return data, nil
	}
	if f == "jsonl" {
		lines := bytes.Split(out, []byte{'\n'})
		var items []any
		for _, line := range lines {
			b := bytes.TrimSpace(line)
			if len(b) == 0 {
				continue
			}
			var v any
			if err := json.Unmarshal(b, &v); err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	}
	return nil, errors.New("unsupported output format")
}

func listProfiles(ctx context.Context, bin string) ([]ProfileInfo, error) {
	out, _, _, err := runCmd(ctx, bin, []string{"--output", "json", "configure", "list"})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Profiles []ProfileInfo `json:"profiles"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	return resp.Profiles, nil
}

func profileHint(account string, region string) string {
	a := strings.TrimSpace(account)
	r := strings.TrimSpace(region)
	if a == "" || r == "" {
		return ""
	}
	return "suggest: tlsctl configure set --profile " + a + "-" + shortRegion(r) + " --ak <ak> --sk <sk> --region " + r
}

func shortRegion(region string) string {
	r := strings.TrimSpace(region)
	if r == "" {
		return ""
	}
	if strings.HasPrefix(r, "cn-") {
		return "cn"
	}
	if strings.HasPrefix(r, "ap-") {
		return "ap"
	}
	return r
}

func hashArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(toString(args[k]))
		b.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
