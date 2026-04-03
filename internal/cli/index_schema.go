package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func validateIndexBody(path string, body map[string]any) error {
	action := "CreateIndex"
	if path == "/ModifyIndex" {
		action = "ModifyIndex"
	}
	fields, required, err := indexBodyFieldSpec(action)
	if err != nil {
		return nil
	}
	if len(fields) == 0 {
		return nil
	}

	var unknown []string
	for key := range body {
		if _, ok := fields[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		msgs := make([]string, 0, len(unknown))
		for _, key := range unknown {
			msg := "unknown body field: " + key
			if suggestion := closestFieldName(key, fields); suggestion != "" {
				msg += " (did you mean " + suggestion + "?)"
			}
			msgs = append(msgs, msg)
		}
		return errors.New(strings.Join(msgs, "; "))
	}

	var missing []string
	for _, key := range required {
		if _, ok := body[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required body field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func indexBodyFieldSpec(action string) (map[string]struct{}, []string, error) {
	doc, err := loadAPICapabilities()
	if err != nil {
		return nil, nil, err
	}
	ops := buildAPIIndex(doc)["index"][normalizeActionToken(action)]
	if len(ops) == 0 {
		return nil, nil, errors.New("index action not found: " + action)
	}
	fields := map[string]struct{}{}
	requiredSet := map[string]struct{}{}
	for _, p := range ops[0].Cmd.RequestParamsDoc {
		if !strings.EqualFold(strings.TrimSpace(p.In), "body") {
			continue
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		fields[name] = struct{}{}
		if isDocRequired(p.RequiredText) {
			requiredSet[name] = struct{}{}
		}
	}
	required := make([]string, 0, len(requiredSet))
	for name := range requiredSet {
		required = append(required, name)
	}
	sort.Strings(required)
	return fields, required, nil
}

func closestFieldName(input string, candidates map[string]struct{}) string {
	best := ""
	bestScore := 1 << 30
	for candidate := range candidates {
		score := levenshtein(strings.ToLower(input), strings.ToLower(candidate))
		if score < bestScore {
			bestScore = score
			best = candidate
		}
	}
	if best == "" {
		return ""
	}
	threshold := 3
	if len(input) > 10 {
		threshold = 4
	}
	if bestScore > threshold {
		return ""
	}
	return best
}

func levenshtein(a string, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = min3(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev = curr
	}
	return prev[len(b)]
}

func min3(a int, b int, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
