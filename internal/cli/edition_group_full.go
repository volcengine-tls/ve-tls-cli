//go:build !agent

package cli

func runEditionSpecificGroup(ctx *Context, group string, args []string) (any, bool, error) {
	switch group {
	case "project":
		out, err := runProject(ctx, args)
		return out, true, err
	case "topic":
		out, err := runTopic(ctx, args)
		return out, true, err
	case "metric-topic":
		out, err := runMetricTopic(ctx, args)
		return out, true, err
	case "index":
		out, err := runIndex(ctx, args)
		return out, true, err
	case "log":
		out, err := runLog(ctx, args)
		return out, true, err
	case "host-group":
		out, err := runHostGroup(ctx, args)
		return out, true, err
	case "collector":
		out, err := runCollector(ctx, args)
		return out, true, err
	case "assistant":
		out, err := runAssistant(ctx, args)
		return out, true, err
	default:
		return nil, false, nil
	}
}
