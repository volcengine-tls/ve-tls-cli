package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestClosedStdoutReportsConfirmedWrite(t *testing.T) {
	const helperEnv = "VOLCLOG_TEST_CLOSED_STDOUT_HELPER"
	if os.Getenv(helperEnv) == "1" {
		os.Args = []string{"volclog", "tool", "exec", "project.create", "--input", `{"ProjectName":"test","Region":"cn-beijing"}`}
		main()
		return
	}
	var calls atomic.Int32
	stdoutClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-stdoutClosed
		w.Header().Set("x-tls-requestid", "confirmed-write")
		_, _ = w.Write([]byte(`{"ProjectId":"p1"}`))
	}))
	defer server.Close()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "fake-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "fake-sk")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClosedStdoutReportsConfirmedWrite$")
	command.Env = append(os.Environ(), helperEnv+"=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	reader, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		close(stdoutClosed)
		t.Fatal(err)
	}
	_ = reader.Close()
	close(stdoutClosed)
	err = command.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 || calls.Load() != 1 {
		t.Fatalf("exit=%v calls=%d stderr=%s", err, calls.Load(), &stderr)
	}
	var result map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &result); err != nil {
		t.Fatalf("decode stderr: %v: %s", err, &stderr)
	}
	summary, _ := result["summary"].(map[string]any)
	failure, _ := result["error"].(map[string]any)
	if summary["remoteOutcome"] != "success" || summary["deliveryOutcome"] != "failed" || failure["kind"] != "output_failed" || result["requestId"] != "confirmed-write" {
		t.Fatalf("lost write evidence after closed stdout: %s", &stderr)
	}
}
