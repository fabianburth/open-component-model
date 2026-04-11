package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1alpha1 "ocm.software/open-component-model/bindings/go/graph/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// BuildGraphDefinition constructs a TransformationGraphDefinition from a ComponentConstructor.
//
// The process has two phases:
//  1. Discovery: A concurrent DAG discoverer resolves each constructor component and follows
//     component references to build a complete dependency graph (including external components).
//  2. Graph construction: For each discovered component, transformation nodes are generated:
//     - Input transformations for resources/sources with input specs
//     - AddLocalResource/AddLocalSource transformations
//     - AddComponentVersion upload transformation
//     - ComputeComponentDigest for referenced components
func BuildGraphDefinition(
	ctx context.Context,
	componentConstructor *constructor.ComponentConstructor,
	targetRepoSpec runtime.Typed,
	workingDirectory string,
	externalComponentRepoProvider ExternalComponentRepositoryProvider,
	externalCopyPolicy ExternalComponentVersionCopyPolicy,
	conflictPolicy ComponentVersionConflictPolicy,
	skipDigestProcessing bool,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	if len(componentConstructor.Components) == 0 {
		return &transformv1alpha1.TransformationGraphDefinition{
			Environment:     &runtime.Unstructured{Data: map[string]any{}},
			Transformations: nil,
		}, nil
	}

	// Phase 1: Discover all components (constructor + external)
	roots := make([]string, len(componentConstructor.Components))
	for i, comp := range componentConstructor.Components {
		roots[i] = comp.ToIdentity().String()
	}

	resAndDis := &resolverAndDiscoverer{
		componentConstructor:      componentConstructor,
		externalRepoProvider:      externalComponentRepoProvider,
		resolveExternalLocalBlobs: externalCopyPolicy == ExternalComponentVersionCopyPolicyCopyOrFail,
	}

	discoverer := syncdag.NewGraphDiscoverer(&syncdag.GraphDiscovererOptions[string, *ConstructorOrExternalComponent]{
		Roots:      roots,
		Resolver:   resAndDis,
		Discoverer: resAndDis,
	})

	slog.DebugContext(ctx, "starting constructor component discovery", "roots", roots)

	if err := discoverer.Discover(ctx); err != nil {
		return nil, fmt.Errorf("component discovery failed: %w", err)
	}

	slog.DebugContext(ctx, "constructor component discovery completed")

	// Phase 2: Build transformation graph from discovered DAG
	tgd := &transformv1alpha1.TransformationGraphDefinition{
		Environment: &runtime.Unstructured{
			Data: map[string]any{},
		},
	}

	g := discoverer.Graph()
	err := g.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		return fillGraphDefinition(ctx, d, tgd, targetRepoSpec, workingDirectory, externalCopyPolicy, conflictPolicy, skipDigestProcessing)
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

// fillGraphDefinition walks the discovered DAG and generates transformation nodes
// for each component.
func fillGraphDefinition(
	ctx context.Context,
	d *dag.DirectedAcyclicGraph[string],
	tgd *transformv1alpha1.TransformationGraphDefinition,
	targetRepoSpec runtime.Typed,
	workingDirectory string,
	externalCopyPolicy ExternalComponentVersionCopyPolicy,
	_ ComponentVersionConflictPolicy,
	skipDigestProcessing bool,
) error {
	slog.DebugContext(ctx, "building transformations for discovered components",
		"components", len(d.Vertices))

	// Collect expected digests and referenced component IDs from constructor references.
	// Key: referenced component's digest transformation ID, Value: expected digest.
	expectedDigests := make(map[string]*constructor.Digest)
	// referencedComponents tracks which components are referenced by others and thus
	// need a ComputeComponentDigest transformation. Components not in this set don't
	// need their digest computed since no other component consumes it.
	referencedComponents := make(map[string]struct{})
	if !skipDigestProcessing {
		for _, v := range d.Vertices {
			val := v.Attributes[syncdag.AttributeValue].(*ConstructorOrExternalComponent)
			if val.ConstructorComponent == nil {
				continue
			}
			for _, ref := range val.ConstructorComponent.References {
				refCompID := identityToTransformationID(ref.ToComponentIdentity())
				referencedComponents[refCompID] = struct{}{}
				if ref.Digest == nil {
					continue
				}
				expectedDigests[refCompID+"Digest"] = ref.Digest
			}
		}
	}

	for key, v := range d.Vertices {
		val := v.Attributes[syncdag.AttributeValue].(*ConstructorOrExternalComponent)

		switch {
		case val.ConstructorComponent != nil:
			if err := processConstructorComponent(ctx, val.ConstructorComponent, key, tgd, targetRepoSpec, workingDirectory, skipDigestProcessing, expectedDigests, referencedComponents); err != nil {
				return fmt.Errorf("error processing constructor component %s: %w", key, err)
			}
		case val.ExternalComponent != nil:
			if err := processExternalComponent(ctx, val.ExternalComponent, key, tgd, targetRepoSpec, externalCopyPolicy, skipDigestProcessing, expectedDigests, referencedComponents); err != nil {
				return fmt.Errorf("error processing external component %s: %w", key, err)
			}
		default:
			return fmt.Errorf("component %s has neither constructor nor external component", key)
		}
	}

	return nil
}

// processConstructorComponent generates transformation nodes for a constructor-defined component.
// This includes input transformations, AddLocalResource/AddLocalSource, and AddComponentVersion.
func processConstructorComponent(
	ctx context.Context,
	component *constructor.Component,
	_ string,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	targetRepoSpec runtime.Typed,
	workingDirectory string,
	skipDigestProcessing bool,
	expectedDigests map[string]*constructor.Digest,
	referencedComponents map[string]struct{},
) error {
	baseID := identityToTransformationID(component.ToIdentity())

	slog.DebugContext(ctx, "processing constructor component",
		"component", component.Name, "version", component.Version,
		"resources", len(component.Resources), "sources", len(component.Sources),
		"references", len(component.References), "id", baseID)

	// Default empty resource/source versions and relations on a copy
	// so the original component is not mutated.
	compCopy := *component
	compCopy.Resources = make([]constructor.Resource, len(component.Resources))
	copy(compCopy.Resources, component.Resources)
	for i := range compCopy.Resources {
		if compCopy.Resources[i].Version == "" {
			compCopy.Resources[i].Version = component.Version
		}
		if compCopy.Resources[i].Relation == "" {
			if compCopy.Resources[i].HasInput() {
				compCopy.Resources[i].Relation = constructor.LocalRelation
			} else {
				compCopy.Resources[i].Relation = constructor.ExternalRelation
			}
		}
	}
	compCopy.Sources = make([]constructor.Source, len(component.Sources))
	copy(compCopy.Sources, component.Sources)
	for i := range compCopy.Sources {
		if compCopy.Sources[i].Version == "" {
			compCopy.Sources[i].Version = component.Version
		}
	}

	// Convert constructor component to v2 descriptor for use in transformation specs.
	descComp := constructor.ConvertToDescriptorComponent(&compCopy)
	v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), &descruntime.Descriptor{
		Meta:      descruntime.Meta{Version: "v2"},
		Component: *descComp,
	})
	if err != nil {
		return fmt.Errorf("cannot convert constructor component to v2 descriptor: %w", err)
	}

	descMap, err := descriptorToMap(v2desc)
	if err != nil {
		return err
	}

	toRepo, err := asUnstructured(targetRepoSpec)
	if err != nil {
		return fmt.Errorf("cannot convert target repo spec to unstructured: %w", err)
	}

	// Process resources
	resourceTransformIDs := make(map[int]string)
	for i, resource := range compCopy.Resources {
		addID, err := processResourceTransformations(
			baseID, i, &resource, tgd, targetRepoSpec, toRepo, compCopy.Name, compCopy.Version, workingDirectory, skipDigestProcessing,
		)
		if err != nil {
			return fmt.Errorf("error processing resource %q: %w", resource.ToIdentity(), err)
		}
		if addID != "" {
			resourceTransformIDs[i] = addID
		}
	}

	// Process sources
	sourceTransformIDs := make(map[int]string)
	for i, source := range compCopy.Sources {
		addID, err := processSourceTransformations(
			baseID, i, &source, tgd, targetRepoSpec, toRepo, compCopy.Name, compCopy.Version, workingDirectory,
		)
		if err != nil {
			return fmt.Errorf("error processing source %q: %w", source.ToIdentity(), err)
		}
		if addID != "" {
			sourceTransformIDs[i] = addID
		}
	}

	// Process references — compute digest for each referenced component
	referenceDigestIDs := make(map[int]string)
	if !skipDigestProcessing {
		for i, ref := range component.References {
			refCompID := identityToTransformationID(ref.ToComponentIdentity())
			digestID := refCompID + "Digest"
			referenceDigestIDs[i] = digestID
		}
	}

	// Build the upload transformation using the inline descriptor map.
	if err := addConstructorUploadTransformation(descMap, baseID, targetRepoSpec, toRepo, tgd, resourceTransformIDs, sourceTransformIDs, referenceDigestIDs); err != nil {
		return err
	}

	// Add ComputeComponentDigest only if another component references this one
	if !skipDigestProcessing {
		if _, isReferenced := referencedComponents[baseID]; isReferenced {
			addComputeDigestTransformation(baseID, true, descMap, expectedDigests[baseID+"Digest"], tgd)
		}
	}

	return nil
}

