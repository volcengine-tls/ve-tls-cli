package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL     = "https://api.github.com/repos/volcengine-tls/ve-tls-cli"
	defaultReleaseBaseURL = "https://github.com/volcengine-tls/ve-tls-cli/releases"
	maxMetadataBytes      = 1 << 20
	maxArchiveBytes       = 256 << 20
	maxBinaryBytes        = 256 << 20
)

type InstallMethod string

const (
	InstallStandalone InstallMethod = "standalone"
	InstallNPM        InstallMethod = "npm"
)

type Installation struct {
	Method      InstallMethod
	PackageName string
	PackageRoot string
	Executable  string
}

type Result struct {
	SchemaVersion   int           `json:"schema_version"`
	CurrentVersion  string        `json:"current_version"`
	TargetVersion   string        `json:"target_version"`
	Edition         string        `json:"edition"`
	InstallMethod   InstallMethod `json:"install_method"`
	UpdateAvailable bool          `json:"update_available"`
	Updated         bool          `json:"updated"`
}

type Manager struct {
	HTTPClient     *http.Client
	APIBaseURL     string
	ReleaseBaseURL string
	CurrentVersion string
	Edition        string
	GOOS           string
	GOARCH         string
	Installation   Installation
	Command        func(context.Context, string, []string, string) ([]byte, error)
}

func NewManager(currentVersion, edition string, installation Installation) *Manager {
	return &Manager{
		HTTPClient:     &http.Client{Timeout: 30 * time.Second},
		APIBaseURL:     defaultAPIBaseURL,
		ReleaseBaseURL: defaultReleaseBaseURL,
		CurrentVersion: normalizeVersion(currentVersion),
		Edition:        strings.TrimSpace(edition),
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		Installation:   installation,
		Command:        runCommand,
	}
}

