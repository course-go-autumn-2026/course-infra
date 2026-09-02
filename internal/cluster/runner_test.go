package cluster

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	progressapi "github.com/course-go-autumn-2026/tripgo-infra/internal/progress"
)

type progressRecorder struct {
	mu     sync.Mutex
	events []progressapi.Event
}

func (r *progressRecorder) Report(event progressapi.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *progressRecorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var messages []string
	for _, event := range r.events {
		messages = append(messages, event.Message)
	}
	return strings.Join(messages, "\n")
}

func TestSafeProgressCommandAllowsManagedRegistryButNotKubeconfig(t *testing.T) {
	t.Parallel()
	if !safeProgressCommand("docker", []string{"run", "-d", "--name", RegistryContainer, "registry:2"}) {
		t.Fatal("managed registry output was not allowlisted")
	}
	if safeProgressCommand("docker", []string{"run", "--name", "unmanaged", "image"}) {
		t.Fatal("unmanaged docker output was allowlisted")
	}
	if safeProgressCommand("kind", []string{"get", "kubeconfig"}) {
		t.Fatal("kubeconfig output was allowlisted")
	}
}

func TestExecRunnerStreamsOnlyAllowlistedCommandOutput(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "kind")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'creating node\\n'\nprintf 'waiting for API\\n' >&2\n"), 0o700); err != nil { // #nosec G306 -- executable is isolated test data.
		t.Fatal(err)
	}
	recorder := &progressRecorder{}
	ctx := progressapi.WithReporter(context.Background(), recorder)
	if _, err := (execRunner{}).Run(ctx, path, "create", "cluster"); err != nil {
		t.Fatal(err)
	}
	if got := recorder.text(); !strings.Contains(got, "creating node") || !strings.Contains(got, "waiting for API") {
		t.Fatalf("streamed activity = %q", got)
	}

	recorder = &progressRecorder{}
	ctx = progressapi.WithReporter(context.Background(), recorder)
	if _, err := (execRunner{}).Run(ctx, path, "get", "kubeconfig"); err != nil {
		t.Fatal(err)
	}
	if got := recorder.text(); got != "" {
		t.Fatalf("sensitive command output was streamed: %q", got)
	}
}