// processExternalComponent generates transformation nodes for an externally referenced component.
func processExternalComponent(
	ctx context.Context,
	extComp *DescriptorWithLocalBlobs,
	_ string,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	targetRepoSpec runtime.Typed,
	copyPolicy ExternalComponentVersionCopyPolicy,
	skipDigestProcessing bool,
	expectedDigests map[string]*constructor.Digest,
	referencedComponents map[string]struct{},
) error {
	baseID := identityToTransformationID(extComp.Descriptor.Component.ToIdentity())

	v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), extComp.Descriptor)
	if err != nil {
		return fmt.Errorf("cannot convert external descriptor to v2: %w", err)
	}

	descMap, err := descriptorToMap(v2desc)
	if err != nil {
		return err
	}

	if copyPolicy == ExternalComponentVersionCopyPolicyCopyOrFail {
		slog.DebugContext(ctx, "generating copy transformations for external component",
			"component", extComp.Descriptor.Component.Name,
			"version", extComp.Descriptor.Component.Version,
			"localResources", len(extComp.Local))

		toRepo, err := asUnstructured(targetRepoSpec)
		if err != nil {
			return fmt.Errorf("cannot convert target repo to unstructured: %w", err)
		}

		component := extComp.Descriptor.Component.Name
		version := extComp.Descriptor.Component.Version

		// Process local blob resources that need to be copied
		resourceTransformIDs := make(map[int]string)
		for _, local := range extComp.Local {
			resourceIdentity := local.Resource.ToIdentity()
			resourceID := identityToTransformationID(resourceIdentity)
			addResourceID := fmt.Sprintf("%sAdd%s", baseID, resourceID)

			addLocalResourceType, err := chooseAddLocalResourceType(targetRepoSpec)
			if err != nil {
				return fmt.Errorf("choosing add local resource type: %w", err)
			}

			// Write the blob content (fetched during discovery) to a temp file.
			blobFile, err := os.CreateTemp("", "ocm-external-blob-*")
			if err != nil {
				return fmt.Errorf("cannot create temp file for external blob: %w", err)
			}
			if err := blob.Copy(blobFile, local.Content); err != nil {
				_ = blobFile.Close()
				return fmt.Errorf("cannot write external blob to temp file: %w", err)
			}
			if err := blobFile.Close(); err != nil {
				return fmt.Errorf("cannot close temp file for external blob: %w", err)
			}

			resourceMap, err := resourceToMap(local.Resource)
			if err != nil {
				return fmt.Errorf("cannot convert resource to map: %w", err)
			}

			addResourceTransform := transformv1alpha1.GenericTransformation{
				TransformationMeta: meta.TransformationMeta{
					Type: addLocalResourceType,
					ID:   addResourceID,
				},
				Spec: &runtime.Unstructured{Data: map[string]any{
					"repository": toRepo.Data,
					"component":  component,
					"version":    version,
					"resource":   resourceMap,
					"file":       map[string]any{"type": "File/v1alpha1", "uri": "file://" + blobFile.Name()},
				}},
			}
			tgd.Transformations = append(tgd.Transformations, addResourceTransform)
			resourceTransformIDs[local.Index] = addResourceID
		}

		// Upload transformation for external component
		if err := addUploadTransformation(descMap, baseID, targetRepoSpec, toRepo, tgd, resourceTransformIDs, nil, nil); err != nil {
			return err
		}
	}

	// Add ComputeComponentDigest only if another component references this one
	if !skipDigestProcessing {
		if _, isReferenced := referencedComponents[baseID]; isReferenced {
			hasUpload := copyPolicy == ExternalComponentVersionCopyPolicyCopyOrFail
			addComputeDigestTransformation(baseID, hasUpload, descMap, expectedDigests[baseID+"Digest"], tgd)
		}
	}

	return nil
}

