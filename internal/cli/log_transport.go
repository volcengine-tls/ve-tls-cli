package cli

import (
	"strings"
)

func maybeSetHeader(dst map[string]string, key string, value string) {
	if strings.TrimSpace(value) != "" {
		dst[key] = strings.TrimSpace(value)
	}
}

func doPutLogs(ctx *Context, topicID string, requestFmt requestFormat, header map[string]string, body []byte) (any, error) {
	ctx.apiIOMeta = apiIOMeta{
		Group:         "log",
		Action:        "PutLogs",
		Method:        "POST",
		Path:          "/PutLogs",
		RequestFormat: requestFmt,
		OutputFormat:  ctx.Format,
		OutputMode:    ctx.OutputMode,
	}
	return ctx.Do("POST", "/PutLogs", map[string]string{"TopicId": topicID}, header, body)
}
