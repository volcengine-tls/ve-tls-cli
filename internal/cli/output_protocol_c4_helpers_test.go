package cli

import (
	"encoding/json"
	"testing"
)

func assertFileDeliveryNotice(t *testing.T, stdout, path, wantMode string) {
	t.Helper()
	if got := parseNoticePath(t, stdout); got != path {
		t.Fatalf("delivery path=%q, want %q", got, path)
	}
	if currentEdition() == cliEditionVolclogHuman {
		var prefix string
		switch wantMode {
		case "file_forced":
			prefix = "结果已写入文件。\n文件: "
		case "file_auto":
			prefix = "结果过大，已写入文件。\n文件: "
		default:
			t.Fatalf("unexpected expected delivery mode %q", wantMode)
		}
		if want := prefix + path + "\n"; stdout != want {
			t.Fatalf("human notice=%q, want %q", stdout, want)
		}
		return
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid file delivery JSON: %v", err)
	}
	summary := env["summary"].(map[string]any)
	if got := summary["deliveryMode"]; got != wantMode {
		t.Fatalf("deliveryMode=%#v, want %q", got, wantMode)
	}
}
