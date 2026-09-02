// Package cluster owns the kind cluster and its managed local registry.
package cluster

import "github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"

// Fixed names and versions in the v1 cluster contract.
const (
	Name                  = "tripgo-local"
	ControlPlaneContainer = "tripgo-local-control-plane"
	RegistryContainer     = "tripgo-local-registry"
	KindVersion           = "v0.27.0"
	KubernetesVersion     = "v1.32.2"
	KindNodeImage         = "kindest/node:v1.32.2@sha256:f226345927d7e348497136874b6d207e0b32cc52154ad8323129352923a3142f"
	markerPath            = "/etc/tripgoctl/cluster-id"
)

var kindChecksums = map[string]string{
	"darwin/amd64": "3435134325b6b9406ccfec417b13bb46a808fc74e9a2ebb0ca31b379c8293863",
	"darwin/arm64": "5240ca1acb587e1d0386532dd8c3373d81f5173b5af322919fc56f0cdd646596",
	"linux/amd64":  "a6875aaea358acf0ac07786b1a6755d08fd640f4c79b7a2e46681cc13f49a04b",
	"linux/arm64":  "5e4507a41c69679562610b1be82ba4f80693a7826f4e9c6e39236169a3e4f9d0",
}

func registryImageReference() string {
	image, ok := catalog.ImageByName("registry")
	if !ok || image.Reference() == "" {
		panic("cluster: registry image is missing from catalog")
	}
	return image.Reference()
}
