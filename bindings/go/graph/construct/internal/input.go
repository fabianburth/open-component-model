package internal

import (
	"encoding/json"
	"fmt"
	"maps"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// Input transformation type constants.
// These match the types defined in the respective input transformation spec packages.
// We define them here to avoid module-level dependencies on input/file, input/utf8, input/dir, and helm/input.
var (
	fileInputV1alpha1 = runtime.NewVersionedType("FileInput", "v1alpha1")
	utf8InputV1alpha1 = runtime.NewVersionedType("UTF8Input", "v1alpha1")
	dirInputV1alpha1  = runtime.NewVersionedType("DirInput", "v1alpha1")
	helmInputV1alpha1 = runtime.NewVersionedType("HelmInput", "v1alpha1")
)

// processResourceTransformations generates transformation nodes for a single resource.
// Returns the Add transformation ID if the resource was processed, empty string if skipped.
func processResourceTransformations(
	baseID string,
	_ int,
	resource *constructor.Resource,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	targetRepoSpec runtime.Typed,
	toRepo *runtime.Unstructured,
	component, version string,
	workingDirectory string,
	skipDigestProcessing bool,
) (string, error) {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	inputID := fmt.Sprintf("%sInput%s", baseID, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", baseID, resourceID)

	switch {
	case resource.HasInput():
		// Build a v2-style resource map for the resource descriptor.
		// We construct the map directly since descriptor.runtime.Resource uses json:"-" tags.
		resourceVersion := resource.Version
		if resourceVersion == "" {
			resourceVersion = version // default to component version
		}
		resourceMap := map[string]any{
			"name":     resource.Name,
			"version":  resourceVersion,
			"type":     resource.Type,
			"relation": "local",
		}
		if resource.ExtraIdentity != nil {
			resourceMap["extraIdentity"] = resource.ExtraIdentity
		}
		if resource.Labels != nil {
			labels := make([]map[string]any, len(resource.Labels))
			for i, l := range resource.Labels {
				labels[i] = map[string]any{
					"name":  l.Name,
					"value": l.Value,
				}
				if l.Version != "" {
					labels[i]["version"] = l.Version
				}
				if l.Signing {
					labels[i]["signing"] = l.Signing
				}
			}
			resourceMap["labels"] = labels
		}

		// Generate input transformation + AddLocalResource chain
		inputType, inputSpec, err := buildInputTransformation(resource.Input, workingDirectory, resourceMap)
		if err != nil {
			return "", fmt.Errorf("error building input transformation: %w", err)
		}

		inputTransform := transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: inputType,
				ID:   inputID,
			},
			Spec: inputSpec,
		}
		tgd.Transformations = append(tgd.Transformations, inputTransform)

		addLocalResourceType, err := chooseAddLocalResourceType(targetRepoSpec)
		if err != nil {
			return "", fmt.Errorf("choosing add local resource type: %w", err)
		}

		addTransform := transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: addLocalResourceType,
				ID:   addResourceID,
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"repository": toRepo.Data,
				"component":  component,
				"version":    version,
				"resource":   fmt.Sprintf("${%s.output.resource}", inputID),
				"file":       fmt.Sprintf("${%s.output.file}", inputID),
			}},
		}
		tgd.Transformations = append(tgd.Transformations, addTransform)

		return addResourceID, nil

	case resource.HasAccess():
		// Resource has access specification — no input transformation needed.
		// By-value resources will be handled in a future iteration if needed (via Get+Add chain like transfer).
		if resource.CopyPolicy == constructor.CopyPolicyByValue {
			// TODO: Implement by-value resource processing via Get+Add chain
			return "", fmt.Errorf("by-value resource processing via transformation graph is not yet implemented for resource %q", resourceIdentity)
		}

		// For by-reference resources, emit a digest processing transformation
		// if digest processing is enabled and the access type is supported.
		if !skipDigestProcessing && isOCIAccessType(resource.Access.GetType()) {
			digestID := fmt.Sprintf("%sDigest%s", baseID, resourceID)

			// Build a v2-style resource map for the spec
			resourceVersion := resource.Version
			if resourceVersion == "" {
				resourceVersion = version
			}
			resMap := map[string]any{
				"name":     resource.Name,
				"version":  resourceVersion,
				"type":     resource.Type,
				"relation": string(resource.Relation),
			}
			if resource.ExtraIdentity != nil {
				resMap["extraIdentity"] = map[string]string(resource.ExtraIdentity)
			}
			if resource.HasAccess() {
				accessData, err := json.Marshal(resource.Access)
				if err == nil {
					var accessMap map[string]any
					if err := json.Unmarshal(accessData, &accessMap); err == nil {
						resMap["access"] = accessMap
					}
				}
			}

			digestTransform := transformv1alpha1.GenericTransformation{
				TransformationMeta: meta.TransformationMeta{
					Type: ociv1alpha1.ProcessOCIResourceDigestV1alpha1,
					ID:   digestID,
				},
				Spec: &runtime.Unstructured{Data: map[string]any{
					"resource": resMap,
				}},
			}
			tgd.Transformations = append(tgd.Transformations, digestTransform)
			return digestID, nil
		}

		// No digest processing — resource stays in the environment as-is
		return "", nil

	default:
		return "", fmt.Errorf("resource %q has no access type and no input method", resourceIdentity)
	}
}

