package cli

import (
	"context"
	"time"
)

// invocationControlState owns one invocation's cancellation and absolute
// timeout budget. It is embedded by Context alongside the invocation options.
type invocationControlState struct {
	baseCtx         context.Context
	executionCtx    context.Context
	executionCancel context.CancelFunc
	startedAt       time.Time
}

// ExecutionContext returns the invocation context, with a safe zero-value
// fallback for existing callers that construct Context directly.
func (c *Context) ExecutionContext() context.Context {
	if c != nil && c.executionCtx != nil {
		return c.executionCtx
	}
	return context.Background()
}

func (c *Context) initializeExecutionContext(parent context.Context, startedAt time.Time) {
	if c == nil {
		return
	}
	c.closeExecutionContext()
	if parent == nil {
		parent = context.Background()
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	c.baseCtx = parent
	c.startedAt = startedAt
	c.executionCtx, c.executionCancel = context.WithCancel(parent)
}

// setExecutionTimeout always measures from invocation start. Reapplying a
// deadline cannot extend or reset a budget that is already in force.
func (c *Context) setExecutionTimeout(timeout time.Duration) {
	if c == nil || timeout <= 0 {
		return
	}
	if c.executionCtx == nil {
		c.initializeExecutionContext(context.Background(), time.Now())
	}
	deadline := c.startedAt.Add(timeout)
	if current, ok := c.executionCtx.Deadline(); ok && !deadline.Before(current) {
		return
	}
	previousCancel := c.executionCancel
	var cancel context.CancelFunc
	c.executionCtx, cancel = context.WithDeadline(c.executionCtx, deadline)
	c.executionCancel = func() {
		cancel()
		if previousCancel != nil {
			previousCancel()
		}
	}
}

func (c *Context) closeExecutionContext() {
	if c != nil && c.executionCancel != nil {
		c.executionCancel()
		c.executionCancel = nil
	}
}
