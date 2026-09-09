package cli

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	bundledskills "github.com/volcengine-tls/ve-tls-cli"
	"github.com/volcengine-tls/ve-tls-cli/internal/version"
)

const (
	skillManifestFileName   = ".volclog-skill.json"
	skillManifestSchema     = 1
	skillStatusNotInstalled = "not_installed"
	skillStatusCurrent      = "current"
	skillStatusOutdated     = "outdated"
	skillStatusModified     = "modified"
	skillStatusUntracked    = "untracked"
	skillStatusInvalid      = "invalid_manifest"
)

// skillRename is kept injectable so replacement rollback can be tested without
// depending on filesystem races or permissions. Production uses os.Rename.
var skillRename = os.Rename

type skillManifest struct {
	SchemaVersion    int    `json:"schema_version"`
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version"`
	SourceDigest     string `json:"source_digest"`
	InstalledDigest  string `json:"installed_digest"`
}

type skillManageOptions struct {
	dir   string
	names []string
	force bool
}

type skillDigestEntry struct {
	path    string
	content []byte
}

type skillState struct {
	Name             string
	Status           string
	Reason           string
	InstalledVersion string
	BundledVersion   string
	VersionMatch     bool
	InstalledDigest  string
	BundledDigest    string
	CurrentDigest    string
}

type skillStatusEntry struct {
	Name             string `json:"Name"`
	Status           string `json:"Status"`
	Reason           string `json:"Reason,omitempty"`
	InstalledVersion string `json:"InstalledVersion,omitempty"`
	BundledVersion   string `json:"BundledVersion"`
	VersionMatch     bool   `json:"VersionMatch"`
	InstalledDigest  string `json:"InstalledDigest,omitempty"`
	BundledDigest    string `json:"BundledDigest"`
	CurrentDigest    string `json:"CurrentDigest,omitempty"`
}

type skillActionSkip struct {
	Name   string `json:"Name"`
	Status string `json:"Status"`
	Reason string `json:"Reason"`
}

func parseSkillManageOptions(args []string, allowForce bool) (skillManageOptions, error) {
	var opts skillManageOptions
	for len(args) > 0 {
		switch args[0] {
		case "--dir":
			if len(args) < 2 {
				return skillManageOptions{}, errors.New("missing --dir value")
			}
			opts.dir = args[1]
			args = args[2:]
		case "--name":
			if len(args) < 2 {
				return skillManageOptions{}, errors.New("missing --name value")
			}
			opts.names = append(opts.names, strings.TrimSpace(args[1]))
			args = args[2:]
		case "--force":
			if !allowForce {
				return skillManageOptions{}, errors.New("unknown flag: --force")
			}
			opts.force = true
			args = args[1:]
		default:
			return skillManageOptions{}, errors.New("unknown flag: " + args[0])
		}
	}
	if strings.TrimSpace(opts.dir) == "" {
		return skillManageOptions{}, &usageError{Text: usageSkill(), ExitCode: 1}
	}
	return opts, nil
}

func absoluteSkillDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", &usageError{Text: usageSkill(), ExitCode: 1}
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absDir = filepath.Clean(absDir)
	info, err := os.Lstat(absDir)
	if err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing symlink skill directory: %s", absDir)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("skill directory is not a directory: %s", absDir)
		}
		return absDir, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	return absDir, nil
}

func ensureSkillDir(dir string) error {
	info, err := os.Lstat(dir)
	if err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink skill directory: %s", dir)
		}
		if !info.IsDir() {
			return fmt.Errorf("skill directory is not a directory: %s", dir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	info, err = os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink skill directory: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("skill directory is not a directory: %s", dir)
	}
	return nil
}

func validateSkillName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("invalid skill name: %q", name)
	}
	return nil
}

func selectBundledSkillNames(names []string) ([]string, error) {
	available, err := bundledskills.List()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return available, nil
	}
	index := make(map[string]struct{}, len(available))
	for _, name := range available {
		if err := validateSkillName(name); err != nil {
			return nil, err
		}
		index[name] = struct{}{}
	}
	selected := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if err := validateSkillName(name); err != nil {
			return nil, err
		}
		if _, ok := index[name]; !ok {
			return nil, errors.New("unknown bundled skill: " + name)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, name)
	}
	sort.Strings(selected)
	return selected, nil
}

func installOneBundledSkill(root fs.FS, destDir, name string, force bool) error {
	if err := validateSkillName(name); err != nil {
		return err
	}
	absDir, err := absoluteSkillDir(destDir)
	if err != nil {
		return err
	}
	if err := ensureSkillDir(absDir); err != nil {
		return err
	}
	return stageAndSwapBundledSkill(root, absDir, name, force)
}

