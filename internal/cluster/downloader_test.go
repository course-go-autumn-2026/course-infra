package cluster

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloaderCachesVerifiedKind(t *testing.T) {
	t.Parallel()

	content := []byte("fake-kind-binary")
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/kind-linux-amd64" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write(content)
	}))
	defer server.Close()

	cache := t.TempDir()
	downloader := Downloader{BaseURL: server.URL, Checksums: map[string]string{"linux/amd64": digest}}
	path, err := downloader.EnsureKind(context.Background(), cache, "linux", "amd64")
	if err != nil {
		t.Fatalf("EnsureKind() error = %v", err)
	}
	if requests.Load() != 1 || !validFileSHA256(path, digest) {
		t.Fatalf("download requests = %d, path = %s", requests.Load(), path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("cached mode = %v, error = %v", info.Mode(), err)
	}
	if _, err := downloader.EnsureKind(context.Background(), cache, "linux", "amd64"); err != nil {
		t.Fatalf("cached EnsureKind() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("cached call made %d requests", requests.Load())
	}

	// #nosec G306 -- the test intentionally corrupts an executable helper cache entry.
	if err := os.WriteFile(path, []byte("corrupt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.EnsureKind(context.Background(), cache, "linux", "amd64"); err != nil {
		t.Fatalf("repair EnsureKind() error = %v", err)
	}
	if requests.Load() != 2 || !validFileSHA256(path, digest) {
		t.Fatalf("corrupt helper was not repaired")
	}
}

func TestDownloaderCancellationDoesNotInstallPartialHelper(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	cache := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	downloader := Downloader{BaseURL: server.URL, Checksums: map[string]string{"linux/amd64": strings.Repeat("0", 64)}}
	if _, err := downloader.EnsureKind(ctx, cache, "linux", "amd64"); err == nil {
		t.Fatal("canceled helper download succeeded")
	}
	if _, err := os.Stat(filepath.Join(cache, "kind", KindVersion, "kind")); !os.IsNotExist(err) {
		t.Fatalf("partial helper remains after cancellation: %v", err)
	}
}

func TestDownloaderUsesWarmedCacheOffline(t *testing.T) {
	t.Parallel()
	content := []byte("verified kind")
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(content) }))
	cache := t.TempDir()
	downloader := Downloader{BaseURL: server.URL, Checksums: map[string]string{"linux/amd64": digest}}
	first, err := downloader.EnsureKind(context.Background(), cache, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	second, err := downloader.EnsureKind(context.Background(), cache, "linux", "amd64")
	if err != nil || second != first {
		t.Fatalf("offline warmed-cache EnsureKind() = %q, %v", second, err)
	}
}

func TestDownloaderRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("unexpected"))
	}))
	defer server.Close()
	cache := t.TempDir()
	downloader := Downloader{BaseURL: server.URL, Checksums: map[string]string{"linux/amd64": strings.Repeat("0", 64)}}
	_, err := downloader.EnsureKind(context.Background(), cache, "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("EnsureKind() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cache, "kind", KindVersion, "kind")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid helper was installed: %v", statErr)
	}
}

func TestDownloaderRejectsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	_, err := (Downloader{}).EnsureKind(context.Background(), t.TempDir(), "windows", "amd64")
	if err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("EnsureKind() error = %v", err)
	}
}