// buildInputTransformation creates the input transformation spec based on the input type.
// The resourceMap is the constructor-style resource descriptor that the input belongs to.
// The input is embedded into the resource as Resource.Input, and the spec contains
// only the resource (with input populated), workingDirectory, and outputPath.
func buildInputTransformation(input runtime.Typed, workingDirectory string, resourceMap map[string]any) (runtime.Type, *runtime.Unstructured, error) {
	inputType := input.GetType()

	// Marshal the input to get its raw data for embedding in the resource
	rawInputData, err := json.Marshal(input)
	if err != nil {
		return runtime.Type{}, nil, fmt.Errorf("cannot marshal input spec: %w", err)
	}

	// Build the input as a map to embed in the resource
	var inputMap map[string]any
	if err := json.Unmarshal(rawInputData, &inputMap); err != nil {
		return runtime.Type{}, nil, fmt.Errorf("cannot unmarshal input spec: %w", err)
	}

	// Map the constructor input type to the corresponding transformation type.
	// Constructor input types use "type/version" format (e.g., "file/v1"),
	// while transformation types use "Type/version" format (e.g., "FileInput/v1alpha1").
	var transformType runtime.Type
	supportsWorkingDirectory := false
	switch inputType.String() {
	case "file/v1", "file":
		transformType = fileInputV1alpha1
		supportsWorkingDirectory = true
	case "utf8/v1", "utf8":
		transformType = utf8InputV1alpha1
	case "dir/v1", "dir":
		transformType = dirInputV1alpha1
		supportsWorkingDirectory = true
	case "helm/v1", "helm":
		transformType = helmInputV1alpha1
		supportsWorkingDirectory = true
	default:
		return runtime.Type{}, nil, fmt.Errorf("unsupported input type %q", inputType)
	}

	// Build a constructorv1.Resource-shaped map with input populated
	fullResourceMap := make(map[string]any, len(resourceMap)+1)
	maps.Copy(fullResourceMap, resourceMap)
	fullResourceMap["input"] = inputMap

	// Build the transformation spec: resource with input, plus workingDirectory/outputPath
	specMap := map[string]any{
		"resource": fullResourceMap,
	}

	// Inject workingDirectory only for input types that support it
	if supportsWorkingDirectory && workingDirectory != "" {
		specMap["workingDirectory"] = workingDirectory
	}

	return transformType, &runtime.Unstructured{Data: specMap}, nil
}
