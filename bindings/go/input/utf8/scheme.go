package utf8

import (
	v1 "ocm.software/open-component-model/bindings/go/input/utf8/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Scheme is the default runtime scheme for UTF-8 input specifications.
var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.UTF8{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}
