package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1alpha1 "ocm.software/open-component-model/bindings/go/constructor/spec/transformation/v1alpha1"
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

	// Store the constructor component (with input specs preserved) in the environment.
	if err := addConstructorToEnvironment(&compCopy, baseID, tgd); err != nil {
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

	// Build the upload transformation using constructor-aware descriptor assembly.
	if err := addConstructorUploadTransformation(&compCopy, baseID, targetRepoSpec, toRepo, tgd, resourceTransformIDs, sourceTransformIDs, referenceDigestIDs); err != nil {
		return err
	}

	// Add ComputeComponentDigest only if another component references this one
	if !skipDigestProcessing {
		if _, isReferenced := referencedComponents[baseID]; isReferenced {
			addComputeDigestTransformation(baseID, true, expectedDigests[baseID+"Digest"], tgd)
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

	if err := addDescriptorToEnvironment(v2desc, baseID, tgd); err != nil {
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

			// Write the blob content (fetched during discovery) to a temp file
			// and store the file access spec in the environment.
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

			fileEnvKey := addResourceID + "File"
			tgd.Environment.Data[fileEnvKey] = map[string]any{
				"type": "File/v1alpha1",
				"uri":  "file://" + blobFile.Name(),
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
					"file":       fmt.Sprintf("${environment.%s}", fileEnvKey),
				}},
			}
			tgd.Transformations = append(tgd.Transformations, addResourceTransform)
			resourceTransformIDs[local.Index] = addResourceID
		}

		// Upload transformation for external component
		if err := addUploadTransformation(v2desc, baseID, targetRepoSpec, toRepo, tgd, resourceTransformIDs, nil, nil); err != nil {
			return err
		}
	}

	// Add ComputeComponentDigest only if another component references this one
	if !skipDigestProcessing {
		if _, isReferenced := referencedComponents[baseID]; isReferenced {
			hasUpload := copyPolicy == ExternalComponentVersionCopyPolicyCopyOrFail
			addComputeDigestTransformation(baseID, hasUpload, expectedDigests[baseID+"Digest"], tgd)
		}
	}

	return nil
}

// addDescriptorToEnvironment marshals the v2 descriptor and adds it to the graph environment.
// Used for external components that are already in descriptor format.
func addDescriptorToEnvironment(v2desc *descriptorv2.Descriptor, id string, tgd *transformv1alpha1.TransformationGraphDefinition) error {
	rawV2Desc, err := json.Marshal(v2desc)
	if err != nil {
		return fmt.Errorf("cannot marshal v2 descriptor: %w", err)
	}
	mapDesc := make(map[string]any)
	if err := json.Unmarshal(rawV2Desc, &mapDesc); err != nil {
		return fmt.Errorf("cannot unmarshal v2 descriptor: %w", err)
	}
	tgd.Environment.Data[id] = mapDesc
	return nil
}

// addConstructorToEnvironment converts the runtime constructor component to v1 spec format
// and stores it in the graph environment, preserving input specifications.
func addConstructorToEnvironment(component *constructor.Component, id string, tgd *transformv1alpha1.TransformationGraphDefinition) error {
	v1comp, err := constructor.ConvertToV1Component(component)
	if err != nil {
		return fmt.Errorf("cannot convert constructor component to v1: %w", err)
	}

	rawComp, err := json.Marshal(v1comp)
	if err != nil {
		return fmt.Errorf("cannot marshal constructor component: %w", err)
	}
	mapComp := make(map[string]any)
	if err := json.Unmarshal(rawComp, &mapComp); err != nil {
		return fmt.Errorf("cannot unmarshal constructor component: %w", err)
	}
	tgd.Environment.Data[id] = mapComp
	return nil
}

