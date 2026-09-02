package cluster

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Downloader installs verified helper binaries into the user cache.
type Downloader struct {
	Client    *http.Client
	BaseURL   string
	Checksums map[string]string
}

// EnsureKind returns a verified cached kind binary, downloading it when needed.
func (d Downloader) EnsureKind(ctx context.Context, cacheDir, goos, goarch string) (string, error) {
	platform := goos + "/" + goarch
	checksums := d.Checksums
	if checksums == nil {
		checksums = kindChecksums
	}
	expected, ok := checksums[platform]
	if !ok {
		return "", fmt.Errorf("unsupported platform %s; expected macOS or Linux on amd64/arm64", platform)
	}
	path := filepath.Join(cacheDir, "kind", KindVersion, "kind")
	if validFileSHA256(path, expected) {
		return path, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove invalid cached kind: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create kind cache: %w", err)
	}

	baseURL := d.BaseURL
	if baseURL == "" {
		baseURL = "https://github.com/kubernetes-sigs/kind/releases/download/" + KindVersion
	}
	url := fmt.Sprintf("%s/kind-%s-%s", baseURL, goos, goarch)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create kind download request: %w", err)
	}
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download kind %s: %w", KindVersion, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download kind %s: server returned %s", KindVersion, response.Status)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".kind-*")
	if err != nil {
		return "", fmt.Errorf("create kind temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	defer func() { _ = temporary.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), response.Body); err != nil {
		return "", fmt.Errorf("write kind download: %w", err)
	}
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if actual != expected {
		return "", fmt.Errorf("verify kind checksum: expected %s, got %s", expected, actual)
	}
	if err := temporary.Chmod(0o700); err != nil {
		return "", fmt.Errorf("make kind executable: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync kind download: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close kind download: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("install kind: %w", err)
	}
	return path, nil
}

func validFileSHA256(path, expected string) bool {
	// #nosec G304 -- path is constrained to the application-managed helper cache.
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}
	return fmt.Sprintf("%x", hash.Sum(nil)) == expected
}
