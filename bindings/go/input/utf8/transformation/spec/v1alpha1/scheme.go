package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	UTF8InputV1alpha1 = runtime.NewVersionedType(UTF8InputType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&UTF8Input{}, UTF8InputV1alpha1)
}
