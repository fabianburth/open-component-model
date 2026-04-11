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
		return ociv1alpha1.OCIAddLocalSourceV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddLocalSourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add local source operation", repo)
	}
}

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

// ociAccessTypeNames lists the access type names that are handled by the OCI
// resource digest processor. These cover the canonical type ("OCIImage") as well
// as all legacy aliases registered in the OCI access scheme.
var ociAccessTypeNames = map[string]struct{}{
	"OCIImage":    {},
	"ociArtifact": {},
	"ociRegistry": {},
	"ociImage":    {},
}

// isOCIAccessType returns true if the given access type is an OCI image access
// type that can be processed by the OCI resource digest processor.
func isOCIAccessType(t runtime.Type) bool {
	_, ok := ociAccessTypeNames[t.Name]
	return ok
}
