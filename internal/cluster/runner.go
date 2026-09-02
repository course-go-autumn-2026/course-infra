package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type runner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	// #nosec G204 -- executable names and arguments are constructed by the cluster service, not user input.
	command := exec.CommandContext(ctx, name, arguments...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return "", fmt.Errorf("run %s: %s: %w", name, detail, err)
		}
		return "", fmt.Errorf("run %s: %w", name, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