func DetectInstallation() (Installation, error) {
	executable, err := os.Executable()
	if err != nil {
		return Installation{}, fmt.Errorf("resolve executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return Installation{}, fmt.Errorf("resolve executable path: %w", err)
	}
	method := InstallStandalone
	packageName := strings.TrimSpace(os.Getenv("VOLCLOG_NPM_PACKAGE"))
	packageRoot := strings.TrimSpace(os.Getenv("VOLCLOG_NPM_PACKAGE_ROOT"))
	if strings.EqualFold(strings.TrimSpace(os.Getenv("VOLCLOG_INSTALL_METHOD")), string(InstallNPM)) {
		method = InstallNPM
		if packageName == "" || packageRoot == "" {
			return Installation{}, errors.New("invalid npm installation metadata; reinstall the npm package")
		}
	}
	return Installation{
		Method:      method,
		PackageName: packageName,
		PackageRoot: packageRoot,
		Executable:  executable,
	}, nil
}

func (m *Manager) Run(ctx context.Context, requestedVersion string, apply bool) (Result, error) {
	if err := m.validate(); err != nil {
		return Result{}, err
	}
	current := normalizeVersion(m.CurrentVersion)
	if !validVersion(current) {
		return Result{}, fmt.Errorf("invalid current version %q", m.CurrentVersion)
	}
	target := normalizeVersion(requestedVersion)
	var err error
	if target == "" {
		if m.Installation.Method == InstallNPM {
			target, err = m.latestNPMVersion(ctx)
		} else {
			target, err = m.latestReleaseVersion(ctx)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if !validVersion(target) {
		return Result{}, fmt.Errorf("invalid target version %q", requestedVersion)
	}
	result := Result{
		SchemaVersion:   1,
		CurrentVersion:  current,
		TargetVersion:   target,
		Edition:         m.Edition,
		InstallMethod:   m.Installation.Method,
		UpdateAvailable: compareVersions(current, target) < 0,
	}
	if !apply || !result.UpdateAvailable {
		return result, nil
	}
	if m.Installation.Method == InstallNPM {
		err = m.upgradeNPM(ctx, target)
	} else {
		err = m.upgradeStandalone(ctx, target)
	}
	if err != nil {
		return Result{}, err
	}
	result.Updated = true
	return result, nil
}

func (m *Manager) validate() error {
	if m.HTTPClient == nil {
		return errors.New("upgrade HTTP client is required")
	}
	if m.Installation.Method != InstallStandalone && m.Installation.Method != InstallNPM {
		return fmt.Errorf("unsupported install method %q", m.Installation.Method)
	}
	if m.Installation.Method == InstallStandalone && strings.TrimSpace(m.Installation.Executable) == "" {
		return errors.New("standalone executable path is required")
	}
	if m.GOOS != "darwin" && m.GOOS != "linux" && m.GOOS != "windows" {
		return fmt.Errorf("unsupported operating system %q", m.GOOS)
	}
	if m.GOARCH != "amd64" && m.GOARCH != "arm64" {
		return fmt.Errorf("unsupported architecture %q", m.GOARCH)
	}
	if m.Edition != "volclog" && m.Edition != "volclog-human" {
		return fmt.Errorf("unsupported edition %q", m.Edition)
	}
	if m.Installation.Method == InstallNPM {
		expectedPackage := "@volcengine-tls/volclog"
		if m.Edition == "volclog-human" {
			expectedPackage = "@volcengine-tls/volclog-human"
		}
		if m.Installation.PackageName != expectedPackage {
			return fmt.Errorf("npm package %q does not match edition %q", m.Installation.PackageName, m.Edition)
		}
		if !filepath.IsAbs(m.Installation.PackageRoot) {
			return errors.New("npm package root must be an absolute path")
		}
		if m.Command == nil {
			return errors.New("npm command runner is required")
		}
	}
	return nil
}

func (m *Manager) latestReleaseVersion(ctx context.Context) (string, error) {
	body, err := m.get(ctx, strings.TrimRight(m.APIBaseURL, "/")+"/releases/latest", maxMetadataBytes)
	if err != nil {
		return "", fmt.Errorf("check latest release: %w", err)
	}
	var response struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("decode latest release: %w", err)
	}
	version := normalizeVersion(response.TagName)
	if !validVersion(version) {
		return "", fmt.Errorf("latest release returned invalid tag %q", response.TagName)
	}
	return version, nil
}

func (m *Manager) latestNPMVersion(ctx context.Context) (string, error) {
	if strings.TrimSpace(m.Installation.PackageName) == "" {
		return "", errors.New("npm package name is missing; reinstall the npm package")
	}
	output, err := m.Command(ctx, "npm", []string{"view", m.Installation.PackageName, "dist-tags.latest", "--json"}, "")
	if err != nil {
		return "", fmt.Errorf("check npm version: %w", err)
	}
	var version string
	if err := json.Unmarshal(output, &version); err != nil {
		return "", fmt.Errorf("decode npm version: %w", err)
	}
	version = normalizeVersion(version)
	if !validVersion(version) {
		return "", fmt.Errorf("npm returned invalid version %q", version)
	}
	return version, nil
}

func (m *Manager) upgradeStandalone(ctx context.Context, target string) error {
	asset, binaryName, err := m.releaseAssetNames()
	if err != nil {
		return err
	}
	base := strings.TrimRight(m.ReleaseBaseURL, "/") + "/download/volclog-v" + target + "/" + asset
	archive, err := m.get(ctx, base, maxArchiveBytes)
	if err != nil {
		return fmt.Errorf("download release archive: %w", err)
	}
	checksum, err := m.get(ctx, base+".sha256", maxMetadataBytes)
	if err != nil {
		return fmt.Errorf("download release checksum: %w", err)
	}
	if err := verifyChecksum(archive, checksum); err != nil {
		return err
	}
	binary, err := extractBinary(archive, asset, binaryName)
	if err != nil {
		return err
	}
	return atomicReplace(m.Installation.Executable, binary)
}

func (m *Manager) releaseAssetNames() (string, string, error) {
	prefix := m.Edition
	binaryName := prefix
	if m.GOOS == "windows" {
		binaryName += ".exe"
		return fmt.Sprintf("%s_%s_%s.zip", prefix, m.GOOS, m.GOARCH), binaryName, nil
	}
	return fmt.Sprintf("%s_%s_%s.tar.gz", prefix, m.GOOS, m.GOARCH), binaryName, nil
}

func (m *Manager) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "volclog/"+m.CurrentVersion)
	response, err := m.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, fmt.Errorf("GET %s returned HTTP %d", url, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", url, limit)
	}
	return body, nil
}

func verifyChecksum(content, checksumFile []byte) error {
	fields := strings.Fields(string(checksumFile))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return errors.New("invalid release checksum")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil {
		return errors.New("invalid release checksum")
	}
	actual := sha256.Sum256(content)
	if !bytes.Equal(expected, actual[:]) {
		return errors.New("release checksum mismatch")
	}
	return nil
}

