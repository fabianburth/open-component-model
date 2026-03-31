package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	FileInputV1alpha1 = runtime.NewVersionedType(FileInputType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&FileInput{}, FileInputV1alpha1)
}
