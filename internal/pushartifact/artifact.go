// Package pushartifact exposes assets used to construct the local Push Service image.
package pushartifact

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed assets/Dockerfile
var files embed.FS

// Binary returns the embedded linux/<CLI architecture> Push Service binary.
// Development builds without the embedded_push build tag return nil.
func Binary() []byte {
	return append([]byte(nil), embeddedBinary()...)
}

// Dockerfile returns a copy of the embedded canonical runtime Dockerfile.
func Dockerfile() []byte {
	content, err := files.ReadFile("assets/Dockerfile")
	if err != nil {
		panic("pushartifact: embedded Dockerfile is missing: " + err.Error())
	}
	return content
}

// ValidateLinked checks linked release assets without exposing a CLI operation.
func ValidateLinked() error {
	binary := embeddedBinary()
	if len(binary) > 0 && !bytes.HasPrefix(binary, []byte("\x7fELF")) {
		return fmt.Errorf("embedded push service binary is not ELF")
	}
	if !bytes.Contains(Dockerfile(), []byte("FROM scratch")) {
		return fmt.Errorf("embedded Push Service Dockerfile is invalid")
	}
	return nil
}

// Extract writes an image build context without replacing existing asset files.
func Extract(directory string) error {
	binary := Binary()
	if len(binary) == 0 {
		return fmt.Errorf("push service binary is not embedded in this development build")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create Push Service build context: %w", err)
	}
	assets := []struct {
		name    string
		content []byte
		mode    os.FileMode
	}{
		{name: "Dockerfile", content: Dockerfile(), mode: 0o644},
		{name: "push-service", content: binary, mode: 0o755},
	}
	for _, asset := range assets {
		path := filepath.Join(directory, asset.name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, asset.mode) // #nosec G304 -- asset names are fixed above.
		if err != nil {
			return fmt.Errorf("create embedded %s: %w", asset.name, err)
		}
		if _, err = file.Write(asset.content); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return fmt.Errorf("write embedded %s: %w", asset.name, err)
		}
		if err = file.Close(); err != nil {
			_ = os.Remove(path)
			return fmt.Errorf("close embedded %s: %w", asset.name, err)
		}
	}
	return nil
}