// addComputeDigestTransformation adds a ComputeComponentDigest transformation for a component.
// If hasUpload is true, the descriptor is taken from the Upload transformation's spec.
// Otherwise (external component without copy), the descriptor map is inlined directly.
// If expectedDigest is non-nil, it is included in the spec for verification.
func addComputeDigestTransformation(baseID string, hasUpload bool, descMap map[string]any, expectedDigest *constructor.Digest, tgd *transformv1alpha1.TransformationGraphDefinition) {
	digestID := baseID + "Digest"

	var descriptorValue any
	if hasUpload {
		descriptorValue = fmt.Sprintf("${%sUpload.spec.descriptor}", baseID)
	} else {
		descriptorValue = descMap
	}

	specData := map[string]any{
		"descriptor": descriptorValue,
	}
	if expectedDigest != nil {
		specData["expectedDigest"] = map[string]any{
			"hashAlgorithm":          expectedDigest.HashAlgorithm,
			"normalisationAlgorithm": expectedDigest.NormalisationAlgorithm,
			"value":                  expectedDigest.Value,
		}
	}

	digestTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: constructorv1alpha1.ComputeComponentDigestV1alpha1,
			ID:   digestID,
		},
		Spec: &runtime.Unstructured{Data: specData},
	}
	tgd.Transformations = append(tgd.Transformations, digestTransform)
}

