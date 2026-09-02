//go:build !embedded_contracts

package contractasset

func embeddedOpenAPI() []byte  { return nil }
func embeddedProto() []byte    { return nil }
func embeddedManifest() []byte { return nil }