func skillStatus(args []string) (any, error) {
	opts, err := parseSkillManageOptions(args, false)
	if err != nil {
		return nil, err
	}
	root, err := bundledskills.Root()
	if err != nil {
		return nil, err
	}
	selected, err := selectBundledSkillNames(opts.names)
	if err != nil {
		return nil, err
	}
	absDir, err := absoluteSkillDir(opts.dir)
	if err != nil {
		return nil, err
	}
	entries := make([]skillStatusEntry, 0, len(selected))
	for _, name := range selected {
		bundledDigest, err := bundledSkillDigest(root, name)
		if err != nil {
			return nil, err
		}
		state, err := inspectSkillState(absDir, name, bundledDigest)
		if err != nil {
			return nil, err
		}
		entries = append(entries, state.statusEntry())
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return map[string]any{
		"Dir":    absDir,
		"Skills": entries,
		"Total":  len(entries),
	}, nil
}

func skillUpdate(args []string) (any, error) {
	opts, err := parseSkillManageOptions(args, true)
	if err != nil {
		return nil, err
	}
	root, err := bundledskills.Root()
	if err != nil {
		return nil, err
	}
	selected, err := selectBundledSkillNames(opts.names)
	if err != nil {
		return nil, err
	}
	absDir, err := absoluteSkillDir(opts.dir)
	if err != nil {
		return nil, err
	}
	updated := make([]string, 0, len(selected))
	skipped := make([]skillActionSkip, 0)
	for _, name := range selected {
		bundledDigest, err := bundledSkillDigest(root, name)
		if err != nil {
			return nil, err
		}
		state, err := inspectSkillState(absDir, name, bundledDigest)
		if err != nil {
			return nil, err
		}
		switch state.Status {
		case skillStatusNotInstalled:
			skipped = append(skipped, skillActionSkip{Name: name, Status: state.Status, Reason: "not_installed"})
		case skillStatusCurrent, skillStatusOutdated:
			if err := stageAndSwapBundledSkill(root, absDir, name, true); err != nil {
				return nil, err
			}
			updated = append(updated, name)
		case skillStatusModified, skillStatusUntracked, skillStatusInvalid:
			if !opts.force {
				skipped = append(skipped, skillActionSkip{Name: name, Status: state.Status, Reason: "protected_without_force"})
				continue
			}
			if err := stageAndSwapBundledSkill(root, absDir, name, true); err != nil {
				return nil, err
			}
			updated = append(updated, name)
		default:
			return nil, fmt.Errorf("unknown skill status %q for %s", state.Status, name)
		}
	}
	sort.Strings(updated)
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Name < skipped[j].Name })
	return map[string]any{
		"Dir":     absDir,
		"Updated": updated,
		"Skipped": skipped,
		"Total":   len(selected),
	}, nil
}

func skillUninstall(args []string) (any, error) {
	opts, err := parseSkillManageOptions(args, true)
	if err != nil {
		return nil, err
	}
	root, err := bundledskills.Root()
	if err != nil {
		return nil, err
	}
	selected, err := selectBundledSkillNames(opts.names)
	if err != nil {
		return nil, err
	}
	absDir, err := absoluteSkillDir(opts.dir)
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(selected))
	skipped := make([]skillActionSkip, 0)
	for _, name := range selected {
		bundledDigest, err := bundledSkillDigest(root, name)
		if err != nil {
			return nil, err
		}
		state, err := inspectSkillState(absDir, name, bundledDigest)
		if err != nil {
			return nil, err
		}
		if state.Status == skillStatusNotInstalled {
			skipped = append(skipped, skillActionSkip{Name: name, Status: state.Status, Reason: "not_installed"})
			continue
		}
		protected := state.Status == skillStatusModified || state.Status == skillStatusUntracked || state.Status == skillStatusInvalid
		if protected && !opts.force {
			skipped = append(skipped, skillActionSkip{Name: name, Status: state.Status, Reason: "protected_without_force"})
			continue
		}
		target := filepath.Join(absDir, name)
		if err := removeSkillDirectory(target); err != nil {
			return nil, err
		}
		removed = append(removed, name)
	}
	sort.Strings(removed)
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Name < skipped[j].Name })
	return map[string]any{
		"Dir":     absDir,
		"Removed": removed,
		"Skipped": skipped,
		"Total":   len(selected),
	}, nil
}

func (s skillState) statusEntry() skillStatusEntry {
	return skillStatusEntry(s)
}

