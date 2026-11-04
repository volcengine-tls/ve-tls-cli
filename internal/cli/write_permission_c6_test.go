package cli

import (
	"net/http"
	"testing"
)

func TestC6BusinessWritePermissionIsExplicit(t *testing.T) {
	create := []string{"tool", "exec", "project.create", "--input", `{"ProjectName":"demo","Region":"cn-beijing"}`}
	for _, tc := range []struct {
		name            string
		args            []string
		allowed, dryRun bool
	}{
		{"write denied", create, false, false},
		{"global permit", append([]string{"--allow-write"}, create...), true, false},
		{"trailing permit", append(append([]string{}, create...), "--allow-write"), true, false},
		{"context permit", append(append([]string{}, create...), "--context", `{"execution":{"allow_write":true}}`), true, false},
		{"context false", append(append([]string{}, create...), "--context", `{"execution":{"allow_write":false}}`), false, false},
		{"dry-run without permit", append([]string{"--dry-run"}, create...), true, true},
		{"raw write denied", []string{"raw", "--method", "POST", "--path", "/CreateProject", "--body", `{"ProjectName":"demo","Region":"cn-beijing"}`}, false, false},
		{"raw unknown GET denied", []string{"raw", "--path", "/UnknownAction"}, false, false},
		{"raw unknown explicitly permitted", []string{"--allow-write", "raw", "--path", "/UnknownAction"}, true, false},
		{"known GET", []string{"tool", "exec", "project.describe-projects"}, true, false},
		{"known POST read", []string{"tool", "exec", "log.search", "--input", `{"TopicId":"t","Query":"*","StartTime":1,"EndTime":2}`}, true, false},
		{"unverified operation denied", []string{"tool", "exec", "log.describe-cursor-time", "--input", `{"TopicId":"t","ShardId":0,"Cursor":"cursor"}`}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"ProjectId":"p","Projects":[],"Total":0,"Logs":[],"ResultStatus":"complete","ListOver":true}`))
			})
			code, env := outcomeCLIRun(t, tc.args)
			if !tc.allowed {
				if code != 1 || calls.Load() != 0 {
					t.Fatalf("denied request: exit=%d calls=%d env=%#v", code, calls.Load(), env)
				}
				assertOutcomeCLIEnvelope(t, env, "not_sent", "success", "permission", "WritePermissionRequired")
				return
			}
			wantCalls := int32(1)
			remote := "success"
			if tc.dryRun {
				wantCalls, remote = 0, "not_sent"
			}
			if code != 0 || calls.Load() != wantCalls {
				t.Fatalf("allowed request: exit=%d calls=%d env=%#v", code, calls.Load(), env)
			}
			assertOutcomeCLIEnvelope(t, env, remote, "success", "", "")
		})
	}
}

func TestC6ExplicitControlConflictDoesNotSend(t *testing.T) {
	_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	for _, args := range [][]string{
		{"--allow-write", "tool", "exec", "project.create", "--input", `{"ProjectName":"demo","Region":"cn-beijing"}`, "--context", `{"execution":{"allow_write":false}}`},
		{"--timeout", "1s", "tool", "exec", "project.describe-projects", "--context", `{"execution":{"timeout":"2s"}}`},
		{"--non-interactive", "tool", "exec", "project.describe-projects", "--context", `{"execution":{"non_interactive":false}}`},
	} {
		code, env := outcomeCLIRun(t, args)
		if code != 1 || calls.Load() != 0 || env["error"].(map[string]any)["kind"] != "validation" {
			t.Fatalf("conflict: exit=%d calls=%d env=%#v", code, calls.Load(), env)
		}
	}
}
