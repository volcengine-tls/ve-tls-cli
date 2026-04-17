package cli

import "strings"

type cliEdition string

const (
	cliEditionFull  cliEdition = "full"
	cliEditionAgent cliEdition = "agent"
)

var agentEditionGroupNames = map[string]bool{
	"configure": true,
	"doctor":    true,
	"skill":     true,
	"tool":      true,
	"workflow":  true,
	"raw":       true,
}

var legacyEditionAgnosticGroupNames = map[string]bool{
	"api":          true,
	"capabilities": true,
}

var hiddenFullEditionGroupNames = map[string]bool{
	"assistant": true,
}

func isGroupEnabledInCurrentEdition(group string) bool {
	group = strings.TrimSpace(group)
	if legacyEditionAgnosticGroupNames[group] {
		return true
	}
	if currentEdition() != cliEditionAgent {
		return true
	}
	return agentEditionGroupNames[group]
}

func isRecognizedGroup(group string) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return false
	}
	if legacyEditionAgnosticGroupNames[group] || hiddenFullEditionGroupNames[group] {
		return true
	}
	for _, item := range visibleCliGroups() {
		if item.Name == group {
			return true
		}
	}
	return false
}

func editionGroupHint(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return "use one of configure/doctor/skill/tool/workflow/raw in this edition"
	}
	return "this edition only exposes configure/doctor/skill/tool/workflow/raw; use 'volclog tool list', 'volclog workflow list', or switch to the full volclog build for human shortcuts"
}
