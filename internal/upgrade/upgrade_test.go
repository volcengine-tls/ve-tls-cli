package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCheckLatestReleaseDoesNotModifyExecutable(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "volclog")
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager(executable)
	manager.APIBaseURL = "https://example.test"
	manager.HTTPClient = testHTTPClient(t, func(request *http.Request) (int, []byte) {
		if request.URL.Path != "/releases/latest" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		return http.StatusOK, []byte(`{"tag_name":"volclog-v1.0.7"}`)
	})
	result, err := manager.Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.UpdateAvailable || result.Updated || result.TargetVersion != "1.0.7" {
		t.Fatalf("result=%+v", result)
	}
	assertFileContent(t, executable, "old")
}

func TestRunOnlyReportsHigherSemanticVersionAsUpdate(t *testing.T) {
	testCases := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{name: "older", current: "1.1.0", target: "1.0.6", want: false},
		{name: "same", current: "1.1.0", target: "1.1.0", want: false},
		{name: "newer", current: "1.0.6", target: "1.1.0", want: true},
		{name: "release after prerelease", current: "1.1.0-rc.2", target: "1.1.0", want: true},
		{name: "prerelease before release", current: "1.1.0", target: "1.1.1-rc.1", want: true},
		{name: "older prerelease", current: "1.1.0-rc.2", target: "1.1.0-rc.1", want: false},
		{name: "newer prerelease", current: "1.1.0-rc.1", target: "1.1.0-rc.2", want: true},
		{name: "build metadata ignored", current: "1.1.0+build.1", target: "1.1.0+build.2", want: false},
		{name: "large numeric component", current: "1.999999999999999999999999999.0", target: "1.1000000000000000000000000000.0", want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := testManager(filepath.Join(t.TempDir(), "volclog"))
			manager.CurrentVersion = testCase.current
			result, err := manager.Run(context.Background(), testCase.target, false)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.UpdateAvailable != testCase.want {
				t.Fatalf("UpdateAvailable=%v, want %v; result=%+v", result.UpdateAvailable, testCase.want, result)
			}
		})
	}
}

