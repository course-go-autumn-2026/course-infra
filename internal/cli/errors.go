package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/cluster"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/environment"
)

var errCanceled = errors.New("operation canceled")

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func noArgs(command *cobra.Command, arguments []string) error {
	if err := cobra.NoArgs(command, arguments); err != nil {
		return &exitError{code: 2, err: err}
	}
	return nil
}

func exactArgs(count int) cobra.PositionalArgs {
	return func(command *cobra.Command, arguments []string) error {
		if err := cobra.ExactArgs(count)(command, arguments); err != nil {
			return &exitError{code: 2, err: err}
		}
		return nil
	}
}

// ExitCode maps an application error to the public CLI contract.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var explicit *exitError
	if errors.As(err, &explicit) {
		return explicit.code
	}
	var config *environment.ConfigError
	if errors.As(err, &config) {
		return 2
	}
	switch {
	case errors.Is(err, errCanceled), errors.Is(err, context.Canceled):
		return 130
	case errors.Is(err, context.DeadlineExceeded):
		return 3
	case errors.Is(err, cluster.ErrPrerequisite):
		return 3
	case errors.Is(err, cluster.ErrConflict):
		return 4
	default:
		return 1
	}
}
