package cli

import (
	"encoding/json"
	"net/url"
	"path"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

const topicIngestSpecField = "DataIngestSpec"

func topicAPIPath(requestPath string) string {
	u, err := url.ParseRequestURI(requestPath)
	if err != nil {
		return ""
	}
	return strings.ToLower(path.Clean(u.Path))
}

func validateTopicWrite(requestPath string, body []byte) error {
	switch topicAPIPath(requestPath) {
	case "/createtopic", "/modifytopic":
	default:
		return nil
	}
	if len(body) == 0 {
		return nil
	}

	var fields map[string]json.RawMessage
	if err := jsonx.Unmarshal(body, &fields); err != nil {
		return &execution.Failure{
			Kind: "validation", Code: "InvalidRequest", Stage: "validate", Outcome: execution.OutcomeNotSent,
			Message: "Topic request body must be a JSON object", Cause: err,
		}
	}
	for field := range fields {
		if strings.EqualFold(field, topicIngestSpecField) {
			return &execution.Failure{
				Stage: "validate", Outcome: execution.OutcomeNotSent,
				Cause: newUnsupportedFeatureError("DataIngestSpec is unavailable in this CLI build", "remove DataIngestSpec from the request"),
			}
		}
	}
	return nil
}

func isTopicQueryPath(requestPath string) bool {
	switch topicAPIPath(requestPath) {
	case "/describetopic", "/describetopics", "/describeanalysistopic", "/describetopicswithindex":
		return true
	default:
		return false
	}
}

func filterTopicResponse(requestPath string, data any) (any, error) {
	if !isTopicQueryPath(requestPath) {
		return data, nil
	}
	if _, ok := data.(map[string]any); !ok {
		return nil, &execution.Failure{
			Kind: "protocol", Code: "InvalidResponse", Stage: "decode", Outcome: execution.OutcomeUnknown,
			Message: "Topic response must be a JSON object",
		}
	}
	removeTopicIngestSpec(data)
	return data, nil
}

func removeTopicIngestSpec(data any) {
	switch value := data.(type) {
	case map[string]any:
		for field, child := range value {
			if strings.EqualFold(field, topicIngestSpecField) {
				delete(value, field)
				continue
			}
			removeTopicIngestSpec(child)
		}
	case []any:
		for _, child := range value {
			removeTopicIngestSpec(child)
		}
	}
}
