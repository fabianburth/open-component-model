package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	ComputeComponentDigestV1alpha1 = runtime.NewVersionedType(ComputeComponentDigestType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&ComputeComponentDigest{}, ComputeComponentDigestV1alpha1)
}
