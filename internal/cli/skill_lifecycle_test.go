package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	bundledskills "github.com/volcengine-tls/ve-tls-cli"
	"github.com/volcengine-tls/ve-tls-cli/internal/version"
)

func runSkillJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run(append([]string{"skill"}, args...), &stdout, &stderr); code != 0 {
		t.Fatalf("skill command exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid skill JSON: %v stdout=%s", err, stdout.String())
	}
	return out
}

func skillOutputNames(t *testing.T, out map[string]any, field string) []string {
	t.Helper()
	values, ok := out[field].([]any)
	if !ok {
		t.Fatalf("%s is not an array: %#v", field, out[field])
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("%s contains non-string value: %#v", field, value)
		}
		names = append(names, name)
	}
	return names
}

func skillStatusOutputEntry(t *testing.T, out map[string]any, name string) map[string]any {
	t.Helper()
	values, ok := out["Skills"].([]any)
	if !ok {
		t.Fatalf("Skills is not an array: %#v", out["Skills"])
	}
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("status entry is not an object: %#v", value)
		}
		if entry["Name"] == name {
			return entry
		}
	}
	t.Fatalf("status entry %q not found: %#v", name, out)
	return nil
}

func readSkillManifestForTest(t *testing.T, dest, name string) skillManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dest, name, skillManifestFileName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest skillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func TestSkillInstallWritesManifestAndDigest(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")

	manifest := readSkillManifestForTest(t, dest, "volclog-core")
	if manifest.SchemaVersion != skillManifestSchema {
		t.Fatalf("schema_version=%d, want %d", manifest.SchemaVersion, skillManifestSchema)
	}
	if manifest.Name != "volclog-core" {
		t.Fatalf("manifest name=%q", manifest.Name)
	}
	if manifest.InstalledVersion != version.Version {
		t.Fatalf("installed_version=%q, want %q", manifest.InstalledVersion, version.Version)
	}
	if manifest.SourceDigest == "" || manifest.SourceDigest != manifest.InstalledDigest {
		t.Fatalf("unexpected manifest digests: %#v", manifest)
	}
	root, err := bundledskills.Root()
	if err != nil {
		t.Fatalf("bundled root: %v", err)
	}
	sourceDigest, err := bundledSkillDigest(root, "volclog-core")
	if err != nil {
		t.Fatalf("source digest: %v", err)
	}
	diskDigest, err := diskSkillDigest(filepath.Join(dest, "volclog-core"))
	if err != nil {
		t.Fatalf("disk digest: %v", err)
	}
	if manifest.SourceDigest != sourceDigest || manifest.InstalledDigest != diskDigest {
		t.Fatalf("digest mismatch: manifest=%#v source=%s disk=%s", manifest, sourceDigest, diskDigest)
	}
}

func TestSkillStatusDetectsModifiedContent(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")
	path := filepath.Join(dest, "volclog-core", "SKILL.md")
	if err := os.WriteFile(path, []byte("user modification\n"), 0o644); err != nil {
		t.Fatalf("modify skill: %v", err)
	}

	out := runSkillJSON(t, "status", "--dir", dest, "--name", "volclog-core")
	entry := skillStatusOutputEntry(t, out, "volclog-core")
	if entry["Status"] != skillStatusModified {
		t.Fatalf("status=%v, want %s: %#v", entry["Status"], skillStatusModified, entry)
	}
	if entry["VersionMatch"] != true {
		t.Fatalf("content edit should not change version match: %#v", entry)
	}
}

