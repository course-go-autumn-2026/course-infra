// Package cli contains the tripgoctl command tree and application wiring.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/buildinfo"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cluster"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/lab"
)

// Dependencies are process-level capabilities supplied by cmd/tripgoctl.
// WorkingDirectory is injected so application packages never rely on a global cwd.
type Dependencies struct {
	Stdin                  io.Reader
	Stdout                 io.Writer
	Stderr                 io.Writer
	WorkingDirectory       func() (string, error)
	IsTerminal             func() bool
	IsStderrTerminal       func() bool
	ProgressCacheDirectory func() (string, error)
	Build                  buildinfo.Info
	Cluster                ClusterLifecycle
	Environment            EnvironmentLifecycle
	Doctor                 Doctor
}

// Doctor performs read-only local prerequisite diagnostics.
type Doctor interface {
	Diagnose(context.Context) cluster.DiagnosticReport
}

// ClusterLifecycle is the CLI-facing cluster application boundary.
type ClusterLifecycle interface {
	Start(context.Context) (cluster.Status, error)
	Inspect(context.Context) (cluster.Status, error)
	Stop(context.Context, []string) error
	EnvironmentResourceWarnings(context.Context, int, int) []string
}

// EnvironmentLifecycle is the CLI-facing lab application boundary.
type EnvironmentLifecycle interface {
	ConfiguredLab(string) (int, error)
	Start(context.Context, string, string) (lab.RuntimeStatus, error)
	Status(context.Context, string) (lab.RuntimeStatus, error)
	Stop(context.Context, string, string) (lab.RuntimeStatus, error)
	Reset(context.Context, string) error
	List(context.Context) ([]lab.RuntimeStatus, error)
	Logs(context.Context, string, string, bool, int64, io.Writer) error
}

// NewRootCommand constructs an isolated command tree.
func NewRootCommand(deps Dependencies) (*cobra.Command, error) {
	if deps.Stdout == nil {
		return nil, errors.New("stdout is required")
	}
	if deps.Stderr == nil {
		return nil, errors.New("stderr is required")
	}
	if deps.WorkingDirectory == nil {
		return nil, errors.New("working directory provider is required")
	}

	if deps.Stdin == nil {
		deps.Stdin = strings.NewReader("")
	}
	if deps.IsTerminal == nil {
		deps.IsTerminal = func() bool { return false }
	}
	if deps.IsStderrTerminal == nil {
		deps.IsStderrTerminal = func() bool { return false }
	}
	if deps.ProgressCacheDirectory == nil {
		deps.ProgressCacheDirectory = os.UserCacheDir
	}
	if deps.Cluster == nil {
		service, err := cluster.NewDefault(deps.Stderr)
		if err != nil {
			return nil, err
		}
		deps.Cluster = service
		deps.Doctor = service
		if deps.Environment == nil {
			deps.Environment = lab.NewService(service)
		}
	}

	root := &cobra.Command{
		Use:           "tripgoctl",
		Short:         "Manage local TripGo lab environments",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          noArgs,
	}
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &exitError{code: 2, err: err}
	})
	root.CompletionOptions.DisableDefaultCmd = true

	if deps.Doctor == nil {
		if doctor, ok := deps.Cluster.(Doctor); ok {
			deps.Doctor = doctor
		}
	}

	root.AddCommand(
		newVersionCommand(deps.Build),
		newDoctorCommand(deps),
		newClusterCommand(deps),
		newEnvironmentCommand(deps),
		newConnectCommand(deps),
	)

	return root, nil
}

func newVersionCommand(info buildinfo.Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print tripgoctl build metadata",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), info.String())
			return err
		},
	}
}
