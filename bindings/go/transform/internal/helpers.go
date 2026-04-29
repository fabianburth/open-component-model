// Package internal provides shared utilities for graph generation used by
// both the construct and transfer subpackages.
package internal

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/transformation/spec/v1alpha1"
	ctfrepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var toWordRunes = []rune{',', '.', '/', '-'}

// IdentityToTransformationID converts a component identity to a camelCase
// transformation ID with the given prefix.
func IdentityToTransformationID(prefix string, id runtime.Identity) string {
	words := []string{prefix}
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

// AsUnstructured converts a runtime.Typed to runtime.Unstructured via Raw.
func AsUnstructured(typed runtime.Typed) (*runtime.Unstructured, error) {
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

var repoScheme = func() *runtime.Scheme {
	s := runtime.NewScheme(runtime.WithAllowUnknown())
	s.MustRegisterScheme(repository.Scheme)
	return s
}()

// ConvertToConcreteRepo converts a runtime.Typed (which may be *runtime.Raw) to a concrete repository type.
func ConvertToConcreteRepo(repo runtime.Typed) (runtime.Typed, error) {
	switch repo.(type) {
	case *oci.Repository, *ctfrepo.Repository:
		return repo, nil
	case *runtime.Raw:
		obj, err := repoScheme.NewObject(repo.GetType())
		if err != nil {
			return nil, fmt.Errorf("cannot create object for type %s: %w", repo.GetType(), err)
		}
		if err := repoScheme.Convert(repo, obj); err != nil {
			return nil, fmt.Errorf("cannot convert raw to concrete type: %w", err)
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("unknown repository type %T", repo)
	}
}

// ChooseAddType selects the AddComponentVersion type based on target repository type.
func ChooseAddType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := ConvertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddComponentVersionV1alpha1, nil
	case *ctfrepo.Repository:
		return ociv1alpha1.CTFAddComponentVersionV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add operation", concreteRepo)
	}
}

// ChooseAddLocalResourceType selects the AddLocalResource type based on target repository type.
func ChooseAddLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := ConvertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddLocalResourceV1alpha1, nil
	case *ctfrepo.Repository:
		return ociv1alpha1.CTFAddLocalResourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add local resource operation", concreteRepo)
	}
}

// ChooseAddLocalSourceType selects the AddLocalSource type based on target repository type.
func ChooseAddLocalSourceType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := ConvertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddLocalSourceV1alpha1, nil
	case *ctfrepo.Repository:
		return ociv1alpha1.CTFAddLocalSourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add local source operation", concreteRepo)
	}
}

// ChooseGetLocalResourceType selects the GetLocalResource type based on source repository type.
func ChooseGetLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := ConvertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIGetLocalResourceV1alpha1, nil
	case *ctfrepo.Repository:
		return ociv1alpha1.CTFGetLocalResourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for get local resource operation", concreteRepo)
	}
}

// DescriptorToMap marshals a v2 descriptor to map[string]any.
func DescriptorToMap(v2desc *descriptorv2.Descriptor) (map[string]any, error) {
	rawV2Desc, err := json.Marshal(v2desc)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal v2 descriptor: %w", err)
	}
	mapDesc := make(map[string]any)
	if err := json.Unmarshal(rawV2Desc, &mapDesc); err != nil {
		return nil, fmt.Errorf("cannot unmarshal v2 descriptor: %w", err)
	}
	return mapDesc, nil
}

// BuildInlineDescriptorSpec deep-copies a descriptor map and replaces
// modified resources, sources, and references with CEL transformation output refs.
func BuildInlineDescriptorSpec(descMap map[string]any, resourceTransformIDs map[int]string, sourceTransformIDs map[int]string, referenceDigestIDs map[int]string) map[string]any {
	if len(resourceTransformIDs) == 0 && len(sourceTransformIDs) == 0 && len(referenceDigestIDs) == 0 {
		return descMap
	}

	result := maps.Clone(descMap)
	component, ok := result["component"].(map[string]any)
	if !ok {
		return result
	}
	result["component"] = maps.Clone(component)
	component = result["component"].(map[string]any)

	if resources, ok := component["resources"].([]any); ok && len(resourceTransformIDs) > 0 {
		newResources := make([]any, len(resources))
		copy(newResources, resources)
		for i, addID := range resourceTransformIDs {
			if i < len(newResources) {
				newResources[i] = fmt.Sprintf("${%s.output.resource}", addID)
			}
		}
		component["resources"] = newResources
	}

	if sources, ok := component["sources"].([]any); ok && len(sourceTransformIDs) > 0 {
		newSources := make([]any, len(sources))
		copy(newSources, sources)
		for i, addID := range sourceTransformIDs {
			if i < len(newSources) {
				newSources[i] = fmt.Sprintf("${%s.output.source}", addID)
			}
		}
		component["sources"] = newSources
	}

	if refs, ok := component["componentReferences"].([]any); ok && len(referenceDigestIDs) > 0 {
		newRefs := make([]any, len(refs))
		copy(newRefs, refs)
		for i, digestID := range referenceDigestIDs {
			if i < len(newRefs) {
				if refMap, ok := newRefs[i].(map[string]any); ok {
					newRef := maps.Clone(refMap)
					newRef["digest"] = fmt.Sprintf("${%s.output.digest}", digestID)
					newRefs[i] = newRef
				}
			}
		}
		component["componentReferences"] = newRefs
	}

	return result
}
