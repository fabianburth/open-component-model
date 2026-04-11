package internal

import (
	"fmt"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processSourceTransformations generates transformation nodes for a single source.
// Returns the Add transformation ID if the source was processed, empty string if skipped.
func processSourceTransformations(
	baseID string,
	_ int,
	source *constructor.Source,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	targetRepoSpec runtime.Typed,
	toRepo *runtime.Unstructured,
	component, version string,
	workingDirectory string,
) (string, error) {
	if !source.HasInput() {
		// Source with access only — stays in the environment descriptor as-is
		return "", nil
	}

	sourceIdentity := source.ToIdentity()
	sourceID := identityToTransformationID(sourceIdentity)
	inputID := fmt.Sprintf("%sInputSrc%s", baseID, sourceID)
	addSourceID := fmt.Sprintf("%sAddSrc%s", baseID, sourceID)

	// Build a v2-style source map for the source descriptor.
	sourceVersion := source.Version
	if sourceVersion == "" {
		sourceVersion = version // default to component version
	}
	sourceMap := map[string]any{
		"name":    source.Name,
		"version": sourceVersion,
		"type":    source.Type,
		"access":  map[string]any{"type": "localBlob/v1"},
	}
	if source.ExtraIdentity != nil {
		sourceMap["extraIdentity"] = source.ExtraIdentity
	}
	if source.Labels != nil {
		labels := make([]map[string]any, len(source.Labels))
		for i, l := range source.Labels {
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
		sourceMap["labels"] = labels
	}

	// Generate input transformation
	inputType, inputSpec, err := buildInputTransformation(source.Input, workingDirectory, sourceMap)
	if err != nil {
		return "", fmt.Errorf("error building input transformation for source: %w", err)
	}

	inputTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: inputType,
			ID:   inputID,
		},
		Spec: inputSpec,
	}
	tgd.Transformations = append(tgd.Transformations, inputTransform)

	addLocalSourceType, err := chooseAddLocalSourceType(targetRepoSpec)
	if err != nil {
		return "", fmt.Errorf("choosing add local source type: %w", err)
	}

	addTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: addLocalSourceType,
			ID:   addSourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"component":  component,
			"version":    version,
			"source":     fmt.Sprintf("${%s.output.source}", inputID),
			"file":       fmt.Sprintf("${%s.output.file}", inputID),
		}},
	}
	tgd.Transformations = append(tgd.Transformations, addTransform)

	return addSourceID, nil
}
