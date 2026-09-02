package pushartifact_test

import (
	"bytes"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/pushartifact"
)

func TestDockerfileIsEmbeddedAndImmutable(t *testing.T) {
	t.Parallel()

	if err := pushartifact.ValidateLinked(); err != nil {
		t.Fatal(err)
	}
	first := pushartifact.Dockerfile()
	if !bytes.Contains(first, []byte("FROM scratch")) || !bytes.Contains(first, []byte("USER 65532:65532")) {
		t.Fatalf("unexpected embedded Dockerfile:\n%s", first)
	}
	first[0] = 'X'
	second := pushartifact.Dockerfile()
	if second[0] == 'X' {
		t.Fatal("Dockerfile() exposed mutable embedded state")
	}
}
