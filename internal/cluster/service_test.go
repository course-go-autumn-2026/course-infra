package cluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, ...string) (string, error) {
	return "", errors.New("daemon unavailable")
}

type canceledRunner struct{}

func (canceledRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	return "", ctx.Err()
}

type blockingCleanupRunner struct {
	fakeRunner
	cleanupTimeouts int
	cancelParent    context.CancelFunc
}

func (r *blockingCleanupRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	if name == "docker" && len(arguments) > 0 && arguments[0] == "network" {
		if r.cancelParent != nil {
			r.cancelParent()
		}
		return "", errors.New("injected network connect failure")
	}
	if (name == "docker" && len(arguments) > 0 && arguments[0] == "rm") ||
		(name != "docker" && len(arguments) > 0 && arguments[0] == "delete") {
		<-ctx.Done()
		r.cleanupTimeouts++
		return "", ctx.Err()
	}
	return r.fakeRunner.Run(ctx, name, arguments...)
}

type fakeRunner struct {
	cluster  bool
	registry bool
	marker   string
	calls    []string
}

func (f *fakeRunner) Run(_ context.Context, name string, arguments ...string) (string, error) {
	call := name + " " + strings.Join(arguments, " ")
	f.calls = append(f.calls, call)
	if name != "docker" {
		switch strings.Join(arguments, " ") {
		case "get clusters":
			if f.cluster {
				return Name, nil
			}
			return "", nil
		default:
			if len(arguments) >= 2 && arguments[0] == "create" && arguments[1] == "cluster" {
				f.cluster = true
				return "", nil
			}
			if len(arguments) >= 2 && arguments[0] == "delete" && arguments[1] == "cluster" {
				f.cluster = false
				return "", nil
			}
		}
	}
	if len(arguments) == 0 {
		return "", nil
	}
	switch arguments[0] {
	case "version":
		return "26.1.0", nil
	case "info":
		return "8589934592", nil
	case "ps":
		if f.registry {
			return RegistryContainer, nil
		}
		return "", nil
	case "run":
		f.registry = true
		for _, argument := range arguments {
			if strings.HasPrefix(argument, "tripgo.course/cluster-id=") {
				f.marker = strings.TrimPrefix(argument, "tripgo.course/cluster-id=")
			}
		}
		return "registry-id", nil
	case "network":
		return "", nil
	case "exec":
		if len(arguments) >= 4 && arguments[2] == "sh" {
			return "", nil
		}
		if len(arguments) >= 3 && arguments[2] == "cat" {
			return f.marker, nil
		}
	case "inspect":
		joined := strings.Join(arguments, " ")
		if strings.Contains(joined, "Config.Labels") {
			return "tripgoctl|" + f.marker, nil
		}
		return "true", nil
	case "rm":
		f.registry = false
		return RegistryContainer, nil
	}
	return "", fmt.Errorf("unexpected command: %s", call)
}

func TestEnvironmentResourceWarningsAreBoundedAndNonBlocking(t *testing.T) {
	t.Parallel()
	service := &Service{runner: &fakeRunner{}}
	if warnings := service.EnvironmentResourceWarnings(t.Context(), 1, 4); len(warnings) != 0 {
		t.Fatalf("lab 1 warnings = %v", warnings)
	}
	warnings := service.EnvironmentResourceWarnings(t.Context(), 4, 2)
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "2 environment(s) are already running") || !strings.Contains(joined, "approximately 10 GiB") {
		t.Fatalf("heavy environment warnings = %v", warnings)
	}
}