// addUploadTransformation creates the final AddComponentVersion upload transformation
// for external components, inlining the descriptor with CEL refs for modified resources.
func addUploadTransformation(
	descMap map[string]any,
	baseID string,
	targetRepoSpec runtime.Typed,
	toRepo *runtime.Unstructured,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	resourceTransformIDs map[int]string,
	sourceTransformIDs map[int]string,
	referenceDigestIDs map[int]string,
) error {
	descriptorSpec := buildInlineDescriptorSpec(descMap, resourceTransformIDs, sourceTransformIDs, referenceDigestIDs)

	addType, err := chooseAddType(targetRepoSpec)
	if err != nil {
		return fmt.Errorf("choosing add type for target repository: %w", err)
	}

	upload := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: addType,
			ID:   baseID + "Upload",
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"descriptor": descriptorSpec,
		}},
	}

	tgd.Transformations = append(tgd.Transformations, upload)
	return nil
}

// addConstructorUploadTransformation creates the AddComponentVersion upload transformation
// for constructor components, using the inline v2 descriptor map with CEL refs for
// resources/sources handled by transformations.
func addConstructorUploadTransformation(
	descMap map[string]any,
	baseID string,
	targetRepoSpec runtime.Typed,
	toRepo *runtime.Unstructured,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	resourceTransformIDs map[int]string,
	sourceTransformIDs map[int]string,
	referenceDigestIDs map[int]string,
) error {
	descriptorSpec := buildInlineDescriptorSpec(descMap, resourceTransformIDs, sourceTransformIDs, referenceDigestIDs)

	addType, err := chooseAddType(targetRepoSpec)
	if err != nil {
		return fmt.Errorf("choosing add type for target repository: %w", err)
	}

	upload := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: addType,
			ID:   baseID + "Upload",
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"descriptor": descriptorSpec,
		}},
	}

	tgd.Transformations = append(tgd.Transformations, upload)
	return nil
}

// descriptorToMap marshals a v2 descriptor to map[string]any.
func descriptorToMap(v2desc *descriptorv2.Descriptor) (map[string]any, error) {
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

// buildInlineDescriptorSpec returns a deep copy of the descriptor map with modified
// resources, sources, and references replaced by CEL references to transformation outputs.
func buildInlineDescriptorSpec(descMap map[string]any, resourceTransformIDs map[int]string, sourceTransformIDs map[int]string, referenceDigestIDs map[int]string) map[string]any {
	if len(resourceTransformIDs) == 0 && len(sourceTransformIDs) == 0 && len(referenceDigestIDs) == 0 {
		return descMap
	}

	// Deep-copy the map so we don't mutate the caller's data.
	raw, _ := json.Marshal(descMap)
	result := make(map[string]any)
	_ = json.Unmarshal(raw, &result)

	component, _ := result["component"].(map[string]any)

	if resources, ok := component["resources"].([]any); ok {
		for i, addID := range resourceTransformIDs {
			resources[i] = fmt.Sprintf("${%s.output.resource}", addID)
		}
	}

	if sources, ok := component["sources"].([]any); ok {
		for i, addID := range sourceTransformIDs {
			sources[i] = fmt.Sprintf("${%s.output.source}", addID)
		}
	}

	if refs, ok := component["componentReferences"].([]any); ok {
		for i, digestID := range referenceDigestIDs {
			if refMap, ok := refs[i].(map[string]any); ok {
				refMap["digest"] = fmt.Sprintf("${%s.output.digest}", digestID)
			}
		}
	}

	return result
}

// resourceToMap converts a descriptor Resource to map[string]any for embedding in unstructured specs.
// Since descriptor.runtime.Resource uses json:"-" tags, we convert through v2 first.
func resourceToMap(resource *descruntime.Resource) (map[string]any, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	v2Res, err := descruntime.ConvertToV2Resource(scheme, resource)
	if err != nil {
		return nil, fmt.Errorf("cannot convert resource to v2: %w", err)
	}
	data, err := json.Marshal(v2Res)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal resource: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("cannot unmarshal resource: %w", err)
	}
	return m, nil
}
