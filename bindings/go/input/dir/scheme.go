package dir

import (
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Scheme is the default runtime scheme for directory input specifications.
var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.Dir{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}
