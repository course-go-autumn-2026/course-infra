//go:build embedded_contracts

package contractasset

import _ "embed"

//go:embed generated/push-service.openapi.yaml
var openapi []byte

//go:embed generated/push.proto
var proto []byte

//go:embed generated/source.json
var manifest []byte

func embeddedOpenAPI() []byte  { return openapi }
func embeddedProto() []byte    { return proto }
func embeddedManifest() []byte { return manifest }
