package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
	"github.com/volcengine-tls/ve-tls-cli/internal/version"
)

func runAssistant(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageAssistant(), nil, shortcutCommandHelpLookup("assistant"), func(command string, commandArgs []string) (any, error) {
		ctx.Action = "assistant." + strings.TrimSpace(command)
		if out, handled, err := maybeHandleShortcutMeta("assistant", command, commandArgs); handled {
			return out, err
		}
		switch command {
		case "describe-session-answer":
			return assistantDescribeSessionAnswer(ctx, commandArgs)
		default:
			return nil, errors.New("unknown assistant command: " + command)
		}
	})
}

func assistantDescribeSessionAnswer(ctx *Context, args []string) (any, error) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		return nil, &usageError{Text: usageAssistantDescribeSessionAnswer(), ExitCode: 0}
	}
	var (
		topicID     string
		questionArg string
		intent      string
		instanceID  string
		accountID   string
	)
	intent = "Text2Tls"
	for len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			return nil, &usageError{Text: usageAssistantDescribeSessionAnswer(), ExitCode: 0}
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--question":
			if len(args) < 2 {
				return nil, errors.New("missing --question value")
			}
			questionArg = args[1]
			args = args[2:]
		case "--intent":
			if len(args) < 2 {
				return nil, errors.New("missing --intent value")
			}
			intent = args[1]
			args = args[2:]
		case "--instance-id":
			if len(args) < 2 {
				return nil, errors.New("missing --instance-id value")
			}
			instanceID = args[1]
			args = args[2:]
		case "--account-id":
			if len(args) < 2 {
				return nil, errors.New("missing --account-id value")
			}
			accountID = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	q, err := util.ReadStringMaybeFile(questionArg)
	if err != nil {
		return nil, err
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, errors.New("missing --question")
	}
	intent = strings.TrimSpace(intent)
	if intent == "" {
		intent = "Text2Tls"
	}
	instanceID = strings.TrimSpace(instanceID)
	accountID = strings.TrimSpace(accountID)

	if err := ctx.ResolveProfile(); err != nil {
		return nil, err
	}
	if instanceID == "" {
		instanceID = strings.TrimSpace(os.Getenv("TLS_AI_ASSISTANT_INSTANCE_ID"))
	}
	if accountID == "" {
		accountID = strings.TrimSpace(os.Getenv("LOG_SERVICE_ACCOUNT_ID"))
	}
	if instanceID == "" {
		if accountID == "" {
			return nil, errors.New("missing --instance-id (or env TLS_AI_ASSISTANT_INSTANCE_ID) and missing --account-id (or env LOG_SERVICE_ACCOUNT_ID)")
		}
		id, err := assistantGetOrCreateInstanceID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		instanceID = id
	}

	sessionID, err := assistantCreateSession(ctx, instanceID, topicID)
	if err != nil {
		return nil, err
	}
	answer, err := assistantStreamAnswer(ctx, instanceID, topicID, sessionID, questionArg, intent)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"InstanceId": instanceID,
		"SessionId":  sessionID,
		"TopicId":    topicID,
		"Intent":     intent,
		"Answer":     answer,
	}, nil
}

func assistantGetOrCreateInstanceID(ctx *Context, accountID string) (string, error) {
	out, err := ctx.Do("POST", "/DescribeAppInstances", nil, nil, mustJSON(map[string]any{
		"PageNumber":   1,
		"PageSize":     10,
		"InstanceType": "AiAssistant",
		"InstanceName": accountID,
	}))
	if err == nil {
		if id := assistantFindInstanceID(out, accountID); strings.TrimSpace(id) != "" {
			return id, nil
		}
	}

	out, err = ctx.Do("POST", "/CreateAppInstance", nil, nil, mustJSON(map[string]any{
		"InstanceType": "AiAssistant",
		"InstanceName": accountID,
	}))
	if err != nil {
		return "", err
	}
	if id := assistantGetString(out, "InstanceID"); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id), nil
	}
	if id := assistantGetString(out, "InstanceId"); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id), nil
	}
	return "", errors.New("empty InstanceId from CreateAppInstance")
}

