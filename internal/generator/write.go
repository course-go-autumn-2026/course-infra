package generator

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Write writes a bundle to target (normally <project>/.tripgo) using a
// same-parent directory swap. Existing non-tripgoctl directories are preserved.
func Write(target string, bundle Bundle) error {
	if target == "" {
		return errors.New("generator: output directory is required")
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("generator: create output parent: %w", err)
	}

	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("generator: output path %q is not an owned directory", target)
		}
		lockPath := filepath.Join(target, "environment.lock")
		lockInfo, inspectErr := os.Lstat(lockPath)
		if inspectErr != nil || lockInfo.Mode()&os.ModeSymlink != 0 || !lockInfo.Mode().IsRegular() {
			return fmt.Errorf("generator: output directory %q is not owned by tripgoctl", target)
		}
		// #nosec G304 -- lockPath is constrained beneath the explicit output directory.
		lock, readErr := os.ReadFile(lockPath)
		if readErr != nil || !matchingLockIdentity(lock, bundle.Lock) {
			return fmt.Errorf("generator: output directory %q is not owned by this environment", target)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("generator: inspect output directory: %w", err)
	}

	temporary, err := os.MkdirTemp(parent, ".tripgo-render-*")
	if err != nil {
		return fmt.Errorf("generator: create temporary output: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	rendered := filepath.Join(temporary, "rendered")
	if err := os.Mkdir(rendered, 0o700); err != nil {
		return fmt.Errorf("generator: create rendered directory: %w", err)
	}
	for _, file := range bundle.Files {
		if filepath.Base(file.Name) != file.Name || file.Name == "." {
			return fmt.Errorf("generator: invalid rendered filename %q", file.Name)
		}
		if err := os.WriteFile(filepath.Join(rendered, file.Name), file.Content, 0o600); err != nil {
			return fmt.Errorf("generator: write %s: %w", file.Name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(temporary, "environment.lock"), bundle.Lock, 0o600); err != nil {
		return fmt.Errorf("generator: write environment.lock: %w", err)
	}

	backup, err := os.MkdirTemp(parent, ".tripgo-previous-*")
	if err != nil {
		return fmt.Errorf("generator: reserve output backup: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("generator: prepare output backup: %w", err)
	}
	defer func() { _ = os.RemoveAll(backup) }()

	targetExists := false
	if _, err := os.Lstat(target); err == nil {
		targetExists = true
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("generator: preserve previous output: %w", err)
		}
	}
	if err := os.Rename(temporary, target); err != nil {
		if targetExists {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("generator: install output: %w", err)
	}
	if targetExists {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("generator: remove previous output: %w", err)
		}
	}
	return nil
}

type writeLockIdentity struct {
	Lab       int    `toml:"lab"`
	Namespace string `toml:"namespace"`
	ClusterID string `toml:"cluster_id"`
}

func matchingLockIdentity(existing, desired []byte) bool {
	if !bytes.HasPrefix(existing, []byte(lockMarker+"\n")) || !bytes.HasPrefix(desired, []byte(lockMarker+"\n")) {
		return false
	}
	var current writeLockIdentity
	var next writeLockIdentity
	if err := toml.Unmarshal(existing, &current); err != nil {
		return false
	}
	if err := toml.Unmarshal(desired, &next); err != nil {
		return false
	}
	return current == next
}