func inspectSkillState(destDir, name, bundledDigest string) (skillState, error) {
	state := skillState{
		Name:           name,
		BundledVersion: version.Version,
		BundledDigest:  bundledDigest,
	}
	target := filepath.Join(destDir, name)
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		state.Status = skillStatusNotInstalled
		return state, nil
	}
	if err != nil {
		return skillState{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return skillState{}, fmt.Errorf("refusing symlink installed skill: %s", target)
	}
	if !info.IsDir() {
		state.Status = skillStatusInvalid
		state.Reason = "installed skill path is not a directory"
		return state, nil
	}
	currentDigest, err := diskSkillDigest(target)
	if err != nil {
		return skillState{}, err
	}
	state.CurrentDigest = currentDigest
	manifest, present, invalidReason, err := readSkillManifest(target, name)
	if err != nil {
		return skillState{}, err
	}
	if !present {
		state.Status = skillStatusUntracked
		state.Reason = "manifest_missing"
		return state, nil
	}
	if invalidReason != "" {
		state.Status = skillStatusInvalid
		state.Reason = invalidReason
		return state, nil
	}
	state.InstalledVersion = manifest.InstalledVersion
	state.VersionMatch = manifest.InstalledVersion == version.Version
	state.InstalledDigest = manifest.InstalledDigest
	if currentDigest != manifest.InstalledDigest {
		state.Status = skillStatusModified
		state.Reason = "content_digest_mismatch"
		return state, nil
	}
	if manifest.SourceDigest != bundledDigest {
		state.Status = skillStatusOutdated
		state.Reason = "bundled_source_changed"
		return state, nil
	}
	if !state.VersionMatch {
		state.Status = skillStatusOutdated
		state.Reason = "bundled_version_changed"
		return state, nil
	}
	state.Status = skillStatusCurrent
	return state, nil
}

func readSkillManifest(skillDir, expectedName string) (skillManifest, bool, string, error) {
	manifestPath := filepath.Join(skillDir, skillManifestFileName)
	info, err := os.Lstat(manifestPath)
	if os.IsNotExist(err) {
		return skillManifest{}, false, "", nil
	}
	if err != nil {
		return skillManifest{}, false, "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return skillManifest{}, true, "manifest is a symlink", nil
	}
	if !info.Mode().IsRegular() {
		return skillManifest{}, true, "manifest is not a regular file", nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return skillManifest{}, true, "manifest cannot be read", nil
	}
	var manifest skillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return skillManifest{}, true, "manifest is invalid JSON", nil
	}
	if manifest.SchemaVersion != skillManifestSchema {
		return manifest, true, "unsupported manifest schema_version", nil
	}
	if manifest.Name != expectedName {
		return manifest, true, "manifest name does not match directory", nil
	}
	if strings.TrimSpace(manifest.InstalledVersion) == "" {
		return manifest, true, "manifest installed_version is empty", nil
	}
	if !validSkillDigest(manifest.SourceDigest) {
		return manifest, true, "manifest source_digest is invalid", nil
	}
	if !validSkillDigest(manifest.InstalledDigest) {
		return manifest, true, "manifest installed_digest is invalid", nil
	}
	return manifest, true, "", nil
}

func validSkillDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func stageAndSwapBundledSkill(root fs.FS, destDir, name string, force bool) error {
	if err := validateSkillName(name); err != nil {
		return err
	}
	if err := ensureSkillDir(destDir); err != nil {
		return err
	}
	target := filepath.Join(destDir, name)
	if err := ensureSkillPathWithinDir(destDir, target); err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink installed skill: %s", target)
		}
		if !force {
			return fmt.Errorf("skill already exists: %s (use --force to overwrite)", target)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	sourceDigest, err := bundledSkillDigest(root, name)
	if err != nil {
		return err
	}
	stageRoot, err := os.MkdirTemp(destDir, ".volclog-skill-stage-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stageRoot) }()
	stageSkill := filepath.Join(stageRoot, name)
	if err := copyBundledSkill(root, name, stageSkill); err != nil {
		return err
	}
	if err := writeSkillManifest(stageSkill, name, sourceDigest); err != nil {
		return err
	}
	stagedDigest, err := diskSkillDigest(stageSkill)
	if err != nil {
		return err
	}
	if stagedDigest != sourceDigest {
		return fmt.Errorf("staged skill digest mismatch for %s", name)
	}
	return swapSkillDirectory(destDir, name, stageSkill, force)
}

func writeSkillManifest(skillDir, name, sourceDigest string) error {
	manifest := skillManifest{
		SchemaVersion:    skillManifestSchema,
		Name:             name,
		InstalledVersion: version.Version,
		SourceDigest:     sourceDigest,
		InstalledDigest:  sourceDigest,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(skillDir, skillManifestFileName), data, 0o644)
}

func swapSkillDirectory(destDir, name, stagedSkill string, force bool) error {
	target := filepath.Join(destDir, name)
	if err := ensureSkillPathWithinDir(destDir, target); err != nil {
		return err
	}
	if err := ensureSkillPathWithinDir(destDir, stagedSkill); err != nil {
		return err
	}
	stagedInfo, err := os.Lstat(stagedSkill)
	if err != nil {
		return err
	}
	if stagedInfo.Mode()&fs.ModeSymlink != 0 || !stagedInfo.IsDir() {
		return fmt.Errorf("staged skill is not a regular directory: %s", stagedSkill)
	}
	info, err := os.Lstat(target)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if exists {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink installed skill: %s", target)
		}
		if !force {
			return fmt.Errorf("skill already exists: %s (use --force to overwrite)", target)
		}
	}

	var backup string
	if exists {
		backup, err = createSkillBackupPath(destDir)
		if err != nil {
			return err
		}
		if err := skillRename(target, backup); err != nil {
			_ = os.RemoveAll(backup)
			return err
		}
	}
	if err := skillRename(stagedSkill, target); err != nil {
		if exists {
			if rollbackErr := skillRename(backup, target); rollbackErr != nil {
				return fmt.Errorf("replace skill %s: %w (rollback failed: %v)", name, err, rollbackErr)
			}
		}
		return err
	}
	if exists {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove skill backup %s: %w", name, err)
		}
	}
	return nil
}

