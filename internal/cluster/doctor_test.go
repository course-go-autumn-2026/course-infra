package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type lowMemoryRunner struct{ fakeRunner }

func (r *lowMemoryRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	if name == "docker" && len(arguments) > 0 && arguments[0] == "info" {
		return "2147483648", nil
	}
	return r.fakeRunner.Run(ctx, name, arguments...)
}

type restartableRunner struct {
	fakeRunner
	unavailable bool
}

func (r *restartableRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	if r.unavailable && name == "docker" {
		return "", errors.New("daemon unavailable")
	}
	return r.fakeRunner.Run(ctx, name, arguments...)
}

func TestDoctorRecoversAfterDockerDaemonRestart(t *testing.T) {
	t.Parallel()
	runner := &restartableRunner{unavailable: true}
	service := &Service{runner: runner, cacheDir: t.TempDir(), configDir: t.TempDir(), goos: "linux", goarch: "amd64"}
	first := service.Diagnose(context.Background())
	runner.unavailable = false
	second := service.Diagnose(context.Background())
	status := func(report DiagnosticReport) string {
		for _, check := range report.Checks {
			if check.Name == "docker-daemon" {
				return check.Status
			}
		}
		return "missing"
	}
	if status(first) != "error" || status(second) != "ok" {
		t.Fatalf("daemon statuses before/after restart = %s/%s", status(first), status(second))
	}
}

func TestDoctorBundleIsSecretFreeAndReportsCorruptState(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := &Service{runner: &lowMemoryRunner{}, cacheDir: filepath.Join(root, "cache"), configDir: filepath.Join(root, "config"), goos: "linux", goarch: "amd64"}
	if err := os.MkdirAll(service.configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.statePath(), []byte(`{"cluster_id":"postgres://user:password@host"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := service.Diagnose(context.Background())
	data, err := MarshalDiagnosticBundle(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "password") || strings.Contains(string(data), "cluster_id") {
		t.Fatalf("bundle leaks state content: %s", data)
	}
	var decoded DiagnosticReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	found := false
	lowMemory := false
	for _, check := range decoded.Checks {
		if check.Name == "local-state" && check.Status == "error" {
			found = true
		}
		if check.Name == "docker-memory" && check.Status == "warning" && strings.Contains(check.Detail, "2.0 GiB") {
			lowMemory = true
		}
	}
	if !found || !lowMemory {
		t.Fatalf("doctor checks = %#v", decoded.Checks)
	}
}
