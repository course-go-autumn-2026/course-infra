// Command tripgoctl manages local TripGo lab environments.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/buildinfo"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cli"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/contractasset"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/pushartifact"
)

func main() {
	if err := pushartifact.ValidateLinked(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := contractasset.ValidateLinked(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	root, err := cli.NewRootCommand(cli.Dependencies{
		Stdin:            os.Stdin,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		WorkingDirectory: os.Getwd,
		IsTerminal: func() bool {
			info, statErr := os.Stdin.Stat()
			return statErr == nil && info.Mode()&os.ModeCharDevice != 0
		},
		IsStderrTerminal: func() bool {
			return term.IsTerminal(int(os.Stderr.Fd()))
		},
		ProgressCacheDirectory: os.UserCacheDir,
		Build:                  buildinfo.Current("tripgoctl"),
	})
	if err == nil {
		signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		// Every invocation is bounded. Long-running log following remains useful,
		// while Ctrl-C/SIGTERM cancels Docker, HTTP, and Kubernetes calls promptly.
		operationContext, cancel := context.WithTimeout(signalContext, 30*time.Minute)
		err = root.ExecuteContext(operationContext)
		cancel()
		stop()
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.ExitCode(err))
	}
}
