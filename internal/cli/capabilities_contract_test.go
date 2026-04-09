package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

type capabilitiesContractLock struct {
	SchemaVersion   int    `json:"schema_version"`
	ContractVersion string `json:"contract_version"`
	CapabilitiesSHA string `json:"capabilities_sha256"`
	CommandsCount   int    `json:"commands_count"`
	SignaturesSHA   string `json:"signatures_sha256"`
	SnapshotPath    string `json:"snapshot_path"`
	UpdatedAt       string `json:"updated_at"`
}

func TestCapabilitiesContractSnapshot(t *testing.T) {
	doc, err := loadAPICapabilities()
	if err != nil {
		t.Fatalf("load capabilities failed: %v", err)
	}

	root := repoRootFromThisFile(t)
	lockPath := filepath.Join(root, "docs", "agentic-stage1", "capabilities-contract-lock.json")
	snapshotPath := filepath.Join(root, "docs", "agentic-stage1", "capabilities-contract-snapshot.txt")

	sigs := buildCapabilitySignatures(doc.Commands)
	snapshotContent := strings.Join(sigs, "\n") + "\n"
	currentCapsSHA := contractSHA256Hex([]byte(generatedCapabilitiesJSON))
	currentSigsSHA := contractSHA256Hex([]byte(snapshotContent))

	if os.Getenv("UPDATE_CAPABILITIES_CONTRACT") == "1" {
		lock := capabilitiesContractLock{
			SchemaVersion:   1,
			ContractVersion: strings.TrimSpace(doc.Version),
			CapabilitiesSHA: currentCapsSHA,
			CommandsCount:   len(doc.Commands),
			SignaturesSHA:   currentSigsSHA,
			SnapshotPath:    "docs/agentic-stage1/capabilities-contract-snapshot.txt",
			UpdatedAt:       time.Now().Format("2006-01-02"),
		}
		lockBytes, err := json.MarshalIndent(lock, "", "  ")
		if err != nil {
			t.Fatalf("marshal lock failed: %v", err)
		}
		lockBytes = append(lockBytes, '\n')
		if err := os.WriteFile(lockPath, lockBytes, 0o644); err != nil {
			t.Fatalf("write lock failed: %v", err)
		}
		if err := os.WriteFile(snapshotPath, []byte(snapshotContent), 0o644); err != nil {
			t.Fatalf("write snapshot failed: %v", err)
		}
		t.Skip("capabilities contract lock and snapshot updated")
	}

	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock failed: %v", err)
	}
	var lock capabilitiesContractLock
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		t.Fatalf("parse lock failed: %v", err)
	}

	if strings.TrimSpace(lock.ContractVersion) != strings.TrimSpace(doc.Version) {
		t.Fatalf("contract version mismatch: lock=%q current=%q", lock.ContractVersion, doc.Version)
	}
	if strings.TrimSpace(lock.CapabilitiesSHA) != currentCapsSHA {
		t.Fatalf("capabilities hash mismatch: lock=%s current=%s", lock.CapabilitiesSHA, currentCapsSHA)
	}
	if lock.CommandsCount != len(doc.Commands) {
		t.Fatalf("commands count mismatch: lock=%d current=%d", lock.CommandsCount, len(doc.Commands))
	}
	if strings.TrimSpace(lock.SignaturesSHA) != currentSigsSHA {
		t.Fatalf("signatures hash mismatch: lock=%s current=%s", lock.SignaturesSHA, currentSigsSHA)
	}

	snapshotBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot failed: %v", err)
	}
	want := strings.TrimSpace(snapshotContent)
	got := strings.TrimSpace(string(snapshotBytes))
	if got != want {
		missing, added := diffSignatures(got, want)
		t.Fatalf("capabilities snapshot mismatch: removed=%v added=%v", trimSlice(missing, 5), trimSlice(added, 5))
	}
}

func repoRootFromThisFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func buildCapabilitySignatures(cmds []apiCapabilityCommand) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, fmt.Sprintf(
			"%s\t%s\t%s\t%s",
			normalizeToken(c.Group),
			normalizeToken(c.Action),
			strings.ToUpper(strings.TrimSpace(c.Method)),
			strings.TrimSpace(c.Path),
		))
	}
	sort.Strings(out)
	return out
}

func contractSHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func diffSignatures(gotRaw, wantRaw string) (missing []string, added []string) {
	gotSet := map[string]struct{}{}
	wantSet := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(gotRaw), "\n") {
		v := strings.TrimSpace(line)
		if v != "" {
			gotSet[v] = struct{}{}
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(wantRaw), "\n") {
		v := strings.TrimSpace(line)
		if v != "" {
			wantSet[v] = struct{}{}
		}
	}
	for v := range wantSet {
		if _, ok := gotSet[v]; !ok {
			missing = append(missing, v)
		}
	}
	for v := range gotSet {
		if _, ok := wantSet[v]; !ok {
			added = append(added, v)
		}
	}
	sort.Strings(missing)
	sort.Strings(added)
	return missing, added
}

func trimSlice(in []string, max int) []string {
	if len(in) <= max {
		return in
	}
	return in[:max]
}
