package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	HelmInputV1alpha1 = runtime.NewVersionedType(HelmInputType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&HelmInput{}, HelmInputV1alpha1)
}
