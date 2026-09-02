package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/buildinfo"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cli"
)

func TestExitCodeMapsContextCancellationAndTimeout(t *testing.T) {
	t.Parallel()
	if got := cli.ExitCode(context.Canceled); got != 130 {
		t.Fatalf("canceled exit code = %d", got)
	}
	if got := cli.ExitCode(context.DeadlineExceeded); got != 3 {
		t.Fatalf("timeout exit code = %d", got)
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	root, err := cli.NewRootCommand(cli.Dependencies{
		Stdout:           stdout,
		Stderr:           &bytes.Buffer{},
		WorkingDirectory: func() (string, error) { return "/lab", nil },
		Build: buildinfo.Info{
			Name:    "tripgoctl",
			Version: "v1.0.0",
			Commit:  "abcdef1",
			BuiltAt: "2026-09-01T00:00:00Z",
		},
	})
	if err != nil {
		t.Fatalf("NewRootCommand() error = %v", err)
	}
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "tripgoctl v1.0.0 (commit abcdef1, built 2026-09-01T00:00:00Z)\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestHelpContainsPublicCommands(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	root, err := cli.NewRootCommand(cli.Dependencies{
		Stdout:           stdout,
		Stderr:           &bytes.Buffer{},
		WorkingDirectory: func() (string, error) { return "/lab", nil },
		Build:            buildinfo.Current("tripgoctl"),
	})
	if err != nil {
		t.Fatalf("NewRootCommand() error = %v", err)
	}
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, command := range []string{"cluster", "environment", "connect", "doctor", "version"} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help output does not contain %q:\n%s", command, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "completion") {
		t.Errorf("help unexpectedly exposes completion command:\n%s", stdout.String())
	}
}
