package runner

import (
	"errors"
	"strings"
)

type Request struct {
	Account        string         `json:"account"`
	Region         string         `json:"region"`
	Action         string         `json:"action"`
	Args           map[string]any `json:"args,omitempty"`
	Output         string         `json:"output,omitempty"`
	DryRun         bool           `json:"dry_run,omitempty"`
	ConfirmToken   string         `json:"confirm_token,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

func RequestFromText(text string) (Request, error) {
	m, err := ParseTextArgs(text)
	if err != nil {
		return Request{}, err
	}
	_, dryRunSet := m["dry_run"]
	req := Request{
		Account: strings.TrimSpace(m["account"]),
		Region:  strings.TrimSpace(m["region"]),
		Action:  strings.TrimSpace(m["action"]),
		Output:  strings.TrimSpace(m["output"]),
		Args:    map[string]any{},
	}
	if v := strings.TrimSpace(m["dry_run"]); v != "" {
		req.DryRun = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
	}
	if v := strings.TrimSpace(m["confirm_token"]); v != "" {
		req.ConfirmToken = v
	}
	if v := strings.TrimSpace(m["idempotency_key"]); v != "" {
		req.IdempotencyKey = v
	}
	for k, v := range m {
		switch k {
		case "account", "region", "action", "output", "dry_run", "confirm_token", "idempotency_key":
			continue
		default:
			req.Args[k] = v
		}
	}
	if req.Account == "" || req.Region == "" || req.Action == "" {
		return Request{}, errors.New("missing required fields: account, region, action")
	}
	if !dryRunSet {
		if spec, ok := GetActionSpec(req.Action); ok && spec.ConfirmRequired {
			req.DryRun = true
		}
	}
	return req, nil
}
