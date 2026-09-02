package cluster

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/boundedio"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/progress"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/redact"
)

type runner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execRunner struct{}

const commandCaptureBytes = 1024 * 1024

func (execRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	// #nosec G204 -- executable names and arguments are constructed by the cluster service, not user input.
	command := exec.CommandContext(ctx, name, arguments...)
	stdout := boundedio.NewBuffer(commandCaptureBytes)
	stderr := boundedio.NewBuffer(commandCaptureBytes)
	stdoutProgress := progress.NewLineWriter(ctx)
	stderrProgress := progress.NewLineWriter(ctx)
	streamed := safeProgressCommand(name, arguments)
	if streamed {
		command.Stdout = io.MultiWriter(stdout, stdoutProgress)
		command.Stderr = io.MultiWriter(stderr, stderrProgress)
	} else {
		command.Stdout = stdout
		command.Stderr = stderr
	}
	err := command.Run()
	_ = stdoutProgress.Close()
	_ = stderrProgress.Close()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return "", fmt.Errorf("run %s: %s: %w", name, redact.TerminalText(detail, 16*1024), err)
		}
		return "", fmt.Errorf("run %s: %w", name, err)
	}
	if !streamed && stdout.Truncated() {
		return "", fmt.Errorf("run %s: command output exceeded %d bytes", name, commandCaptureBytes)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Only commands with fixed, non-secret output are allowed to expose subprocess
// activity. In particular, `kind get kubeconfig` stdout contains credentials.
func safeProgressCommand(name string, arguments []string) bool {
	base := filepath.Base(name)
	if base == "kind" && len(arguments) >= 2 {
		return arguments[0] == "create" && arguments[1] == "cluster"
	}
	if base != "docker" || len(arguments) == 0 || arguments[0] != "run" {
		return false
	}
	for index := 1; index+1 < len(arguments); index++ {
		if arguments[index] == "--name" && arguments[index+1] == RegistryContainer {
			return true
		}
	}
	return false
}
