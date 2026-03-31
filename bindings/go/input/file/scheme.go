package file

import (
	v1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Scheme is the default runtime scheme for file input specifications.
var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.File{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}
