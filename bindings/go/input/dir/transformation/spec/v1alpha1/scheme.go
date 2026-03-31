package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	DirInputV1alpha1 = runtime.NewVersionedType(DirInputType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&DirInput{}, DirInputV1alpha1)
}
