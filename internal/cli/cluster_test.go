package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/buildinfo"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cli"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cluster"
	progressapi "github.com/course-go-autumn-2026/tripgo-infra/internal/progress"
)

type fakeCluster struct {
	status           cluster.Status
	inspectErr       error
	resourceWarnings []string
	startErr         error
	starts           int
	stops            int
	startProgress    []progressapi.Event
}

func (f *fakeCluster) Start(ctx context.Context) (cluster.Status, error) {
	f.starts++
	for _, event := range f.startProgress {
		progressapi.Report(ctx, event.Kind, event.Message)
	}
	return f.status, f.startErr
}

func (f *fakeCluster) Inspect(context.Context) (cluster.Status, error) {
	return f.status, f.inspectErr
}

func (f *fakeCluster) Stop(context.Context, []string) error {
	f.stops++
	return nil
}

func (f *fakeCluster) EnvironmentResourceWarnings(context.Context, int, int) []string {
	return append([]string(nil), f.resourceWarnings...)
}

func TestClusterStartCanceledContextExits130ThroughCommandBoundary(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	application := &fakeCluster{startErr: fmt.Errorf("%w: Docker operation: %w", cluster.ErrPrerequisite, ctx.Err())}
	root := testRoot(t, cli.Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cluster: application})
	root.SetArgs([]string{"cluster", "start"})
	err := root.ExecuteContext(ctx)
	if err == nil || cli.ExitCode(err) != 130 {
		t.Fatalf("canceled cluster start error = %v, exit code = %d", err, cli.ExitCode(err))
	}
}

func TestClusterStartOutput(t *testing.T) {
	t.Parallel()

	application := &fakeCluster{status: cluster.Status{ClusterID: "identity", Ready: true, RegistryReady: true, KindVersion: "v0.27.0", Kubernetes: "v1.32.2", KindConfigHash: "config-hash"}}
	stdout := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdout: stdout, Stderr: &bytes.Buffer{}, Cluster: application})
	root.SetArgs([]string{"cluster", "start"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Cluster: tripgo-local", "Status:  ready", "Registry: localhost:5001 (ready)", "Identity: identity", "Port mappings: config-hash"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("output does not contain %q:\n%s", expected, stdout)
		}
	}
	if application.starts != 1 {
		t.Fatalf("Start calls = %d", application.starts)
	}
}

func TestClusterStartKeepsProgressOnStderrAndFinalStatusOnStdout(t *testing.T) {
	t.Parallel()
	application := &fakeCluster{
		status:        cluster.Status{Ready: true, RegistryReady: true},
		startProgress: []progressapi.Event{{Kind: progressapi.Stage, Message: "Creating Kubernetes cluster"}},
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdout: stdout, Stderr: stderr, Cluster: application})
	root.SetArgs([]string{"cluster", "start"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "Creating Kubernetes") || !strings.Contains(stdout.String(), "Cluster: tripgo-local") {
		t.Fatalf("stdout contract changed: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Creating Kubernetes cluster") {
		t.Fatalf("progress did not reach stderr: %q", stderr.String())
	}
}

func TestClusterStopRequiresYesOutsideTerminal(t *testing.T) {
	t.Parallel()

	application := &fakeCluster{status: cluster.Status{Ready: true}}
	root := testRoot(t, cli.Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cluster: application, IsTerminal: func() bool { return false }})
	root.SetArgs([]string{"cluster", "stop"})
	err := root.Execute()
	if err == nil || cli.ExitCode(err) != 2 || application.stops != 0 {
		t.Fatalf("stop error = %v, code = %d, calls = %d", err, cli.ExitCode(err), application.stops)
	}
}

func TestClusterStopConfirmation(t *testing.T) {
	t.Parallel()

	application := &fakeCluster{status: cluster.Status{Ready: true, KnownNamespaces: []string{"tripgo-lab-01"}}}
	stdout := &bytes.Buffer{}
	root := testRoot(t, cli.Dependencies{Stdin: strings.NewReader("yes\n"), Stdout: stdout, Stderr: &bytes.Buffer{}, Cluster: application, IsTerminal: func() bool { return true }})
	root.SetArgs([]string{"cluster", "stop"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Environments:", "  - tripgo-lab-01", "all environment PVC data", "Type yes to continue:"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("stop output does not contain %q: %q", expected, stdout.String())
		}
	}
	if application.stops != 1 {
		t.Fatalf("stop calls = %d", application.stops)
	}
}

func TestClusterStopCanceled(t *testing.T) {
	t.Parallel()

	application := &fakeCluster{status: cluster.Status{Ready: true}}
	root := testRoot(t, cli.Dependencies{Stdin: strings.NewReader("no\n"), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cluster: application, IsTerminal: func() bool { return true }})
	root.SetArgs([]string{"cluster", "stop"})
	err := root.Execute()
	if err == nil || cli.ExitCode(err) != 130 || application.stops != 0 {
		t.Fatalf("stop error = %v, code = %d", err, cli.ExitCode(err))
	}
}

func TestClusterArgumentErrorUsesCodeTwo(t *testing.T) {
	t.Parallel()

	application := &fakeCluster{}
	root := testRoot(t, cli.Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cluster: application})
	root.SetArgs([]string{"cluster", "status", "unexpected"})
	err := root.Execute()
	if err == nil || cli.ExitCode(err) != 2 {
		t.Fatalf("argument error = %v, code = %d", err, cli.ExitCode(err))
	}
}

func TestExitCodesForClusterErrors(t *testing.T) {
	t.Parallel()

	if code := cli.ExitCode(cluster.ErrPrerequisite); code != 3 {
		t.Errorf("prerequisite code = %d", code)
	}
	if code := cli.ExitCode(cluster.ErrConflict); code != 4 {
		t.Errorf("conflict code = %d", code)
	}
	if code := cli.ExitCode(errors.New("other")); code != 1 {
		t.Errorf("generic code = %d", code)
	}
}

func testRoot(t *testing.T, deps cli.Dependencies) *cobra.Command {
	t.Helper()
	if deps.Stdout == nil {
		deps.Stdout = &bytes.Buffer{}
	}
	if deps.Stderr == nil {
		deps.Stderr = &bytes.Buffer{}
	}
	if deps.WorkingDirectory == nil {
		deps.WorkingDirectory = func() (string, error) { return "/lab", nil }
	}
	if deps.ProgressCacheDirectory == nil {
		cache := t.TempDir()
		deps.ProgressCacheDirectory = func() (string, error) { return cache, nil }
	}
	deps.Build = buildinfo.Current("tripgoctl")
	root, err := cli.NewRootCommand(deps)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