func extractBinary(archive []byte, assetName, binaryName string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("open release archive: %w", err)
		}
		for _, file := range reader.File {
			if filepath.Base(filepath.ToSlash(file.Name)) != binaryName || file.FileInfo().IsDir() {
				continue
			}
			if file.UncompressedSize64 > maxBinaryBytes {
				return nil, errors.New("release binary is too large")
			}
			stream, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("open release binary: %w", err)
			}
			body, readErr := io.ReadAll(io.LimitReader(stream, maxBinaryBytes+1))
			closeErr := stream.Close()
			if readErr != nil {
				return nil, fmt.Errorf("read release binary: %w", readErr)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("close release binary: %w", closeErr)
			}
			if len(body) > maxBinaryBytes {
				return nil, errors.New("release binary is too large")
			}
			return body, nil
		}
		return nil, fmt.Errorf("release archive is missing %s", binaryName)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(filepath.ToSlash(header.Name)) != binaryName {
			continue
		}
		if header.Size < 0 || header.Size > maxBinaryBytes {
			return nil, errors.New("release binary is too large")
		}
		body, err := io.ReadAll(io.LimitReader(tarReader, maxBinaryBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read release binary: %w", err)
		}
		if len(body) > maxBinaryBytes {
			return nil, errors.New("release binary is too large")
		}
		return body, nil
	}
	return nil, fmt.Errorf("release archive is missing %s", binaryName)
}

func atomicReplace(target string, content []byte) (returnErr error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("executable path is empty")
	}
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("inspect executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("refusing to replace a non-regular executable")
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".volclog-upgrade-*")
	if err != nil {
		return fmt.Errorf("create upgrade file: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(info.Mode().Perm() | 0o500); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set upgrade file permissions: %w", err)
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write upgrade file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync upgrade file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close upgrade file: %w", err)
	}
	if err := replaceFile(tempName, target); err != nil {
		return fmt.Errorf("replace executable atomically: %w", err)
	}
	return nil
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "volclog-v")
	value = strings.TrimPrefix(value, "v")
	return value
}

func validVersion(value string) bool {
	_, ok := parseVersion(value)
	return ok
}

type semanticVersion struct {
	core       [3]string
	prerelease []string
}

func parseVersion(value string) (semanticVersion, bool) {
	value = normalizeVersion(value)
	if value == "" || strings.Count(value, "+") > 1 {
		return semanticVersion{}, false
	}
	precedence, build, hasBuild := strings.Cut(value, "+")
	if hasBuild && !validIdentifiers(build, false) {
		return semanticVersion{}, false
	}
	coreValue, prerelease, hasPrerelease := strings.Cut(precedence, "-")
	if hasPrerelease && !validIdentifiers(prerelease, true) {
		return semanticVersion{}, false
	}
	core := strings.Split(coreValue, ".")
	if len(core) != 3 {
		return semanticVersion{}, false
	}
	var parsed semanticVersion
	for index, part := range core {
		if !numericIdentifier(part) || (len(part) > 1 && part[0] == '0') {
			return semanticVersion{}, false
		}
		parsed.core[index] = part
	}
	if hasPrerelease {
		parsed.prerelease = strings.Split(prerelease, ".")
	}
	return parsed, true
}

func validIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		for _, char := range identifier {
			if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '-' {
				continue
			}
			return false
		}
		if rejectNumericLeadingZero && len(identifier) > 1 && identifier[0] == '0' && numericIdentifier(identifier) {
			return false
		}
	}
	return true
}

func numericIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// compareVersions returns -1, 0, or 1 when left has lower, equal, or higher
// SemVer precedence than right. Callers validate both inputs first.
func compareVersions(left, right string) int {
	leftVersion, _ := parseVersion(left)
	rightVersion, _ := parseVersion(right)
	for index := range leftVersion.core {
		if compared := compareNumericIdentifier(leftVersion.core[index], rightVersion.core[index]); compared != 0 {
			return compared
		}
	}
	if len(leftVersion.prerelease) == 0 && len(rightVersion.prerelease) == 0 {
		return 0
	}
	if len(leftVersion.prerelease) == 0 {
		return 1
	}
	if len(rightVersion.prerelease) == 0 {
		return -1
	}
	limit := min(len(leftVersion.prerelease), len(rightVersion.prerelease))
	for index := 0; index < limit; index++ {
		leftIdentifier := leftVersion.prerelease[index]
		rightIdentifier := rightVersion.prerelease[index]
		leftNumeric := numericIdentifier(leftIdentifier)
		rightNumeric := numericIdentifier(rightIdentifier)
		switch {
		case leftNumeric && rightNumeric:
			if compared := compareNumericIdentifier(leftIdentifier, rightIdentifier); compared != 0 {
				return compared
			}
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case leftIdentifier < rightIdentifier:
			return -1
		case leftIdentifier > rightIdentifier:
			return 1
		}
	}
	switch {
	case len(leftVersion.prerelease) < len(rightVersion.prerelease):
		return -1
	case len(leftVersion.prerelease) > len(rightVersion.prerelease):
		return 1
	default:
		return 0
	}
}

func compareNumericIdentifier(left, right string) int {
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func runCommand(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, err
	}
	return output, nil
}
