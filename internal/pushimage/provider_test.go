package pushimage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
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
