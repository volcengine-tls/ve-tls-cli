package runner

import (
	"errors"
	"strings"
)

type ActionSpec struct {
	ActionKey       string
	RequiredArgs    []string
	ForbiddenCombos [][2]string
	OutputDefault   string
	ConfirmRequired bool
}

func GetActionSpec(action string) (ActionSpec, bool) {
	key := canonicalActionKey(action)
	spec, ok := actionSpecs[key]
	return spec, ok
}

func canonicalActionKey(action string) string {
	a := strings.TrimSpace(action)
	if a == "" {
		return ""
	}
	a = strings.ToLower(a)
	a = strings.ReplaceAll(a, "-", "_")
	return a
}

func validateArgs(spec ActionSpec, args map[string]any) error {
	for _, k := range spec.RequiredArgs {
		if strings.TrimSpace(toString(args[k])) == "" {
			return errors.New("missing required arg: " + k)
		}
	}
	for _, pair := range spec.ForbiddenCombos {
		a := strings.TrimSpace(toString(args[pair[0]]))
		b := strings.TrimSpace(toString(args[pair[1]]))
		if a != "" && b != "" {
			return errors.New(pair[0] + " and " + pair[1] + " cannot be provided together")
		}
	}
	return nil
}

var actionSpecs = map[string]ActionSpec{
	"project.list":   {ActionKey: "project.list"},
	"project.get":    {ActionKey: "project.get", RequiredArgs: []string{"project_id"}},
	"project.create": {ActionKey: "project.create", RequiredArgs: []string{"project_name"}},
	"project.modify": {ActionKey: "project.modify", RequiredArgs: []string{"project_id"}, ConfirmRequired: true},
	"project.delete": {ActionKey: "project.delete", RequiredArgs: []string{"project_id"}, ConfirmRequired: true},

	"topic.list":   {ActionKey: "topic.list", ForbiddenCombos: [][2]string{{"topic_name", "topic_id"}}},
	"topic.get":    {ActionKey: "topic.get", RequiredArgs: []string{"topic_id"}},
	"topic.create": {ActionKey: "topic.create", RequiredArgs: []string{"project_id", "topic_name"}, ConfirmRequired: true},
	"topic.modify": {ActionKey: "topic.modify", RequiredArgs: []string{"topic_id"}, ConfirmRequired: true},
	"topic.delete": {ActionKey: "topic.delete", RequiredArgs: []string{"topic_id"}, ConfirmRequired: true},

	"metric_topic.list":   {ActionKey: "metric_topic.list", ForbiddenCombos: [][2]string{{"topic_name", "topic_id"}}},
	"metric_topic.get":    {ActionKey: "metric_topic.get", RequiredArgs: []string{"topic_id"}},
	"metric_topic.create": {ActionKey: "metric_topic.create", RequiredArgs: []string{"project_id", "topic_name"}, ConfirmRequired: true},
	"metric_topic.modify": {ActionKey: "metric_topic.modify", RequiredArgs: []string{"topic_id"}, ConfirmRequired: true},
	"metric_topic.delete": {ActionKey: "metric_topic.delete", RequiredArgs: []string{"topic_id"}, ConfirmRequired: true},
	"metric_topic.search": {ActionKey: "metric_topic.search", RequiredArgs: []string{"topic_id", "query"}},

	"metric_topic.prom.query":        {ActionKey: "metric_topic.prom.query", RequiredArgs: []string{"topic_id", "query"}},
	"metric_topic.prom.query_range":  {ActionKey: "metric_topic.prom.query_range", RequiredArgs: []string{"topic_id", "query", "start_ms", "end_ms", "step"}},
	"metric_topic.prom.series":       {ActionKey: "metric_topic.prom.series", RequiredArgs: []string{"topic_id", "start_ms", "end_ms", "match"}},
	"metric_topic.prom.labels":       {ActionKey: "metric_topic.prom.labels", RequiredArgs: []string{"topic_id"}},
	"metric_topic.prom.label_values": {ActionKey: "metric_topic.prom.label_values", RequiredArgs: []string{"topic_id", "label_name"}},

	"index.get":    {ActionKey: "index.get", RequiredArgs: []string{"topic_id"}},
	"index.create": {ActionKey: "index.create", RequiredArgs: []string{"topic_id", "body"}, ConfirmRequired: true},
	"index.modify": {ActionKey: "index.modify", RequiredArgs: []string{"topic_id", "body"}, ConfirmRequired: true},

	"log.search": {ActionKey: "log.search", RequiredArgs: []string{"topic_id", "query"}},
	"log.export": {ActionKey: "log.export", RequiredArgs: []string{"topic_id", "query"}, OutputDefault: "jsonl", ConfirmRequired: true},
}