func TestSkillLegacyUntrackedIsProtectedByDefault(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")
	manifestPath := filepath.Join(dest, "volclog-core", skillManifestFileName)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	contentPath := filepath.Join(dest, "volclog-core", "SKILL.md")
	before, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("read original skill: %v", err)
	}

	status := runSkillJSON(t, "status", "--dir", dest, "--name", "volclog-core")
	if got := skillStatusOutputEntry(t, status, "volclog-core")["Status"]; got != skillStatusUntracked {
		t.Fatalf("status=%v, want %s", got, skillStatusUntracked)
	}
	update := runSkillJSON(t, "update", "--dir", dest, "--name", "volclog-core")
	if got := skillOutputNames(t, update, "Updated"); len(got) != 0 {
		t.Fatalf("untracked skill was updated without force: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "volclog-core")); err != nil {
		t.Fatalf("protected skill disappeared after update: %v", err)
	}
	after, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("read skill after update: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("untracked skill content changed after update")
	}

	uninstall := runSkillJSON(t, "uninstall", "--dir", dest, "--name", "volclog-core")
	if got := skillOutputNames(t, uninstall, "Removed"); len(got) != 0 {
		t.Fatalf("untracked skill was removed without force: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "volclog-core")); err != nil {
		t.Fatalf("protected skill disappeared after uninstall: %v", err)
	}
}

func TestSkillStatusReportsOutdatedWithoutUserModification(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")
	manifestPath := filepath.Join(dest, "volclog-core", skillManifestFileName)
	manifest := readSkillManifestForTest(t, dest, "volclog-core")
	manifest.SourceDigest = strings.Repeat("f", 64)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("encode old manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write old manifest: %v", err)
	}

	out := runSkillJSON(t, "status", "--dir", dest, "--name", "volclog-core")
	entry := skillStatusOutputEntry(t, out, "volclog-core")
	if entry["Status"] != skillStatusOutdated {
		t.Fatalf("status=%v, want %s: %#v", entry["Status"], skillStatusOutdated, entry)
	}
	if entry["VersionMatch"] != true {
		t.Fatalf("manifest version unexpectedly changed: %#v", entry)
	}
}

func TestSkillStatusReportsVersionOnlyChangeAsOutdatedNotModified(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")
	previousVersion := version.Version
	version.Version = "volclog-v9.9.9"
	defer func() { version.Version = previousVersion }()

	out := runSkillJSON(t, "status", "--dir", dest, "--name", "volclog-core")
	entry := skillStatusOutputEntry(t, out, "volclog-core")
	if entry["Status"] != skillStatusOutdated || entry["Reason"] != "bundled_version_changed" {
		t.Fatalf("version-only change must be outdated, not modified: %#v", entry)
	}
	if entry["VersionMatch"] != false {
		t.Fatalf("version-only change should report mismatch: %#v", entry)
	}
}

func TestSkillUpdateForceRepairsModifiedSkill(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")
	contentPath := filepath.Join(dest, "volclog-core", "SKILL.md")
	if err := os.WriteFile(contentPath, []byte("user modification\n"), 0o644); err != nil {
		t.Fatalf("modify skill: %v", err)
	}
	withoutForce := runSkillJSON(t, "update", "--dir", dest, "--name", "volclog-core")
	if got := skillOutputNames(t, withoutForce, "Updated"); len(got) != 0 {
		t.Fatalf("modified skill was updated without force: %v", got)
	}
	withForce := runSkillJSON(t, "update", "--dir", dest, "--name", "volclog-core", "--force")
	if got := skillOutputNames(t, withForce, "Updated"); !reflect.DeepEqual(got, []string{"volclog-core"}) {
		t.Fatalf("force update=%v", got)
	}
	status := runSkillJSON(t, "status", "--dir", dest, "--name", "volclog-core")
	if got := skillStatusOutputEntry(t, status, "volclog-core")["Status"]; got != skillStatusCurrent {
		t.Fatalf("status after force update=%v, want %s", got, skillStatusCurrent)
	}
}

func TestSkillInvalidManifestIsProtectedThenForceRepaired(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")
	manifestPath := filepath.Join(dest, "volclog-core", skillManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("not-json\n"), 0o644); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}
	status := runSkillJSON(t, "status", "--dir", dest, "--name", "volclog-core")
	if got := skillStatusOutputEntry(t, status, "volclog-core")["Status"]; got != skillStatusInvalid {
		t.Fatalf("status=%v, want %s", got, skillStatusInvalid)
	}
	withoutForce := runSkillJSON(t, "update", "--dir", dest, "--name", "volclog-core")
	if got := skillOutputNames(t, withoutForce, "Updated"); len(got) != 0 {
		t.Fatalf("invalid manifest was updated without force: %v", got)
	}
	withForce := runSkillJSON(t, "update", "--dir", dest, "--name", "volclog-core", "--force")
	if got := skillOutputNames(t, withForce, "Updated"); !reflect.DeepEqual(got, []string{"volclog-core"}) {
		t.Fatalf("force update=%v", got)
	}
	if got := skillStatusOutputEntry(t, runSkillJSON(t, "status", "--dir", dest, "--name", "volclog-core"), "volclog-core")["Status"]; got != skillStatusCurrent {
		t.Fatalf("status after force repair=%v, want %s", got, skillStatusCurrent)
	}
}

