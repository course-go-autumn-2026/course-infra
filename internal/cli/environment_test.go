package cli_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/cli"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cluster"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/lab"
)

type fakeEnvironment struct {
	status      lab.RuntimeStatus
	items       []lab.RuntimeStatus
	startCWD    string
	startVer    string
	logsFollow  bool
	logsTail    int64
	resetCalls  int
	statusCalls int
}

func (f *fakeEnvironment) ConfiguredLab(string) (int, error) {
	return f.status.Lab, nil
}

func (f *fakeEnvironment) Start(_ context.Context, cwd, version string) (lab.RuntimeStatus, error) {
	f.startCWD, f.startVer = cwd, version
	return f.status, nil
}

func (f *fakeEnvironment) Status(context.Context, string) (lab.RuntimeStatus, error) {
	f.statusCalls++
	return f.status, nil
}

func (f *fakeEnvironment) Stop(context.Context, string, string) (lab.RuntimeStatus, error) {
	return f.status, nil
}

func (f *fakeEnvironment) Reset(context.Context, string) error {
	f.resetCalls++
	return nil
}

func (f *fakeEnvironment) List(context.Context) ([]lab.RuntimeStatus, error) {
	return f.items, nil
}

func (f *fakeEnvironment) Logs(_ context.Context, _, _ string, follow bool, tail int64, output io.Writer) error {
	f.logsFollow, f.logsTail = follow, tail
	_, err := io.WriteString(output, "push log\n")
	return err
}

func TestEnvironmentStartAndConnectOutput(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 1, Namespace: "tripgo-lab-01", State: "ready", Components: []lab.ComponentStatus{{Name: "postgres", Desired: 1, Ready: 1, ImageDigest: "sha256:digest"}}}}
	stdout := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdout: stdout, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{status: cluster.Status{Ready: true}}, Environment: application})
	root.SetArgs([]string{"environment", "start"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Environment: tripgo-lab-01", "Status:      ready", "postgres: desired=1 ready=1 image=sha256:digest", "Next: tripgoctl connect"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("start output does not contain %q:\n%s", expected, stdout)
		}
	}
	if application.startCWD != "/lab" || application.startVer == "" {
		t.Fatalf("Start args = %q, %q", application.startCWD, application.startVer)
	}

	stdout.Reset()
	root.SetArgs([]string{"connect"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"PostgreSQL  localhost:21032", "DATABASE_URL=postgres://tripgo:tripgo@localhost:21032", `psql "postgres://tripgo:tripgo@localhost:21032/tripgo?sslmode=disable"`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("connect output does not contain %q:\n%s", expected, stdout)
		}
	}
	if application.statusCalls != 1 {
		t.Fatalf("connect performed %d status reads, want exactly one", application.statusCalls)
	}
}

func TestEnvironmentStartPrintsResourceWarningsBeforeLifecycle(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 4, Namespace: "tripgo-lab-04", State: "ready"}}
	stderr := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Cluster: &fakeCluster{status: cluster.Status{RunningNamespaces: []string{"tripgo-lab-02"}}, resourceWarnings: []string{"Warning: bounded memory"}}, Environment: application})
	root.SetArgs([]string{"environment", "start"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "Warning: bounded memory") {
		t.Fatalf("resource warning output = %q", stderr.String())
	}
}

func TestConnectPrintsLab2ObservabilityEndpoints(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 2, Namespace: "tripgo-lab-02", State: "ready"}}
	stdout := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdout: stdout, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{}, Environment: application})
	root.SetArgs([]string{"connect"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"PostgreSQL  localhost:22032",
		"Grafana     http://localhost:22300",
		"CREDENTIALS  USER   PASSWORD",
		"Grafana      admin  admin",
		"OTel gRPC   localhost:22317",
		"OTel HTTP   http://localhost:22318",
		"Jaeger      http://localhost:22686",
		"Prometheus  http://localhost:22909",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("connect output does not contain %q:\n%s", expected, stdout)
		}
	}
}

func TestConnectPrintsLab3PushEndpointsAndLogsCommand(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 3, Namespace: "tripgo-lab-03", State: "ready"}}
	stdout := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdout: stdout, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{}, Environment: application})
	root.SetArgs([]string{"connect"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"http://localhost:23809", "localhost:23905", "environment logs push-service --follow"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("connect output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestEnvironmentLogsWritesServiceOutput(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	application := &fakeEnvironment{}
	root := testRoot(t, cli.Dependencies{Stdout: stdout, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{}, Environment: application})
	root.SetArgs([]string{"environment", "logs", "push-service", "--follow", "--tail", "42"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "push log\n" || !application.logsFollow || application.logsTail != 42 {
		t.Fatalf("logs output/options = %q, follow=%v tail=%d", stdout.String(), application.logsFollow, application.logsTail)
	}
}

func TestConnectPrintsLab4RedpandaEndpointsAndCommands(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 4, Namespace: "tripgo-lab-04", State: "ready"}}
	stdout := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdout: stdout, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{}, Environment: application})
	root.SetArgs([]string{"connect"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Kafka       localhost:24092",
		"Console     http://localhost:24081",
		"rpk topic list --brokers localhost:24092",
		"environment logs redpanda --follow",
		"environment logs redpanda-console --follow",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("connect output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestEnvironmentResetRequiresConfirmation(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 1, Namespace: "tripgo-lab-01", State: "ready"}}
	root := testRoot(t, cli.Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{}, Environment: application, IsTerminal: func() bool { return false }})
	root.SetArgs([]string{"environment", "reset"})
	err := root.Execute()
	if err == nil || cli.ExitCode(err) != 2 || application.resetCalls != 0 {
		t.Fatalf("reset error = %v, code = %d, calls = %d", err, cli.ExitCode(err), application.resetCalls)
	}

	root.SetArgs([]string{"environment", "reset", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if application.resetCalls != 1 {
		t.Fatalf("reset calls = %d", application.resetCalls)
	}
}

func TestConnectRequiresReadyEnvironment(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 1, Namespace: "tripgo-lab-01", State: "stopped"}}
	root := testRoot(t, cli.Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{}, Environment: application})
	root.SetArgs([]string{"connect"})
	err := root.Execute()
	if err == nil || cli.ExitCode(err) != 3 {
		t.Fatalf("connect error = %v, code = %d", err, cli.ExitCode(err))
	}
}

func TestEnvironmentResetRejectsWhitespacePaddedConfirmation(t *testing.T) {
	t.Parallel()
	application := &fakeEnvironment{status: lab.RuntimeStatus{Lab: 1, Namespace: "tripgo-lab-01", State: "ready"}}
	root := testRoot(t, cli.Dependencies{Stdin: strings.NewReader(" yes \n"), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cluster: &fakeCluster{}, Environment: application, IsTerminal: func() bool { return true }})
	root.SetArgs([]string{"environment", "reset"})
	err := root.Execute()
	if cli.ExitCode(err) != 130 || application.resetCalls != 0 {
		t.Fatalf("reset cancellation code = %d, calls = %d", cli.ExitCode(err), application.resetCalls)
	}
}
