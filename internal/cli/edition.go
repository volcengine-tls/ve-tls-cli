package cli

import "strings"

type cliEdition string

const (
	cliEditionVolclog      cliEdition = "volclog"
	cliEditionVolclogHuman cliEdition = "volclog-human"
)

var volclogPrimaryGroupNames = map[string]bool{
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

var hiddenVolclogHumanGroupNames = map[string]bool{
	"assistant": true,
}

func isGroupEnabledInCurrentEdition(group string) bool {
	group = strings.TrimSpace(group)
	if legacyEditionAgnosticGroupNames[group] {
		return true
	}
	if currentEdition() != cliEditionVolclog {
		return true
	}
	return volclogPrimaryGroupNames[group]
}

func isRecognizedGroup(group string) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return false
	}
	if legacyEditionAgnosticGroupNames[group] || hiddenVolclogHumanGroupNames[group] {
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
		return "use one of configure/doctor/skill/tool/workflow/raw in the default volclog build"
	}
	return "the default volclog build only exposes configure/doctor/skill/tool/workflow/raw; use 'volclog tool list', 'volclog workflow list', or switch to the volclog-human build (-tags=human) for human shortcuts"
}
