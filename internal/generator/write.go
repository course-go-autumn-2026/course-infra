package generator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Write atomically replaces <target>/rendered with the generated manifests.
// The .tripgo directory is reserved for tripgoctl output, but unknown sibling
// entries are preserved because no local ownership lock is maintained.
func Write(target string, bundle Bundle) error {
	if target == "" {
		return errors.New("generator: output directory is required")
	}
	if err := ensureOutputDirectory(target); err != nil {
		return err
	}

	temporary, err := os.MkdirTemp(target, ".rendered-new-*")
	if err != nil {
		return fmt.Errorf("generator: create temporary rendered directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	for _, file := range bundle.Files {
		if filepath.Base(file.Name) != file.Name || file.Name == "." {
			return fmt.Errorf("generator: invalid rendered filename %q", file.Name)
		}
		if err := os.WriteFile(filepath.Join(temporary, file.Name), file.Content, 0o600); err != nil {
			return fmt.Errorf("generator: write %s: %w", file.Name, err)
		}
	}

	rendered := filepath.Join(target, "rendered")
	backup, err := os.MkdirTemp(target, ".rendered-previous-*")
	if err != nil {
		return fmt.Errorf("generator: reserve rendered backup: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("generator: prepare rendered backup: %w", err)
	}
	defer func() { _ = os.RemoveAll(backup) }()

	renderedExists := false
	if info, inspectErr := os.Lstat(rendered); inspectErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("generator: rendered path %q is not a directory", rendered)
		}
		renderedExists = true
		if err := os.Rename(rendered, backup); err != nil {
			return fmt.Errorf("generator: preserve previous rendered output: %w", err)
		}
	} else if !errors.Is(inspectErr, os.ErrNotExist) {
		return fmt.Errorf("generator: inspect rendered output: %w", inspectErr)
	}

	if err := os.Rename(temporary, rendered); err != nil {
		if renderedExists {
			_ = os.Rename(backup, rendered)
		}
		return fmt.Errorf("generator: install rendered output: %w", err)
	}
	if renderedExists {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("generator: remove previous rendered output: %w", err)
		}
	}
	return nil
}

func ensureOutputDirectory(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(target, 0o700); err != nil {
			return fmt.Errorf("generator: create output directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("generator: inspect output directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("generator: output path %q is not a directory", target)
	}
	return nil
}