func assistantFindInstanceID(out any, accountID string) string {
	m, ok := out.(map[string]any)
	if !ok {
		return ""
	}
	raw, ok := m["InstanceInfo"]
	if !ok {
		return ""
	}
	a, ok := raw.([]any)
	if !ok {
		return ""
	}
	for _, item := range a {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(im["InstanceName"])) != strings.TrimSpace(accountID) {
			continue
		}
		if id := strings.TrimSpace(asString(im["InstanceId"])); id != "" {
			return id
		}
		if id := strings.TrimSpace(asString(im["InstanceID"])); id != "" {
			return id
		}
	}
	return ""
}

func assistantCreateSession(ctx *Context, instanceID, topicID string) (string, error) {
	out, err := ctx.Do("POST", "/CreateAppSceneMeta", nil, nil, mustJSON(map[string]any{
		"InstanceId":        instanceID,
		"Id":                topicID,
		"CreateAPPMetaType": "AiAssistantSession",
	}))
	if err != nil {
		return "", err
	}
	sessionID := strings.TrimSpace(assistantGetString(out, "Id"))
	if sessionID == "" {
		return "", errors.New("empty SessionId from CreateAppSceneMeta")
	}
	return sessionID, nil
}

func assistantStreamAnswer(ctx *Context, instanceID, topicID, sessionID, question, intent string) (string, error) {
	cl, err := ctx.Client()
	if err != nil {
		return "", err
	}
	q, err := util.ReadStringMaybeFile(question)
	if err != nil {
		return "", err
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return "", errors.New("empty question")
	}
	body := mustJSON(map[string]any{
		"InstanceId": instanceID,
		"TopicId":    topicID,
		"SessionId":  sessionID,
		"Question":   q,
		"Intent":     intent,
	})
	resp, err := doStream(context.Background(), cl, "POST", "/DescribeSessionAnswer", nil, nil, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	ctx.RequestID = resp.Header.Get("x-tls-requestid")
	ctx.StatusCode = resp.StatusCode
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", &httpError{
			statusCode: resp.StatusCode,
			body:       b,
			requestID:  resp.Header.Get("x-tls-requestid"),
		}
	}

	var sb strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "" || line == "[DONE]" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if s := assistantExtractAnswer(ev); s != "" {
			sb.WriteString(s)
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func assistantExtractAnswer(ev map[string]any) string {
	mt := strings.ToLower(strings.TrimSpace(asString(ev["RspMsgType"])))
	if mt != "" && mt != "inference" {
		return ""
	}
	ma, ok := ev["ModelAnswer"].(map[string]any)
	if ok {
		if s := strings.TrimSpace(asString(ma["Answer"])); s != "" {
			return s
		}
	}
	if s := strings.TrimSpace(asString(ev["Answer"])); s != "" {
		return s
	}
	return ""
}

func assistantGetString(out any, key string) string {
	m, ok := out.(map[string]any)
	if !ok {
		return ""
	}
	return asString(m[key])
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch vv := v.(type) {
	case string:
		return vv
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return strings.Trim(string(bytes.TrimSpace(b)), `"`)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

func doStream(ctx context.Context, c *tlsapi.Client, method, path string, query map[string]string, header map[string]string, body []byte) (*http.Response, error) {
	u, err := url.Parse(c.Endpoint + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	} else {
		r = bytes.NewReader([]byte{})
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return nil, err
	}
	req.Host = u.Host

	req.Header.Set("User-Agent", "volclog/"+version.Version)
	req.Header.Set("x-tls-apiversion", "0.3.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	if len(body) > 0 {
		sum := md5.Sum(body)
		req.Header.Set("Content-Md5", strings.ToUpper(hex.EncodeToString(sum[:])))
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	req = c.Creds.Sign(req)
	if strings.TrimSpace(req.Header.Get("Authorization")) == "" {
		return nil, errors.New("signing failed: missing Authorization header")
	}
	return c.HTTP.Do(req)
}
