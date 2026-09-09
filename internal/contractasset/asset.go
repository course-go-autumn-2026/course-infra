// Package contractasset exposes canonical course contracts embedded in release tripgoctl binaries.
package contractasset

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Binary release contract paths recorded in api/source.json.
const (
	OpenAPIDestination = "api/openapi/push-service.openapi.yaml"
	ProtoDestination   = "api/proto/push/v1/push.proto"
)

type sourceManifest struct {
	SourceRepository string `json:"source_repository"`
	SourceCommit     string `json:"source_commit"`
	Files            []struct {
		Destination string `json:"destination"`
		SHA256      string `json:"sha256"`
	} `json:"files"`
}

// ValidateLinked verifies that a tagged release contains both contracts matching its manifest.
func ValidateLinked() error {
	openapi, proto, manifest := embeddedOpenAPI(), embeddedProto(), embeddedManifest()
	if len(openapi) == 0 && len(proto) == 0 && len(manifest) == 0 {
		return nil
	}
	if len(openapi) == 0 || len(proto) == 0 || len(manifest) == 0 {
		return fmt.Errorf("embedded contract assets are incomplete")
	}
	var source sourceManifest
	if err := json.Unmarshal(manifest, &source); err != nil {
		return fmt.Errorf("decode embedded contract manifest: %w", err)
	}
	if source.SourceRepository == "" || source.SourceCommit == "" {
		return fmt.Errorf("embedded contract manifest is missing source identity")
	}
	expected := map[string][]byte{OpenAPIDestination: openapi, ProtoDestination: proto}
	seen := make(map[string]bool, len(expected))
	for _, file := range source.Files {
		content, ok := expected[file.Destination]
		if !ok || seen[file.Destination] {
			return fmt.Errorf("embedded contract manifest has unexpected destination %q", file.Destination)
		}
		seen[file.Destination] = true
		if actual := fmt.Sprintf("%x", sha256.Sum256(content)); actual != file.SHA256 {
			return fmt.Errorf("embedded contract %s SHA-256 does not match manifest", file.Destination)
		}
	}
	for destination := range expected {
		if !seen[destination] {
			return fmt.Errorf("embedded contract manifest is missing %s", destination)
		}
	}
	return nil
}
