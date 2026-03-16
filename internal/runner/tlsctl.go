package runner

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func EnsureTLSCTL(ctx context.Context) (string, error) {
	if p := strings.TrimSpace(os.Getenv("TLSCTL_BIN")); p != "" {
		return p, nil
	}
	if p, ok, err := tryEmbeddedTLSCTL(); err != nil {
		return "", err
	} else if ok {
		return p, nil
	}
	if p, err := exec.LookPath("tlsctl"); err == nil && strings.TrimSpace(p) != "" {
		return p, nil
	}
	return downloadTLSCTL(ctx)
}

func downloadTLSCTL(ctx context.Context) (string, error) {
	baseURL := strings.TrimSpace(os.Getenv("TLSCTL_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download"
	}
	cacheDir := strings.TrimSpace(os.Getenv("TLSCTL_CACHE_DIR"))
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		cacheDir = filepath.Join(home, ".cache", "tlsctl-runner")
	}
	key := cacheKey(baseURL)
	dir := filepath.Join(cacheDir, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	binName := "tlsctl"
	if runtime.GOOS == "windows" {
		binName = "tlsctl.exe"
	}
	binPath := filepath.Join(dir, binName)
	if st, err := os.Stat(binPath); err == nil && !st.IsDir() {
		return binPath, nil
	}

	pkgName, pkgType := packageName(runtime.GOOS, runtime.GOARCH)
	pkgURL := strings.TrimRight(baseURL, "/") + "/" + pkgName
	shaURL := pkgURL + ".sha256"

	pkgPath := filepath.Join(dir, pkgName)
	if err := downloadFile(ctx, pkgURL, pkgPath); err != nil {
		return "", err
	}
	if err := verifySHA256IfPresent(ctx, shaURL, pkgName, pkgPath); err != nil {
		return "", err
	}
	if pkgType == "tar.gz" {
		if err := extractTarGzBinary(pkgPath, binPath, "tlsctl"); err != nil {
			return "", err
		}
	} else if pkgType == "zip" {
		if err := extractZipBinary(pkgPath, binPath, "tlsctl.exe"); err != nil {
			return "", err
		}
	} else {
		return "", errors.New("unsupported package type")
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(binPath, 0o755)
	}
	return binPath, nil
}

func packageName(goos, goarch string) (string, string) {
	if goos == "windows" {
		return "tlsctl_windows_" + goarch + ".zip", "zip"
	}
	return "tlsctl_" + goos + "_" + goarch + ".tar.gz", "tar.gz"
}

func cacheKey(baseURL string) string {
	sum := sha256.Sum256([]byte(baseURL + "|" + runtime.GOOS + "|" + runtime.GOARCH))
	return hex.EncodeToString(sum[:8])
}

func downloadFile(ctx context.Context, url string, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("download failed: " + resp.Status)
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}

func verifySHA256IfPresent(ctx context.Context, shaURL string, filename string, filePath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shaURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("sha256 download failed: " + resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return errors.New("invalid sha256 file")
	}
	want := strings.ToLower(strings.TrimSpace(fields[0]))
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if want != got {
		return errors.New("sha256 mismatch for " + filename)
	}
	return nil
}

func extractTarGzBinary(pkgPath string, outPath string, memberName string) error {
	f, err := os.Open(pkgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Base(h.Name)
		if name != memberName {
			continue
		}
		tmp := outPath + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(tmp)
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		return os.Rename(tmp, outPath)
	}
	return errors.New("binary not found in tar.gz")
}

func extractZipBinary(pkgPath string, outPath string, memberName string) error {
	zr, err := zip.OpenReader(pkgPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != memberName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		tmp := outPath + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		_ = rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(tmp)
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		return os.Rename(tmp, outPath)
	}
	return errors.New("binary not found in zip")
}
