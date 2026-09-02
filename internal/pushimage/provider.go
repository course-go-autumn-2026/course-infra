// Package pushimage builds the embedded Push Service image and publishes it only
// to the registry owned by tripgoctl.
package pushimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/pushartifact"
)

var immutableLocalReference = regexp.MustCompile(`^` + regexp.QuoteMeta(catalog.LocalPushImageRepository) + `@sha256:[0-9a-f]{64}$`)

type commandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	// #nosec G204 -- executable and arguments are fixed application inputs.
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	detail := strings.TrimSpace(string(output))
	if err != nil {
		if detail == "" {
			return "", fmt.Errorf("run %s: %w", name, err)
		}
		return "", fmt.Errorf("run %s: %s: %w", name, detail, err)
	}
	return detail, nil
}

// Provider materializes one platform-matched embedded image in the local registry.
type Provider struct {
	runner commandRunner
	goarch string
}

// NewProvider returns the production local image provider.
func NewProvider() *Provider { return &Provider{runner: execRunner{}, goarch: runtime.GOARCH} }

// Ensure extracts, builds and pushes the image and returns Docker's verified RepoDigest.
func (p *Provider) Ensure(ctx context.Context) (string, error) {
	if p.goarch != "amd64" && p.goarch != "arm64" {
		return "", fmt.Errorf("unsupported Push Service image architecture %q", p.goarch)
	}
	binary := pushartifact.Binary()
	if len(binary) == 0 {
		return "", fmt.Errorf("embedded Push Service binary is absent; use a release tripgoctl build")
	}
	file, err := elf.NewFile(bytes.NewReader(binary))
	if err != nil {
		return "", fmt.Errorf("embedded Push Service binary is not ELF: %w", err)
	}
	expectedMachine := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[p.goarch]
	if file.Machine != expectedMachine {
		return "", fmt.Errorf("embedded Push Service architecture %s does not match CLI architecture %s", file.Machine, p.goarch)
	}
	directory, err := os.MkdirTemp("", "tripgo-push-image-*")
	if err != nil {
		return "", fmt.Errorf("create Push Service build context: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()
	if err := pushartifact.Extract(directory); err != nil {
		return "", err
	}

	contentHash := sha256.New()
	_, _ = contentHash.Write(pushartifact.Dockerfile())
	_, _ = contentHash.Write(binary)
	tag := fmt.Sprintf("%s:embedded-%x", catalog.LocalPushImageRepository, contentHash.Sum(nil)[:12])
	if _, err := p.runner.Run(ctx, "docker", "build", "--platform", "linux/"+p.goarch, "--tag", tag, filepath.Clean(directory)); err != nil {
		return "", fmt.Errorf("build embedded Push Service image: %w", err)
	}
	if _, err := p.runner.Run(ctx, "docker", "push", tag); err != nil {
		return "", fmt.Errorf("push embedded Push Service image to managed registry: %w", err)
	}
	output, err := p.runner.Run(ctx, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", tag)
	if err != nil {
		return "", fmt.Errorf("inspect pushed Push Service digest: %w", err)
	}
	var digests []string
	if err := json.Unmarshal([]byte(output), &digests); err != nil {
		return "", fmt.Errorf("decode pushed Push Service digests: %w", err)
	}
	for _, reference := range digests {
		if immutableLocalReference.MatchString(reference) {
			return reference, nil
		}
	}
	return "", fmt.Errorf("pushed image did not produce an immutable digest in %s", catalog.LocalPushImageRepository)
}