// addComputeDigestTransformation adds a ComputeComponentDigest transformation for a component.
// If hasUpload is true, the descriptor is taken from the Upload transformation's spec.
// Otherwise (external component without copy), it is taken from the environment.
// If expectedDigest is non-nil, it is included in the spec for verification.
func addComputeDigestTransformation(baseID string, hasUpload bool, expectedDigest *constructor.Digest, tgd *transformv1alpha1.TransformationGraphDefinition) {
	digestID := baseID + "Digest"

	var descriptorRef string
	if hasUpload {
		descriptorRef = fmt.Sprintf("${%sUpload.spec.descriptor}", baseID)
	} else {
		descriptorRef = fmt.Sprintf("${environment.%s}", baseID)
	}

	specData := map[string]any{
		"descriptor": descriptorRef,
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

// addUploadTransformation creates the final AddComponentVersion upload transformation.
func addUploadTransformation(
	v2desc *descriptorv2.Descriptor,
	baseID string,
	targetRepoSpec runtime.Typed,
	toRepo *runtime.Unstructured,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	resourceTransformIDs map[int]string,
	sourceTransformIDs map[int]string,
	referenceDigestIDs map[int]string,
) error {
	descriptorSpec := buildDescriptorSpec(v2desc, baseID, resourceTransformIDs, sourceTransformIDs, referenceDigestIDs)

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
// for constructor components. It assembles a v2-compatible descriptor from constructor
// environment data using CEL expression references.
func addConstructorUploadTransformation(
	component *constructor.Component,
	baseID string,
	targetRepoSpec runtime.Typed,
	toRepo *runtime.Unstructured,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	resourceTransformIDs map[int]string,
	sourceTransformIDs map[int]string,
	referenceDigestIDs map[int]string,
) error {
	descriptorSpec := buildConstructorDescriptorSpec(component, baseID, resourceTransformIDs, sourceTransformIDs, referenceDigestIDs, tgd)

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

// buildDescriptorSpec constructs the descriptor specification for the upload transformation.
// Modified resources/sources reference their Add transformation outputs via CEL.
// References get their digest from ComputeComponentDigest transformations.
func buildDescriptorSpec(
	v2desc *descriptorv2.Descriptor,
	id string,
	resourceTransformIDs map[int]string,
	sourceTransformIDs map[int]string,
	referenceDigestIDs map[int]string,
) any {
	if len(resourceTransformIDs) == 0 && len(sourceTransformIDs) == 0 && len(referenceDigestIDs) == 0 {
		return fmt.Sprintf("${environment.%s}", id)
	}

	// Build resources array
	resourcesArray := make([]any, len(v2desc.Component.Resources))
	for i := range v2desc.Component.Resources {
		if addID, ok := resourceTransformIDs[i]; ok {
			resourcesArray[i] = fmt.Sprintf("${%s.output.resource}", addID)
		} else {
			resourcesArray[i] = fmt.Sprintf("${environment.%s.component.resources[%d]}", id, i)
		}
	}

	// Build sources array
	var sourcesSpec any
	if len(v2desc.Component.Sources) > 0 {
		sourcesArray := make([]any, len(v2desc.Component.Sources))
		for i := range v2desc.Component.Sources {
			if addID, ok := sourceTransformIDs[i]; ok {
				sourcesArray[i] = fmt.Sprintf("${%s.output.source}", addID)
			} else {
				sourcesArray[i] = fmt.Sprintf("${environment.%s.component.sources[%d]}", id, i)
			}
		}
		sourcesSpec = sourcesArray
	} else {
		sourcesSpec = nil
	}

	// Build references array
	var referencesSpec any
	if len(v2desc.Component.References) > 0 {
		refsArray := make([]any, len(v2desc.Component.References))
		for i := range v2desc.Component.References {
			if digestID, ok := referenceDigestIDs[i]; ok {
				// Build the reference with the digest from ComputeComponentDigest
				ref := v2desc.Component.References[i]
				refMap := map[string]any{
					"name":          fmt.Sprintf("${environment.%s.component.componentReferences[%d].name}", id, i),
					"version":       fmt.Sprintf("${environment.%s.component.componentReferences[%d].version}", id, i),
					"componentName": fmt.Sprintf("${environment.%s.component.componentReferences[%d].componentName}", id, i),
					"digest":        fmt.Sprintf("${%s.output.digest}", digestID),
				}
				if ref.ExtraIdentity != nil {
					refMap["extraIdentity"] = fmt.Sprintf("${environment.%s.component.componentReferences[%d].extraIdentity}", id, i)
				}
				if ref.Labels != nil {
					refMap["labels"] = fmt.Sprintf("${environment.%s.component.componentReferences[%d].labels}", id, i)
				}
				refsArray[i] = refMap
			} else {
				refsArray[i] = fmt.Sprintf("${environment.%s.component.componentReferences[%d]}", id, i)
			}
		}
		referencesSpec = refsArray
	} else {
		referencesSpec = nil
	}

	componentMap := map[string]any{
		"name":                fmt.Sprintf("${environment.%s.component.name}", id),
		"version":             fmt.Sprintf("${environment.%s.component.version}", id),
		"provider":            fmt.Sprintf("${environment.%s.component.provider}", id),
		"resources":           resourcesArray,
		"sources":             sourcesSpec,
		"componentReferences": referencesSpec,
	}

	if v2desc.Component.RepositoryContexts != nil {
		componentMap["repositoryContexts"] = fmt.Sprintf("${environment.%s.component.repositoryContexts}", id)
	} else {
		componentMap["repositoryContexts"] = nil
	}

	descSpecMap := map[string]any{
		"meta":      fmt.Sprintf("${environment.%s.meta}", id),
		"component": componentMap,
	}

	if v2desc.Signatures != nil {
		descSpecMap["signatures"] = fmt.Sprintf("${environment.%s.signatures}", id)
	}

	return descSpecMap
}

// buildConstructorDescriptorSpec constructs a v2-compatible descriptor specification
// from constructor component data stored in the environment. The constructor v1 spec
// has a different field layout than the v2 descriptor, so this function maps between them:
//   - Constructor fields are at the top level (name, version, provider, resources, etc.)
//   - v2 descriptor wraps them under meta + component
//   - Constructor provider is {name, labels} while v2 is a plain string
//   - Constructor has no meta, repositoryContexts, or signatures
func buildConstructorDescriptorSpec(
	component *constructor.Component,
	id string,
	resourceTransformIDs map[int]string,
	sourceTransformIDs map[int]string,
	referenceDigestIDs map[int]string,
	tgd *transformv1alpha1.TransformationGraphDefinition,
) any {
	// Build resources array
	// For resources handled by AddLocalResource, reference the transformation output.
	// For by-reference resources, store them individually in the environment as v2-compatible
	// entries to avoid CEL type inference issues with heterogeneous arrays (input vs access).
	resourcesArray := make([]any, len(component.Resources))
	for i := range component.Resources {
		if addID, ok := resourceTransformIDs[i]; ok {
			resourcesArray[i] = fmt.Sprintf("${%s.output.resource}", addID)
		} else {
			resEnvKey := fmt.Sprintf("%sResource%d", id, i)
			relation := component.Resources[i].Relation
			resMap := map[string]any{
				"name":     component.Resources[i].Name,
				"version":  component.Resources[i].Version,
				"type":     component.Resources[i].Type,
				"relation": string(relation),
			}
			if component.Resources[i].HasAccess() {
				accessData, err := json.Marshal(component.Resources[i].Access)
				if err == nil {
					var accessMap map[string]any
					if err := json.Unmarshal(accessData, &accessMap); err == nil {
						resMap["access"] = accessMap
					}
				}
			}
			if component.Resources[i].ExtraIdentity != nil {
				resMap["extraIdentity"] = map[string]string(component.Resources[i].ExtraIdentity)
			}
			tgd.Environment.Data[resEnvKey] = resMap
			resourcesArray[i] = fmt.Sprintf("${environment.%s}", resEnvKey)
		}
	}

	// Build sources array
	// Same pattern: store by-reference sources as individual environment entries.
	var sourcesSpec any
	if len(component.Sources) > 0 {
		sourcesArray := make([]any, len(component.Sources))
		for i := range component.Sources {
			if addID, ok := sourceTransformIDs[i]; ok {
				sourcesArray[i] = fmt.Sprintf("${%s.output.source}", addID)
			} else {
				srcEnvKey := fmt.Sprintf("%sSource%d", id, i)
				srcMap := map[string]any{
					"name":    component.Sources[i].Name,
					"version": component.Sources[i].Version,
					"type":    component.Sources[i].Type,
				}
				if component.Sources[i].HasAccess() {
					accessData, err := json.Marshal(component.Sources[i].Access)
					if err == nil {
						var accessMap map[string]any
						if err := json.Unmarshal(accessData, &accessMap); err == nil {
							srcMap["access"] = accessMap
						}
					}
				}
				if component.Sources[i].ExtraIdentity != nil {
					srcMap["extraIdentity"] = map[string]string(component.Sources[i].ExtraIdentity)
				}
				tgd.Environment.Data[srcEnvKey] = srcMap
				sourcesArray[i] = fmt.Sprintf("${environment.%s}", srcEnvKey)
			}
		}
		sourcesSpec = sourcesArray
	} else {
		sourcesSpec = nil
	}

	// Build references array
	var referencesSpec any
	if len(component.References) > 0 {
		refsArray := make([]any, len(component.References))
		for i, ref := range component.References {
			if digestID, ok := referenceDigestIDs[i]; ok {
				refMap := map[string]any{
					"name":          ref.Name,
					"version":       ref.Version,
					"componentName": ref.Component,
					"digest":        fmt.Sprintf("${%s.output.digest}", digestID),
				}
				if ref.ExtraIdentity != nil {
					refMap["extraIdentity"] = map[string]string(ref.ExtraIdentity)
				}
				if ref.Labels != nil {
					labelsData, err := json.Marshal(ref.Labels)
					if err == nil {
						var labelsAny []any
						if err := json.Unmarshal(labelsData, &labelsAny); err == nil {
							refMap["labels"] = labelsAny
						}
					}
				}
				refsArray[i] = refMap
			} else {
				refMap := map[string]any{
					"name":          ref.Name,
					"version":       ref.Version,
					"componentName": ref.Component,
				}
				if ref.ExtraIdentity != nil {
					refMap["extraIdentity"] = map[string]string(ref.ExtraIdentity)
				}
				if ref.Labels != nil {
					labelsData, err := json.Marshal(ref.Labels)
					if err == nil {
						var labelsAny []any
						if err := json.Unmarshal(labelsData, &labelsAny); err == nil {
							refMap["labels"] = labelsAny
						}
					}
				}
				refsArray[i] = refMap
			}
		}
		referencesSpec = refsArray
	} else {
		referencesSpec = nil
	}

	componentMap := map[string]any{
		"name":                component.Name,
		"version":             component.Version,
		"provider":            component.Provider.Name,
		"resources":           resourcesArray,
		"sources":             sourcesSpec,
		"componentReferences": referencesSpec,
		"repositoryContexts":  nil,
	}

	if component.CreationTime != "" {
		componentMap["creationTime"] = component.CreationTime
	}

	if len(component.Labels) > 0 {
		labelsData, err := json.Marshal(component.Labels)
		if err == nil {
			var labelsAny []any
			if err := json.Unmarshal(labelsData, &labelsAny); err == nil {
				componentMap["labels"] = labelsAny
			}
		}
	}

	return map[string]any{
		"meta":      map[string]any{"schemaVersion": "v2"},
		"component": componentMap,
	}
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
