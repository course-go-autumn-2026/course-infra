//go:build embedded_contracts

package contractasset

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedContractsMatchCanonicalSymlinks(t *testing.T) {
	if err := ValidateLinked(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..")
	for path, embedded := range map[string][]byte{
		OpenAPIDestination: OpenAPI(),
		ProtoDestination:   Proto(),
		"api/source.json":  Manifest(),
	} {
		canonical, err := os.ReadFile(filepath.Join(root, path)) // #nosec G304 -- fixed repository test paths.
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(embedded, canonical) {
			t.Errorf("embedded %s differs from canonical source", path)
		}
	}
}
