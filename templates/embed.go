// Package templates exposes Kubernetes templates embedded in tripgoctl.
package templates

import "embed"

// Files contains deterministic manifest templates.
//
//go:embed base/*.yaml.tmpl components/*.yaml.tmpl
var Files embed.FS