func createSkillBackupPath(destDir string) (string, error) {
	backup, err := os.MkdirTemp(destDir, ".volclog-skill-backup-*")
	if err != nil {
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		_ = os.RemoveAll(backup)
		return "", err
	}
	return backup, nil
}

func removeSkillDirectory(target string) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink installed skill: %s", target)
	}
	return os.RemoveAll(target)
}

func ensureSkillPathWithinDir(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("skill path escapes destination directory: %s", target)
	}
	return nil
}

func bundledSkillDigest(root fs.FS, name string) (string, error) {
	if err := validateSkillName(name); err != nil {
		return "", err
	}
	sub, err := fs.Sub(root, name)
	if err != nil {
		return "", err
	}
	entries, err := collectFSDigestEntries(sub)
	if err != nil {
		return "", err
	}
	return hashSkillDigestEntries(entries)
}

func diskSkillDigest(skillDir string) (string, error) {
	entries, err := collectDiskDigestEntries(skillDir)
	if err != nil {
		return "", err
	}
	return hashSkillDigestEntries(entries)
}

func collectFSDigestEntries(fsys fs.FS) ([]skillDigestEntry, error) {
	entries := make([]skillDigestEntry, 0)
	err := fs.WalkDir(fsys, ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "." {
			if !entry.IsDir() {
				return fmt.Errorf("bundled skill root is not a directory")
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("bundled skill contains symlink: %s", rel)
		}
		if rel == skillManifestFileName {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("bundled skill contains special directory: %s", rel)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeType != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("bundled skill contains special file: %s", rel)
		}
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return err
		}
		entries = append(entries, skillDigestEntry{path: pathpkg.Clean(rel), content: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func collectDiskDigestEntries(skillDir string) ([]skillDigestEntry, error) {
	info, err := os.Lstat(skillDir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlink installed skill: %s", skillDir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("installed skill is not a directory: %s", skillDir)
	}
	entries := make([]skillDigestEntry, 0)
	err = filepath.WalkDir(skillDir, func(fullPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(skillDir, fullPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("installed skill contains symlink: %s", rel)
		}
		if rel == skillManifestFileName {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("installed skill contains special directory: %s", rel)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeType != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("installed skill contains special file: %s", rel)
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		entries = append(entries, skillDigestEntry{path: rel, content: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func hashSkillDigestEntries(entries []skillDigestEntry) (string, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	hasher := sha256.New()
	var length [8]byte
	for _, entry := range entries {
		pathBytes := []byte(entry.path)
		binary.BigEndian.PutUint64(length[:], uint64(len(pathBytes)))
		if _, err := hasher.Write(length[:]); err != nil {
			return "", err
		}
		if _, err := hasher.Write(pathBytes); err != nil {
			return "", err
		}
		binary.BigEndian.PutUint64(length[:], uint64(len(entry.content)))
		if _, err := hasher.Write(length[:]); err != nil {
			return "", err
		}
		if _, err := hasher.Write(entry.content); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func copyBundledSkill(root fs.FS, name, dest string) error {
	sub, err := fs.Sub(root, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "." {
			if !entry.IsDir() {
				return fmt.Errorf("bundled skill root is not a directory")
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("bundled skill contains symlink: %s", rel)
		}
		if rel == skillManifestFileName {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !safeSkillRelativePath(rel) {
			return fmt.Errorf("bundled skill path escapes root: %s", rel)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := ensureSkillPathWithinDir(dest, target); err != nil {
			return err
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("bundled skill contains special directory: %s", rel)
			}
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeType != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("bundled skill contains special file: %s", rel)
		}
		data, err := fs.ReadFile(sub, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func safeSkillRelativePath(rel string) bool {
	clean := pathpkg.Clean(rel)
	return clean != "." && !pathpkg.IsAbs(rel) && clean != ".." && !strings.HasPrefix(clean, "../")
}