func TestRunDoesNotApplyOlderTarget(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "volclog")
	if err := os.WriteFile(executable, []byte("current-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager(executable)
	manager.CurrentVersion = "1.1.0"
	manager.HTTPClient = testHTTPClient(t, func(request *http.Request) (int, []byte) {
		t.Fatalf("older target must not trigger a download: %s", request.URL)
		return 0, nil
	})
	result, err := manager.Run(context.Background(), "1.0.6", true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.UpdateAvailable || result.Updated {
		t.Fatalf("result=%+v, want no update", result)
	}
	assertFileContent(t, executable, "current-binary")
}

func TestStandaloneUpgradeVerifiesAndAtomicallyReplacesExecutable(t *testing.T) {
	archive := tarGzipArchive(t, "volclog", []byte("new-binary"))
	digest := sha256.Sum256(archive)
	httpClient := testHTTPClient(t, func(request *http.Request) (int, []byte) {
		switch request.URL.Path {
		case "/download/volclog-v1.0.7/volclog_linux_amd64.tar.gz":
			return http.StatusOK, archive
		case "/download/volclog-v1.0.7/volclog_linux_amd64.tar.gz.sha256":
			return http.StatusOK, []byte(fmt.Sprintf("%x  volclog_linux_amd64.tar.gz\n", digest))
		default:
			return http.StatusNotFound, nil
		}
	})

	executable := filepath.Join(t.TempDir(), "volclog")
	if err := os.WriteFile(executable, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager(executable)
	manager.ReleaseBaseURL = "https://example.test"
	manager.HTTPClient = httpClient
	result, err := manager.Run(context.Background(), "volclog-v1.0.7", true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Updated || !result.UpdateAvailable {
		t.Fatalf("result=%+v", result)
	}
	assertFileContent(t, executable, "new-binary")
	info, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o500 != 0o500 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestStandaloneUpgradeChecksumFailurePreservesExecutable(t *testing.T) {
	archive := tarGzipArchive(t, "volclog", []byte("new-binary"))
	httpClient := testHTTPClient(t, func(request *http.Request) (int, []byte) {
		if strings.HasSuffix(request.URL.Path, ".sha256") {
			return http.StatusOK, []byte(strings.Repeat("0", 64))
		}
		return http.StatusOK, archive
	})

	executable := filepath.Join(t.TempDir(), "volclog")
	if err := os.WriteFile(executable, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager(executable)
	manager.ReleaseBaseURL = "https://example.test"
	manager.HTTPClient = httpClient
	if _, err := manager.Run(context.Background(), "1.0.7", true); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err=%v", err)
	}
	assertFileContent(t, executable, "old-binary")
}

func TestNPMUpgradeDelegatesToGlobalNPM(t *testing.T) {
	var calls []string
	manager := testManager("/ignored/volclog")
	manager.Installation = Installation{
		Method:      InstallNPM,
		PackageName: "@volcengine-tls/volclog",
		PackageRoot: "/opt/npm/lib/node_modules/@volcengine-tls/volclog",
	}
	manager.Command = func(_ context.Context, name string, args []string, dir string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " ")+"|"+dir)
		switch strings.Join(args, " ") {
		case "view @volcengine-tls/volclog dist-tags.latest --json":
			return []byte(`"1.0.7"`), nil
		case "root --global":
			return []byte("/opt/npm/lib/node_modules\n"), nil
		case "install --global @volcengine-tls/volclog@1.0.7":
			return nil, nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", args)
		}
	}
	result, err := manager.Run(context.Background(), "", true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Updated || result.InstallMethod != InstallNPM {
		t.Fatalf("result=%+v", result)
	}
	want := []string{
		"npm view @volcengine-tls/volclog dist-tags.latest --json|",
		"npm root --global|",
		"npm install --global @volcengine-tls/volclog@1.0.7|",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%q, want=%q", calls, want)
	}
}

func TestNPMExplicitCheckDoesNotInstall(t *testing.T) {
	manager := testManager("/ignored/volclog")
	manager.Installation = Installation{
		Method:      InstallNPM,
		PackageName: "@volcengine-tls/volclog",
		PackageRoot: "/project/node_modules/@volcengine-tls/volclog",
	}
	manager.Command = func(_ context.Context, _ string, _ []string, _ string) ([]byte, error) {
		t.Fatal("explicit check must not invoke npm")
		return nil, nil
	}
	result, err := manager.Run(context.Background(), "1.0.7", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.UpdateAvailable || result.Updated {
		t.Fatalf("result=%+v", result)
	}
}

func TestNPMUpgradeDelegatesToLocalProject(t *testing.T) {
	projectRoot := t.TempDir()
	packageRoot := filepath.Join(projectRoot, "node_modules", "@volcengine-tls", "volclog")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager("/ignored/volclog")
	manager.Installation = Installation{
		Method:      InstallNPM,
		PackageName: "@volcengine-tls/volclog",
		PackageRoot: packageRoot,
	}
	var installDir string
	manager.Command = func(_ context.Context, _ string, args []string, dir string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "root --global":
			return []byte(filepath.Join(t.TempDir(), "node_modules")), nil
		case "install --save-exact @volcengine-tls/volclog@1.0.7":
			installDir = dir
			return nil, nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", args)
		}
	}
	result, err := manager.Run(context.Background(), "1.0.7", true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantDir := canonicalPath(projectRoot)
	if !result.Updated || installDir != wantDir {
		t.Fatalf("result=%+v installDir=%q, want %q", result, installDir, wantDir)
	}
}

func TestExtractWindowsZipBinary(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("volclog-human.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("windows-binary"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractBinary(buffer.Bytes(), "volclog-human_windows_arm64.zip", "volclog-human.exe")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if string(got) != "windows-binary" {
		t.Fatalf("got=%q", got)
	}
}

func TestValidVersionRejectsUnsafeReleasePathValues(t *testing.T) {
	for _, value := range []string{"", "1.1", "01.1.0", "1.01.0", "1.1.00", "1.1.0-rc.01", "1.1.0-", "1.1.0+", "1.1.0/asset", "1.1.0@latest", "1.1.0?asset"} {
		if validVersion(value) {
			t.Fatalf("validVersion(%q)=true", value)
		}
	}
}

func testManager(executable string) *Manager {
	manager := NewManager("volclog-v1.0.6", "volclog", Installation{
		Method:     InstallStandalone,
		Executable: executable,
	})
	manager.GOOS = "linux"
	manager.GOARCH = "amd64"
	return manager
}

func tarGzipArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("content=%q, want=%q", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func testHTTPClient(t *testing.T, response func(*http.Request) (int, []byte)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status, body := response(request)
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
}
