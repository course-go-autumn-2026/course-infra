package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/cli"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cluster"
)

type fakeDoctor struct{ report cluster.DiagnosticReport }

func (f fakeDoctor) Diagnose(context.Context) cluster.DiagnosticReport { return f.report }

func TestDoctorWritesExclusiveSecretFreeBundle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "doctor.json")
	stdout := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{
		Stdout: stdout, Stderr: &bytes.Buffer{},
		Doctor: fakeDoctor{report: cluster.DiagnosticReport{SchemaVersion: 1, Platform: "linux/amd64", Checks: []cluster.DiagnosticCheck{{Name: "docker-daemon", Status: "ok", Detail: "available"}}}},
	})
	root.SetArgs([]string{"doctor", "--bundle", path})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"schema_version": 1`) || !strings.Contains(stdout.String(), "docker-daemon") {
		t.Fatalf("stdout=%s bundle=%s", stdout, data)
	}
	root = testRoot(t, cli.Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Doctor: fakeDoctor{}})
	root.SetArgs([]string{"doctor", "--bundle", path})
	if err := root.Execute(); err == nil {
		t.Fatal("doctor overwrote an existing bundle")
	}
}
