package internal

import "ocm.software/open-component-model/bindings/go/runtime"

// ociAccessTypeNames lists the access type names that are handled by the OCI
// resource digest processor.
var ociAccessTypeNames = map[string]struct{}{
	"OCIImage":    {},
	"ociArtifact": {},
	"ociRegistry": {},
	"ociImage":    {},
}

func isOCIAccessType(t runtime.Type) bool {
	_, ok := ociAccessTypeNames[t.Name]
	return ok
}
