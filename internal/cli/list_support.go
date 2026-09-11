package cli

import (
	"strings"
)

func supportsTableOutput(ctx *Context) bool {
	switch strings.TrimSpace(ctx.Action) {
	case "project.list", "project.get",
		"topic.list", "topic.get",
		"metric-topic.list", "metric-topic.get",
		"index.get", "log.search":
		return true
	default:
		return false
	}
}
