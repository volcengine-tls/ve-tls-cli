package cli

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

func parseInvocationTimeout(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || duration <= 0 {
		return 0, errors.New("timeout must be a positive Go duration, for example 30s or 2m")
	}
	return duration, nil
}

func (c *Context) applyExecutionControls(cfg toolExecContext) error {
	var timeout time.Duration
	if value, exists := cfg.Execution["timeout"]; exists {
		var err error
		timeout, err = parseInvocationTimeout(value.(string))
		if err != nil {
			return &toolContextValidationError{Path: "$.execution.timeout", Reason: err.Error()}
		}
		if c.TimeoutExplicit && c.Timeout != timeout {
			return &toolContextValidationError{Path: "$.execution.timeout", Reason: "conflicts with global --timeout"}
		}
	}
	if value, ok := cfg.Execution["allow_write"].(bool); ok && c.AllowWriteExplicit && c.AllowWrite != value {
		return &toolContextValidationError{Path: "$.execution.allow_write", Reason: "conflicts with global --allow-write"}
	}
	if value, ok := cfg.Execution["non_interactive"].(bool); ok && c.NonInteractiveExplicit && c.NonInteractive != value {
		return &toolContextValidationError{Path: "$.execution.non_interactive", Reason: "conflicts with global --non-interactive"}
	}
	if value, ok := cfg.Execution["allow_write"].(bool); ok {
		c.AllowWrite = value
	}
	if value, ok := cfg.Execution["non_interactive"].(bool); ok && value {
		c.NonInteractive = true
	}
	if timeout > 0 {
		c.Timeout, c.TimeoutExplicit = timeout, true
		c.setExecutionTimeout(timeout)
	}
	return nil
}

// A reviewed read is the only default permission. HTTP method alone cannot
// establish this: SearchLogs is POST, and unknown GET routes can have effects.
func (c *Context) authorizeBusinessRequest(method, path string) error {
	if c.AllowWrite || c.DryRun {
		return nil
	}
	parsed, err := url.ParseRequestURI(path)
	if err == nil && parsed.Scheme == "" && parsed.Host == "" && parsed.Fragment == "" {
		catalog, err := loadOperationCatalog()
		if err != nil {
			return &execution.Failure{Kind: "config", Code: "InvalidCatalog", Stage: "authorize", Outcome: execution.OutcomeNotSent, Message: "cannot load operation permissions", Cause: err}
		}
		for _, op := range catalog.Operations {
			if strings.EqualFold(op.Wire.Method, method) && op.Wire.Path == parsed.Path && operationIsReviewedRead(op) {
				return nil
			}
		}
	}
	return &execution.Failure{Kind: "permission", Code: "WritePermissionRequired", Stage: "authorize", Outcome: execution.OutcomeNotSent, Message: "operation requires explicit write permission; use --allow-write or context.execution.allow_write=true, or inspect it with --dry-run"}
}

func operationIsReviewedRead(op contract.Operation) bool {
	return op.Risk.RecoverySource == "reviewed-read-contract" && op.Risk.ErrorRecovery == "safe-retry"
}
