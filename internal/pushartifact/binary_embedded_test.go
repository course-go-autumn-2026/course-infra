//go:build embedded_push

package pushartifact_test

import (
	"bytes"
	"debug/elf"
	"os"
	"path/filepath"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/pushartifact"
)

func TestEmbeddedPushBinaryArchitecture(t *testing.T) {
	binary := pushartifact.Binary()
	file, err := elf.NewFile(bytes.NewReader(binary))
	if err != nil {
		t.Fatalf("embedded Push Service is not ELF: %v", err)
	}
	expected := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[os.Getenv("EXPECTED_PUSH_ARCH")]
	if expected == elf.EM_NONE || file.Machine != expected {
		t.Fatalf("embedded machine = %s, expected arch %q (%s)", file.Machine, os.Getenv("EXPECTED_PUSH_ARCH"), expected)
	}

	directory := filepath.Join(t.TempDir(), "context")
	if err := pushartifact.Extract(directory); err != nil {
		t.Fatal(err)
	}
	extractedBinary, err := os.ReadFile(filepath.Join(directory, "push-service"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extractedBinary, binary) {
		t.Fatal("extracted Push Service differs from embedded bytes")
	}
	extractedDockerfile, err := os.ReadFile(filepath.Join(directory, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extractedDockerfile, pushartifact.Dockerfile()) {
		t.Fatal("extracted Dockerfile differs from embedded bytes")
	}
	if err := pushartifact.Extract(directory); err == nil {
		t.Fatal("Extract replaced an existing build context")
	}
}