func TestSkillUninstallForceRemovesModifiedSkill(t *testing.T) {
	dest := t.TempDir()
	runSkillJSON(t, "install", "--dir", dest, "--name", "volclog-core")
	contentPath := filepath.Join(dest, "volclog-core", "SKILL.md")
	if err := os.WriteFile(contentPath, []byte("user modification\n"), 0o644); err != nil {
		t.Fatalf("modify skill: %v", err)
	}
	withoutForce := runSkillJSON(t, "uninstall", "--dir", dest, "--name", "volclog-core")
	if got := skillOutputNames(t, withoutForce, "Removed"); len(got) != 0 {
		t.Fatalf("modified skill was removed without force: %v", got)
	}
	withForce := runSkillJSON(t, "uninstall", "--dir", dest, "--name", "volclog-core", "--force")
	if got := skillOutputNames(t, withForce, "Removed"); !reflect.DeepEqual(got, []string{"volclog-core"}) {
		t.Fatalf("force uninstall=%v", got)
	}
	if _, err := os.Lstat(filepath.Join(dest, "volclog-core")); !os.IsNotExist(err) {
		t.Fatalf("skill still exists after force uninstall, err=%v", err)
	}
	second := runSkillJSON(t, "uninstall", "--dir", dest, "--name", "volclog-core")
	if got := skillOutputNames(t, second, "Removed"); len(got) != 0 {
		t.Fatalf("idempotent uninstall removed unexpected skill: %v", got)
	}
}

func TestSkillAtomicReplacementRollsBackOnRenameFailure(t *testing.T) {
	dest := t.TempDir()
	root, err := bundledskills.Root()
	if err != nil {
		t.Fatalf("bundled root: %v", err)
	}
	if err := installOneBundledSkill(root, dest, "volclog-core", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}
	contentPath := filepath.Join(dest, "volclog-core", "SKILL.md")
	before, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("read original skill: %v", err)
	}

	previousRename := skillRename
	defer func() { skillRename = previousRename }()
	callCount := 0
	skillRename = func(oldPath, newPath string) error {
		callCount++
		if callCount == 2 {
			return errors.New("injected rename failure")
		}
		return previousRename(oldPath, newPath)
	}
	if err := installOneBundledSkill(root, dest, "volclog-core", true); err == nil {
		t.Fatal("expected replacement failure")
	}
	after, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("read skill after rollback: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing skill content changed after failed replacement")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".volclog-skill-stage-") || strings.HasPrefix(entry.Name(), ".volclog-skill-backup-") {
			t.Fatalf("temporary replacement path leaked: %s", entry.Name())
		}
	}
}

func TestSkillStatusRejectsInstalledSymlink(t *testing.T) {
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("outside\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(dest, "volclog-core")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"skill", "status", "--dir", dest, "--name", "volclog-core"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected symlink rejection, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(strings.ToLower(stderr.String()), "symlink") {
		t.Fatalf("missing symlink error: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestSkillLifecycleUsesStableNameOrdering(t *testing.T) {
	dest := t.TempDir()
	out := runSkillJSON(t, "status", "--dir", dest, "--name", "tls-logcollector", "--name", "volclog-core", "--name", "tls-logcollector")
	values, ok := out["Skills"].([]any)
	if !ok {
		t.Fatalf("Skills is not an array: %#v", out["Skills"])
	}
	got := make([]string, 0, len(values))
	for _, value := range values {
		entry := value.(map[string]any)
		got = append(got, entry["Name"].(string))
	}
	want := append([]string(nil), got...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status order=%v, want sorted=%v", got, want)
	}

	update := runSkillJSON(t, "update", "--dir", dest, "--name", "tls-logcollector", "--name", "volclog-core")
	skipped, ok := update["Skipped"].([]any)
	if !ok {
		t.Fatalf("Skipped is not an array: %#v", update["Skipped"])
	}
	got = got[:0]
	for _, value := range skipped {
		entry := value.(map[string]any)
		got = append(got, entry["Name"].(string))
	}
	want = append([]string(nil), got...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("update order=%v, want sorted=%v", got, want)
	}
}
