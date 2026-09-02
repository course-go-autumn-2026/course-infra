//go:build embedded_push

package pushartifact

import _ "embed"

//go:embed generated/push-service
var pushServiceBinary []byte

func embeddedBinary() []byte { return pushServiceBinary }
