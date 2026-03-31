package graph

import (
	"encoding/json"
	"fmt"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
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
) (string, error) {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	inputID := fmt.Sprintf("%sInput%s", baseID, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", baseID, resourceID)

	switch {
	case resource.HasInput():
		// Generate input transformation + AddLocalResource chain
		inputType, inputSpec, err := buildInputTransformation(resource.Input, workingDirectory)
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

		// Build a v2-style resource map for the AddLocalResource spec.
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
			"access":   map[string]any{"type": "localBlob/v1"},
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
				"resource":   resourceMap,
				"file":       fmt.Sprintf("${%s.output.file}", inputID),
			}},
		}
		tgd.Transformations = append(tgd.Transformations, addTransform)

		return addResourceID, nil

	case resource.HasAccess():
		// Resource has access specification — no input transformation needed.
		// By-reference resources don't need any transformation — they're in the environment descriptor.
		// By-value resources will be handled in a future iteration if needed (via Get+Add chain like transfer).
		if resource.CopyPolicy == constructor.CopyPolicyByValue {
			// TODO: Implement by-value resource processing via Get+Add chain
			return "", fmt.Errorf("by-value resource processing via transformation graph is not yet implemented for resource %q", resourceIdentity)
		}
		// By-reference: nothing to do, the resource stays in the environment descriptor as-is
		return "", nil

	default:
		return "", fmt.Errorf("resource %q has no access type and no input method", resourceIdentity)
	}
}

// buildInputTransformation creates the input transformation spec based on the input type.
func buildInputTransformation(input runtime.Typed, workingDirectory string) (runtime.Type, *runtime.Unstructured, error) {
	inputType := input.GetType()

	// Marshal the input to get its raw data, then build the transformation spec
	rawData, err := json.Marshal(input)
	if err != nil {
		return runtime.Type{}, nil, fmt.Errorf("cannot marshal input spec: %w", err)
	}
	var inputMap map[string]any
	if err := json.Unmarshal(rawData, &inputMap); err != nil {
		return runtime.Type{}, nil, fmt.Errorf("cannot unmarshal input spec: %w", err)
	}

	// Remove the "type" field — it belongs to the constructor input envelope,
	// not the transformation spec (which has additionalProperties: false).
	delete(inputMap, "type")

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
	default:
		return runtime.Type{}, nil, fmt.Errorf("unsupported input type %q", inputType)
	}

	// Inject workingDirectory only for input types that support it (file, dir)
	if supportsWorkingDirectory && workingDirectory != "" {
		if wd, ok := inputMap["workingDirectory"]; !ok || wd == "" || wd == nil {
			inputMap["workingDirectory"] = workingDirectory
		}
	}

	return transformType, &runtime.Unstructured{Data: inputMap}, nil
}
