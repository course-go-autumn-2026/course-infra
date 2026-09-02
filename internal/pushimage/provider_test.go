package pushimage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
	progressapi "github.com/course-go-autumn-2026/tripgo-infra/internal/progress"
)

type recordedRunner struct {
	calls [][]string
	fail  string
}

func (r *recordedRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	if name == r.fail {
		return "", errors.New("failed")
	}
	if len(args) >= 2 && args[0] == "image" && args[1] == "inspect" {
		return `["localhost:5001/tripgo-push-service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]`, nil
	}
	return "", nil
}

type testReporter struct {
	mu     sync.Mutex
	events []progressapi.Event
}

func (r *testReporter) Report(event progressapi.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func TestEnsureRejectsMissingReleaseAssetBeforeDocker(t *testing.T) {
	runner := &recordedRunner{}
	provider := &Provider{runner: runner, goarch: "amd64"}
	_, err := provider.Ensure(t.Context())
	if err == nil || !strings.Contains(err.Error(), "release tripgoctl") {
		t.Fatalf("Ensure() error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("Docker unexpectedly called: %v", runner.calls)
	}
}

func TestExecRunnerStreamsBuildOutputButNotInspectOutput(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fake-docker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'step output\\n'\n"), 0o700); err != nil { // #nosec G306 -- executable is isolated test data.
		t.Fatal(err)
	}
	reporter := &testReporter{}
	ctx := progressapi.WithReporter(t.Context(), reporter)
	if _, err := (execRunner{}).Run(ctx, path, "build"); err != nil {
		t.Fatal(err)
	}
	if len(reporter.events) != 1 || reporter.events[0].Message != "step output" {
		t.Fatalf("build progress = %+v", reporter.events)
	}
	reporter = &testReporter{}
	ctx = progressapi.WithReporter(t.Context(), reporter)
	if _, err := (execRunner{}).Run(ctx, path, "image", "inspect"); err != nil {
		t.Fatal(err)
	}
	if len(reporter.events) != 0 {
		t.Fatalf("inspect output was streamed: %+v", reporter.events)
	}
}

func TestExecRunnerOmitsEmptyCommandOutput(t *testing.T) {
	_, err := (execRunner{}).Run(t.Context(), "false")
	if err == nil {
		t.Fatal("false command unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), ": :") {
		t.Fatalf("empty command output leaked into error: %v", err)
	}
}

func TestImmutableLocalReferenceRejectsRemoteOrMutableReferences(t *testing.T) {
	valid := catalog.LocalPushImageRepository + "@sha256:" + strings.Repeat("a", 64)
	if !immutableLocalReference.MatchString(valid) {
		t.Fatal("valid local digest rejected")
	}
	for _, invalid := range []string{
		"localhost:5001/tripgo-push-service:latest",
		"ghcr.io/course/tripgo-push-service@sha256:" + strings.Repeat("a", 64),
		"localhost:5001/other@sha256:" + strings.Repeat("a", 64),
	} {
		if immutableLocalReference.MatchString(invalid) {
			t.Errorf("unsafe reference %q accepted", invalid)
		}
	}
}
