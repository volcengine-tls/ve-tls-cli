package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

func TestUnificationBaselineAllToolDigests(t *testing.T) {
	operations := loadToolOperations("", "", "")
	if got, want := len(operations), 159; got != want {
		t.Fatalf("tool catalog count: got %d, want %d", got, want)
	}

	digestPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	got := make(map[string]string, len(operations))
	for _, tool := range operations {
		id := string(tool.ID)
		if _, exists := got[id]; exists {
			t.Fatalf("duplicate tool ID %q", tool.ID)
		}
		digest := toolContractForDigest(tool)
		if !digestPattern.MatchString(digest) {
			t.Fatalf("tool %q digest %q is not 64-character lowercase hexadecimal", tool.ID, digest)
		}
		got[id] = digest
	}

	goldenPath := filepath.Join("testdata", "unification", "tool_contract_digests.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read tool contract digest golden %q: %v", goldenPath, err)
	}
	var want map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode tool contract digest golden %q: %v", goldenPath, err)
	}
	if reflect.DeepEqual(got, want) {
		return
	}

	var missing, extra, changed []string
	for id, wantDigest := range want {
		gotDigest, exists := got[id]
		switch {
		case !exists:
			missing = append(missing, id)
		case gotDigest != wantDigest:
			changed = append(changed, id)
		}
	}
	for id := range got {
		if _, exists := want[id]; !exists {
			extra = append(extra, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(changed)
	t.Fatalf("tool contract digest baseline mismatch: missing IDs=%v, extra IDs=%v, changed IDs=%v", missing, extra, changed)
}
