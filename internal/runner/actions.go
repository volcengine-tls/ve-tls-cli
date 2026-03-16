package runner

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

func BuildCommand(action string, profile string, output string, args map[string]any) ([]string, error) {
	spec, ok := GetActionSpec(action)
	if !ok {
		return nil, errors.New("unsupported action: " + strings.TrimSpace(action))
	}
	p := strings.TrimSpace(profile)
	if p == "" {
		return nil, errors.New("missing profile")
	}
	if args == nil {
		args = map[string]any{}
	}
	if err := validateArgs(spec, args); err != nil {
		return nil, err
	}

	a := canonicalActionKey(spec.ActionKey)
	parts := strings.Split(a, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid action: " + a)
	}
	for i := range parts {
		parts[i] = segmentToCLI(parts[i])
	}
	group := parts[0]
	sub := parts[1:]

	cmd := []string{"tlsctl", "--profile", p}
	outFmt := strings.TrimSpace(output)
	if outFmt == "" {
		outFmt = strings.TrimSpace(spec.OutputDefault)
	}
	if outFmt != "" {
		cmd = append(cmd, "--output", outFmt)
	}
	cmd = append(cmd, group)
	cmd = append(cmd, sub...)

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := args[k]
		flag, val, ok := argToFlag(k, v)
		if !ok {
			continue
		}
		if flag == "" {
			continue
		}
		cmd = append(cmd, flag)
		if val != "" {
			cmd = append(cmd, val)
		}
	}
	return cmd, nil
}

func segmentToCLI(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
}

func argToFlag(key string, v any) (string, string, bool) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", "", false
	}
	switch k {
	case "from_ms":
		return "--from", toString(v), true
	case "to_ms":
		return "--to", toString(v), true
	case "start_ms":
		return "--start", toString(v), true
	case "end_ms":
		return "--end", toString(v), true
	case "time_ms":
		return "--time", toString(v), true
	}
	flag := "--" + strings.ReplaceAll(k, "_", "-")
	val := toString(v)
	if val == "" {
		return "", "", false
	}
	return flag, val, true
}

func toString(v any) string {
	switch vv := v.(type) {
	case string:
		return strings.TrimSpace(vv)
	case int:
		return strconv.Itoa(vv)
	case int64:
		return strconv.FormatInt(vv, 10)
	case float64:
		if vv == float64(int64(vv)) {
			return strconv.FormatInt(int64(vv), 10)
		}
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case bool:
		if vv {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
