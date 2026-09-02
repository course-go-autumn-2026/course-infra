package cluster

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestKindConfigContract(t *testing.T) {
	t.Parallel()

	config, digest, err := kindConfig()
	if err != nil {
		t.Fatal(err)
	}
	if digest != fmt.Sprintf("%x", sha256.Sum256(config)) {
		t.Fatal("kind config digest mismatch")
	}
	for _, required := range []string{
		"image: " + KindNodeImage,
		"hostPort: 21032",
		"hostPort: 25909",
		"listenAddress: 127.0.0.1",
		`registry.mirrors."localhost:5001"`,
		`endpoint = ["http://tripgo-local-registry:5000"]`,
	} {
		if !bytes.Contains(config, []byte(required)) {
			t.Errorf("kind config does not contain %q", required)
		}
	}
	if count := strings.Count(string(config), "      - containerPort:"); count != 50 {
		t.Fatalf("extraPortMappings count = %d", count)
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(config, &decoded); err != nil {
		t.Fatalf("kind config is invalid YAML: %v", err)
	}
}
