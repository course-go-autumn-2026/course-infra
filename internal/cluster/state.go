package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const stateSchemaVersion = 1

// stateSchemaVersion changes only for incompatible state layouts. Additive
// fields and tripgoctl patch/minor releases remain readable.
type state struct {
	SchemaVersion     int      `json:"schema_version"`
	ClusterID         string   `json:"cluster_id"`
	KindVersion       string   `json:"kind_version"`
	Kubernetes        string   `json:"kubernetes_version"`
	KindConfigHash    string   `json:"kind_config_sha256"`
	Namespaces        []string `json:"namespaces"`
	RunningNamespaces []string `json:"running_namespaces,omitempty"`
}

func loadState(path string) (state, error) {
	// #nosec G304 -- path is constrained to the application config directory.
	data, err := os.ReadFile(path)
	if err != nil {
		return state{}, err
	}
	var result state
	if err := json.Unmarshal(data, &result); err != nil {
		return state{}, fmt.Errorf("decode cluster state: %w", err)
	}
	if result.SchemaVersion != stateSchemaVersion || result.ClusterID == "" {
		return state{}, errors.New("cluster state is incompatible or incomplete")
	}
	return result, nil
}

func writeState(path string, value state) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cluster state: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return fmt.Errorf("create state temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	defer func() { _ = temporary.Close() }()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install state: %w", err)
	}
	return nil
}

type operationLock struct{ file *os.File }

func acquireLock(path string) (*operationLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	// #nosec G304 -- path is constrained to the application config directory.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open operation lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another tripgoctl cluster operation is running: %w", err)
	}
	return &operationLock{file: file}, nil
}

func (l *operationLock) close() {
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}