func TestServiceStartPreservesCanceledDockerContext(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := &Service{
		stderr: bytes.NewBuffer(nil), runner: canceledRunner{},
		cacheDir: filepath.Join(root, "cache"), configDir: filepath.Join(root, "config"),
		goos: "linux", goarch: "amd64", portConflicts: func() []int { return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Start(ctx)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrPrerequisite) {
		t.Fatalf("Start() cancellation error = %v", err)
	}
}

func TestServiceReportsUnavailableDocker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	service := &Service{
		stderr: bytes.NewBuffer(nil), runner: failingRunner{},
		cacheDir: filepath.Join(root, "cache"), configDir: filepath.Join(root, "config"),
		goos: "linux", goarch: "amd64", portConflicts: func() []int { return nil },
	}
	_, err := service.Start(context.Background())
	if err == nil || !errors.Is(err, ErrPrerequisite) || !strings.Contains(err.Error(), "Docker daemon") {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestFailedStartCleanupHasFreshFixedBound(t *testing.T) {
	t.Parallel()
	binary := []byte("fake kind")
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(binary)
	}))
	defer server.Close()

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	runner := &blockingCleanupRunner{cancelParent: cancelParent}
	root := t.TempDir()
	service := &Service{
		stderr: bytes.NewBuffer(nil), runner: runner,
		download: Downloader{BaseURL: server.URL, Checksums: map[string]string{"linux/amd64": digest}},
		cacheDir: filepath.Join(root, "cache"), configDir: filepath.Join(root, "config"),
		goos: "linux", goarch: "amd64", portConflicts: func() []int { return nil }, cleanupTimeout: 20 * time.Millisecond,
		kubernetesAccess: func(_ context.Context, _, clusterID string, _ bool) (Access, error) {
			return Access{ClusterID: clusterID}, nil
		},
	}
	started := time.Now()
	_, err := service.Start(parent)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "network connect failure") {
		t.Fatalf("Start() error = %v", err)
	}
	if runner.cleanupTimeouts != 2 {
		t.Fatalf("bounded cleanup calls = %d, want kind delete and registry remove", runner.cleanupTimeouts)
	}
	if elapsed > time.Second {
		t.Fatalf("failed start cleanup exceeded bound: %s", elapsed)
	}
}

func TestServiceLifecycleAndIdempotency(t *testing.T) {
	t.Parallel()

	binary := []byte("fake kind")
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(binary)
	}))
	defer server.Close()

	runner := &fakeRunner{}
	root := t.TempDir()
	service := &Service{
		stderr: bytes.NewBuffer(nil), runner: runner,
		download: Downloader{BaseURL: server.URL, Checksums: map[string]string{"linux/amd64": digest}},
		cacheDir: filepath.Join(root, "cache"), configDir: filepath.Join(root, "config"),
		goos: "linux", goarch: "amd64", portConflicts: func() []int { return nil },
		kubernetesAccess: func(_ context.Context, _, clusterID string, _ bool) (Access, error) {
			return Access{ClusterID: clusterID}, nil
		},
	}

	status, err := service.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !status.Ready || !status.RegistryReady || status.ClusterID == "" || runner.marker != status.ClusterID {
		t.Fatalf("Start() status = %#v, marker = %q", status, runner.marker)
	}
	firstCalls := len(runner.calls)
	second, err := service.Start(context.Background())
	if err != nil {
		t.Fatalf("idempotent Start() error = %v", err)
	}
	if second.ClusterID != status.ClusterID || len(runner.calls) <= firstCalls {
		t.Fatalf("idempotent Start() status = %#v", second)
	}
	inspected, err := service.Inspect(context.Background())
	if err != nil || !inspected.Ready || inspected.ClusterID != status.ClusterID {
		t.Fatalf("Inspect() = %#v, %v", inspected, err)
	}
	lease, err := service.BeginEnvironmentOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.SetEnvironmentState("tripgo-lab-01", true, true); err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(context.Background(), nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("concurrent Stop() error = %v", err)
	}
	lease.Close()
	if err := service.Stop(context.Background(), nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale-confirmation Stop() error = %v", err)
	}
	if err := service.Stop(context.Background(), []string{"tripgo-lab-01"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if runner.cluster || runner.registry {
		t.Fatalf("resources remain: cluster=%v registry=%v", runner.cluster, runner.registry)
	}
	if _, err := os.Stat(service.statePath()); !os.IsNotExist(err) {
		t.Fatalf("state remains after stop: %v", err)
	}
	if err := service.Stop(context.Background(), nil); err != nil {
		t.Fatalf("idempotent Stop() error = %v", err)
	}
}

func TestServiceReportsAllInjectedPortConflicts(t *testing.T) {
	t.Parallel()

	binary := []byte("fake kind")
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(binary) }))
	defer server.Close()
	runner := &fakeRunner{}
	root := t.TempDir()
	service := &Service{
		stderr: bytes.NewBuffer(nil), runner: runner,
		download: Downloader{BaseURL: server.URL, Checksums: map[string]string{"linux/amd64": digest}},
		cacheDir: filepath.Join(root, "cache"), configDir: filepath.Join(root, "config"),
		goos: "linux", goarch: "amd64", portConflicts: func() []int { return []int{5001, 21032, 22032} },
	}
	_, err := service.Start(context.Background())
	if err == nil || !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "5001, 21032, 22032") {
		t.Fatalf("Start() error = %v", err)
	}
	if runner.registry || runner.cluster {
		t.Fatal("Start() mutated resources after port conflict")
	}
}
