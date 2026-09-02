package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// DiagnosticCheck is one secret-free doctor result.
type DiagnosticCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// DiagnosticReport contains only bounded metadata and never raw environment,
// Kubernetes Secret, command output, cluster identity, or credentials.
type DiagnosticReport struct {
	SchemaVersion int               `json:"schema_version"`
	Platform      string            `json:"platform"`
	Checks        []DiagnosticCheck `json:"checks"`
}

// Diagnose checks local prerequisites without mutating Docker or Kubernetes.
func (s *Service) Diagnose(ctx context.Context) DiagnosticReport {
	report := DiagnosticReport{SchemaVersion: 1, Platform: s.goos + "/" + s.goarch}
	add := func(name, status, detail string) {
		report.Checks = append(report.Checks, DiagnosticCheck{Name: name, Status: status, Detail: detail})
	}
	if err := s.checkPlatform(); err != nil {
		add("platform", "error", "unsupported OS or architecture")
	} else {
		add("platform", "ok", "supported")
	}
	if _, err := s.runner.Run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		add("docker-daemon", "error", "unavailable; start Docker and retry")
	} else {
		add("docker-daemon", "ok", "available")
	}
	memory, err := s.runner.Run(ctx, "docker", "info", "--format", "{{.MemTotal}}")
	if err != nil {
		add("docker-memory", "warning", "could not be measured")
	} else if bytes, parseErr := strconv.ParseUint(memory, 10, 64); parseErr != nil {
		add("docker-memory", "warning", "Docker returned an invalid value")
	} else {
		status := "ok"
		if bytes < 6*1024*1024*1024 {
			status = "warning"
		}
		add("docker-memory", status, fmt.Sprintf("%.1f GiB available; 6 GiB recommended", float64(bytes)/(1024*1024*1024)))
	}
	var stats syscall.Statfs_t
	diskPath := s.cacheDir
	if err := os.MkdirAll(diskPath, 0o700); err != nil {
		add("disk", "warning", "cache directory is unavailable")
	} else if err := syscall.Statfs(diskPath, &stats); err != nil {
		add("disk", "warning", "free space could not be measured")
	} else {
		free := uint64(stats.Bavail) * uint64(stats.Bsize)
		status := "ok"
		if free < 10*1024*1024*1024 {
			status = "warning"
		}
		add("disk", status, fmt.Sprintf("%.1f GiB free near cache; 10 GiB recommended", float64(free)/(1024*1024*1024)))
	}
	checksum := kindChecksums[s.goos+"/"+s.goarch]
	if checksum != "" && validFileSHA256(filepath.Join(s.cacheDir, "kind", KindVersion, "kind"), checksum) {
		add("kind-cache", "ok", "verified; offline cluster operations can use the warmed helper cache")
	} else {
		add("kind-cache", "warning", "missing or invalid; network is required before first cluster operation")
	}
	value, stateErr := loadState(s.statePath())
	switch {
	case errors.Is(stateErr, os.ErrNotExist):
		add("local-state", "ok", "absent (no owned cluster recorded)")
	case stateErr != nil:
		add("local-state", "error", "corrupt or incompatible; ownership-sensitive cleanup is intentionally blocked")
	default:
		add("local-state", "ok", fmt.Sprintf("compatible schema; %d known, %d running environments", len(value.Namespaces), len(value.RunningNamespaces)))
	}
	return report
}

// MarshalDiagnosticBundle returns deterministic, secret-free JSON bytes.
func MarshalDiagnosticBundle(report DiagnosticReport) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode diagnostic bundle: %w", err)
	}
	return append(data, '\n'), nil
}
