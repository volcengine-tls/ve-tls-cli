//go:build human

package cli

import (
	"bytes"
	"net/http"
	"testing"
)

func TestTopicDataIngestSpecHiddenFromShortcutHelpAndTemplates(t *testing.T) {
	for _, command := range []string{"create", "modify"} {
		for _, flag := range []string{"--help", "--describe", "--print-request-template=full"} {
			t.Run(command+"/"+flag, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				if code := Run([]string{"topic", command, flag}, &stdout, &stderr); code != 0 {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				assertTopicIngestSpecHidden(t, stdout.String()+stderr.String())
			})
		}
	}
}

func TestTopicDataIngestSpecShortcutRequestRejectedBeforeSend(t *testing.T) {
	_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{"create", []string{"--request", `{"ProjectId":"p","TopicName":"demo","Ttl":30,"MeteringMode":"ChargeByDataIngest","DataIngestSpec":"D7"}`}},
		{"modify", []string{"--topic-id", "t", "--request", `{"DataIngestSpec":null}`}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			args := append([]string{"--allow-write", "topic", tc.command}, tc.args...)
			code, env := outcomeCLIRun(t, args)
			if code == 0 || calls.Load() != 0 {
				t.Fatalf("restricted request sent or accepted: exit=%d calls=%d result=%#v", code, calls.Load(), env)
			}
			assertOutcomeCLIEnvelope(t, env, "not_sent", "success", "unsupported_feature", "")
		})
	}
}

func TestTopicDataIngestSpecHiddenFromShortcutQuery(t *testing.T) {
	outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"TopicId":"t","Ttl":7,"DataIngestSpec":"D7"}`))
	})
	code, env := outcomeCLIRun(t, []string{"topic", "get", "--topic-id", "t"})
	data := c5Object(t, env["data"], "data")
	if _, exists := data["DataIngestSpec"]; code != 0 || exists || data["TopicId"] != "t" || data["Ttl"] != float64(7) {
		t.Fatalf("exit=%d result=%#v", code, env)
	}
}
