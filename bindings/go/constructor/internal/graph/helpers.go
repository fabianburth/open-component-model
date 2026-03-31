package graph

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var toWordRunes = []rune{',', '.', '/', '-'}

// identityToTransformationID converts a component identity to a camelCase
// transformation ID suitable for use as a DAG vertex key.
func identityToTransformationID(id runtime.Identity) string {
	words := []string{"construct"}
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		words = append(words, strings.FieldsFunc(id[k], func(r rune) bool {
			return slices.Contains(toWordRunes, r)
		})...)
	}
	result := strings.ToLower(words[0])
	for i := 1; i < len(words); i++ {
		w := strings.ToLower(words[i])
		if len(w) > 0 {
			result += strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return result
}

// asUnstructured converts a runtime.Typed to runtime.Unstructured via Raw.
func asUnstructured(typed runtime.Typed) (*runtime.Unstructured, error) {
	var raw runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(typed, &raw); err != nil {
		return nil, fmt.Errorf("cannot convert to raw: %w", err)
	}
	var unstructured runtime.Unstructured
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&raw, &unstructured); err != nil {
		return nil, fmt.Errorf("cannot convert to unstructured: %w", err)
	}
	return &unstructured, nil
}

// chooseAddType selects the AddComponentVersion type based on target repository type.
func chooseAddType(repo runtime.Typed) (runtime.Type, error) {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddComponentVersionV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddComponentVersionV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add operation", repo)
	}
}

// chooseAddLocalResourceType selects the AddLocalResource type based on target repository type.
func chooseAddLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddLocalResourceV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddLocalResourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add local resource operation", repo)
	}
}

// chooseAddLocalSourceType selects the AddLocalSource type based on target repository type.
func chooseAddLocalSourceType(repo runtime.Typed) (runtime.Type, error) {
	switch repo.(type) {
	case *oci.Repository:
		return ociAddLocalSourceV1alpha1, nil
	case *ctfv1.Repository:
		return ctfAddLocalSourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add local source operation", repo)
	}
}

// AddLocalSource type constants.
// These match the types defined in the OCI transformation spec package.
// We define them here because the published OCI module version may not yet include them.
var (
	ociAddLocalSourceV1alpha1 = runtime.NewVersionedType("OCIAddLocalSource", "v1alpha1")
	ctfAddLocalSourceV1alpha1 = runtime.NewVersionedType("CTFAddLocalSource", "v1alpha1")
)

// chooseGetLocalResourceType selects the GetLocalResource type based on source repository type.
func chooseGetLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIGetLocalResourceV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFGetLocalResourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for get local resource operation", repo)
	}
}
