package internal

import (
	"context"
	"fmt"
	"log/slog"

	graphinternal "ocm.software/open-component-model/bindings/go/transform/internal"
	"ocm.software/open-component-model/bindings/go/dag"
	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/signing/transformation/spec/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// TransferRoot pairs a DAG root key with its target repositories and source resolver.
//
// Design notes:
//   - SourceResolver is intentionally a single resolver per component: a component version
//     has exactly one source, even when it is pushed to multiple targets. The component is
//     fetched once and uploaded to all Targets. Callers that supply conflicting resolvers for
//     the same component key receive an error from collectTransferRoots before
//     BuildGraphDefinition is invoked.
//   - There is intentionally no TargetSpecResolver to mirror SourceResolver. Target
//     repositories are opened directly from their runtime.Typed specs by the builder
//     (transform executor), not by the transfer layer. Dynamic per-component target routing
//     can be added in the future if needed.
//   - During recursive discovery, child components inherit their parent's Targets and
//     SourceResolver. All components in a dependency tree are therefore transferred to the
//     same set of targets as the root that references them.
type TransferRoot struct {
	// RootComponentKey is the "component:version" string used as a DAG root.
	RootComponentKey string
	// Targets is the list of target repository specs this component should be transferred to.
	// Each spec is opened independently by the builder; the transfer layer only tracks which
	// targets exist, not how to open them. Note that there is intentionally no
	// TargetSpecResolver here — see the struct doc for the rationale.
	Targets []runtime.Typed
	// SourceResolver resolves this root's component version from its source repository.
	// One resolver per component — see the struct doc for the design rationale.
	SourceResolver resolvers.ComponentVersionRepositoryResolver
}

// BuildGraphDefinition constructs a [transformv1alpha1.TransformationGraphDefinition] for
// transferring component versions (and optionally their resources) from source to target(s).
//
// The process has two phases:
//
//  1. Discovery: A concurrent DAG discoverer resolves each root component and, if recursive is true,
//     follows component references to build a complete dependency graph. During discovery, each
//     component's target repositories and resolver are tracked in shared maps (targetMap, resolverMap)
//     that the discoverer propagates from parent to child.
//
//  2. Graph construction: For each discovered component, transformation nodes are generated for
//     every assigned target repository. Each (component, target) pair produces:
//     - Get transformations for resources (fetching from source)
//     - Add transformations for resources (uploading to target)
//     - An AddComponentVersion upload transformation for the descriptor itself
//
// The returned graph definition can be validated and executed by a builder.Builder.
func BuildGraphDefinition(
	ctx context.Context,
	roots map[string]TransferRoot,
	recursive bool,
	copyMode int,
	uploadType int,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	// Seed the targetMap and resolverMap from explicit roots.
	// These maps are shared with the discoverer and multiResolver:
	// - targetMap: component key → list of target repository specs
	// - resolverMap: component key → resolver to fetch that component from its source
	// During recursive discovery, the discoverer will grow both maps as children are found.
	// Using a map[string]TransferRoot guarantees key uniqueness — no duplicate roots possible.
	targetMap := make(map[string][]runtime.Typed)
	resolverMap := make(map[string]resolvers.ComponentVersionRepositoryResolver)
	dagRoots := make([]string, 0, len(roots))
	for key, root := range roots {
		dagRoots = append(dagRoots, key)
		targetMap[key] = AppendUniqueRepositories(targetMap[key], root.Targets)
		resolverMap[key] = root.SourceResolver
	}

	disc := &discoverer{
		recursive:         recursive,
		discoveredDigests: make(map[string]descruntime.Digest),
		targetMap:         targetMap,
		resolverMap:       resolverMap,
	}
	// The multiResolver delegates to per-component resolvers from resolverMap.
	// The expectedDigest closure checks whether a recursively discovered child has
	// a pinned digest from its parent's reference, enabling integrity verification.
	res := &multiResolver{
		mu:          &disc.mu,
		resolverMap: resolverMap,
		expectedDigest: func(id runtime.Identity) *descruntime.Digest {
			disc.mu.Lock()
			defer disc.mu.Unlock()
			if !disc.recursive {
				return nil
			}
			dig, ok := disc.discoveredDigests[id.String()]
			if !ok {
				return nil
			}
			return &dig
		},
	}

	slog.DebugContext(ctx, "starting component discovery",
		"roots", dagRoots, "recursive", recursive)

	dr := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, *discoveryValue]{
		Roots:      dagRoots,
		Resolver:   res,
		Discoverer: disc,
	})

	if err := dr.Discover(ctx); err != nil {
		return nil, fmt.Errorf("recursive discovery failed: %w", err)
	}

	slog.DebugContext(ctx, "component discovery completed")

	tgd := &transformv1alpha1.TransformationGraphDefinition{
		Environment: &runtime.Unstructured{
			Data: map[string]any{},
		},
	}

	// Phase 2: walk the discovered DAG and generate transformation nodes per (component, target) pair.
	g := dr.Graph()
	err := g.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		return fillGraphDefinitionWithPrefetchedComponents(ctx, d, targetMap, tgd, copyMode, uploadType)
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

// fillGraphDefinitionWithPrefetchedComponents iterates over all discovered components in the DAG
// and generates transformation nodes for each (component, target) pair.
//
// For each component:
//  1. The descriptor is converted to v2 format and marshalled to a map for inlining.
//  2. For each assigned target, resource transformations (get/add pairs) are created based
//     on the resource access type (local blob, OCI artifact, Helm chart) and the copy mode.
//  3. A final AddComponentVersion upload transformation is appended, referencing the processed
//     resources via CEL expressions.
//
// When a component has multiple targets, transformation IDs are suffixed (e.g., "T0", "T1")
// to ensure uniqueness in the DAG. The descriptor map is shared across targets
// since it's source-side data.
func fillGraphDefinitionWithPrefetchedComponents(
	ctx context.Context,
	d *dag.DirectedAcyclicGraph[string],
	targetMap map[string][]runtime.Typed,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	copyMode int,
	uploadType int,
) error {
	slog.DebugContext(ctx, "building transformations for discovered components",
		"components", len(d.Vertices))

	// Scan for referenced components that need digest computation.
	// A component is "referenced" if another component's descriptor lists it in
	// componentReferences. For each such child we record:
	//   - referencedComponents: set of baseIDs that need a ComputeComponentDigest node
	//   - expectedDigests: the digest the parent declared, keyed by "<baseID>Digest"
	referencedComponents := make(map[string]struct{})
	expectedDigests := make(map[string]*descruntime.Digest)
	for _, v := range d.Vertices {
		val := v.Attributes[dagsync.AttributeValue].(*discoveryValue)
		for _, ref := range val.Descriptor.Component.References {
			refID := graphinternal.IdentityToTransformationID("transform", ref.ToComponentIdentity())
			referencedComponents[refID] = struct{}{}
			if ref.Digest.Value != "" {
				dig := ref.Digest // capture loop variable
				expectedDigests[refID+"Digest"] = &dig
			}
		}
	}

	for key, v := range d.Vertices {
		val := v.Attributes[dagsync.AttributeValue].(*discoveryValue)
		component := val.Descriptor.Component.Name
		version := val.Descriptor.Component.Version

		baseID := graphinternal.IdentityToTransformationID("transform", runtime.Identity{
			descruntime.IdentityAttributeName:    component,
			descruntime.IdentityAttributeVersion: version,
		})

		v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), val.Descriptor)
		if err != nil {
			return fmt.Errorf("cannot convert to v2: %w", err)
		}

		descMap, err := graphinternal.DescriptorToMap(v2desc)
		if err != nil {
			return err
		}

		// Build referenceDigestIDs for this component's references so the upload
		// transformation's descriptor carries CEL refs to child digest outputs.
		referenceDigestIDs := make(map[int]string)
		for i, ref := range v2desc.Component.References {
			refBaseID := graphinternal.IdentityToTransformationID("transform", ref.ToComponentIdentity())
			if _, ok := referencedComponents[refBaseID]; ok {
				referenceDigestIDs[i] = refBaseID + "Digest"
			}
		}

		targets := targetMap[key]
		slog.DebugContext(ctx, "processing component for transfer",
			"component", component, "version", version,
			"targets", len(targets),
			"resources", len(v2desc.Component.Resources))

		for targetIdx, target := range targets {
			id := baseID
			if len(targets) > 1 {
				id = fmt.Sprintf("%sT%d", baseID, targetIdx)
			}

			slog.DebugContext(ctx, "generating transformations for target",
				"component", component, "version", version,
				"targetIndex", targetIdx, "targetType", fmt.Sprintf("%T", target),
				"transformID", id)

			resourceTransformIDs, err := processResources(ctx, v2desc, id, val, tgd, target, copyMode, uploadType)
			if err != nil {
				return err
			}

			if err := addUploadTransformation(descMap, id, target, tgd, resourceTransformIDs, referenceDigestIDs); err != nil {
				return err
			}
		}

		// Add ComputeComponentDigest if another component references this one.
		if _, isReferenced := referencedComponents[baseID]; isReferenced {
			addComputeDigestTransformation(baseID, expectedDigests[baseID+"Digest"], tgd)
		}
	}
	return nil
}

// processResources iterates over resources in a v2 descriptor and creates the appropriate
// get/add transformation pairs based on access type, copy mode, and upload type.
func processResources(ctx context.Context, v2desc *descriptorv2.Descriptor, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, copyMode int, uploadType int) (map[int]string, error) {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version
	resourceTransformIDs := make(map[int]string)

	for i, resource := range v2desc.Component.Resources {
		access, err := scheme.NewObject(resource.Access.Type)
		if err != nil {
			return nil, fmt.Errorf("cannot create new object for resource access type %q: %w", resource.Access.Type.String(), err)
		}
		if err := scheme.Convert(resource.Access, access); err != nil {
			return nil, fmt.Errorf("cannot convert resource access to typed object: %w", err)
		}

		if copyMode == CopyModeLocalBlobResources && !descriptorv2.IsLocalBlob(access) {
			logSkippedResource(ctx, component, version, resource, copyMode, uploadType)
			continue
		}

		if err := processResource(resource, access, id, val, tgd, toSpec, resourceTransformIDs, i, uploadType); err != nil {
			return nil, err
		}
	}
	return resourceTransformIDs, nil
}

func logSkippedResource(ctx context.Context, component, version string, resource descriptorv2.Resource, copyMode, uploadType int) {
	logLevel := slog.LevelDebug
	if uploadType == UploadAsOciArtifact {
		logLevel = slog.LevelWarn
	}
	slog.Log(ctx, logLevel,
		"Skipping copy of resource since its access type is not a local blob. Only resources with local blob access are copied when CopyModeLocalBlobResources is set.",
		"component", component,
		"version", version,
		"resource", resource.ToIdentity().String(),
		"accessType", resource.Access.Type.String(),
		"copyMode", copyMode)
}

// processResource dispatches a single resource to the appropriate handler based on its access type.
// Each handler creates a Get transformation (fetching the resource from the source) and an Add
// transformation (uploading it to the target). The uploadType and target type determine whether
// resources are stored as local blobs or separate OCI artifacts (including Helm charts) in the
// target repository.
func processResource(resource descriptorv2.Resource, access runtime.Typed, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadType int) error {
	_, isOCITarget := toSpec.(*oci.Repository)
	uploadAsArtifact := isOCITarget && uploadType == UploadAsOciArtifact

	switch acc := access.(type) {
	case *descriptorv2.LocalBlob:
		shouldUpload := uploadAsArtifact && isOCICompliantManifest(acc.MediaType) && acc.ReferenceName != ""
		if err := processLocalBlob(resource, acc, id, val, tgd, toSpec, resourceTransformIDs, i, shouldUpload); err != nil {
			return fmt.Errorf("failed processing local blob resource: %w", err)
		}
	case *ociv1.OCIImage:
		if err := processOCIArtifact(resource, id, val, tgd, toSpec, resourceTransformIDs, i, uploadAsArtifact); err != nil {
			return fmt.Errorf("cannot process OCI artifact resource: %w", err)
		}
	case *helmv1.Helm:
		if err := processHelm(resource, id, val, tgd, toSpec, resourceTransformIDs, i, uploadAsArtifact); err != nil {
			return fmt.Errorf("cannot process Helm Chart resource: %w", err)
		}
	default:
		slog.Info("Unsupported resource access type, skipping resource. Only local blob, OCI artifact, and Helm chart resources are supported for transformation.",
			"component", val.Descriptor.Component.Name, "version", val.Descriptor.Component.Version,
			"resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String())
	}
	return nil
}

// addUploadTransformation creates the final upload (AddComponentVersion) transformation
// for a component, reconstructing the descriptor with CEL references to modified resources
// and recomputed reference digests.
func addUploadTransformation(descMap map[string]any, id string, toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, referenceDigestIDs map[int]string) error {
	descriptorSpec := graphinternal.BuildInlineDescriptorSpec(descMap, resourceTransformIDs, nil, referenceDigestIDs)

	addType, err := graphinternal.ChooseAddType(toSpec)
	if err != nil {
		return fmt.Errorf("choosing add type for target repository: %w", err)
	}

	toRepo, err := graphinternal.AsUnstructured(toSpec)
	if err != nil {
		return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}

	upload := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: addType,
			ID:   id + "Upload",
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"descriptor": descriptorSpec,
		}},
	}

	tgd.Transformations = append(tgd.Transformations, upload)
	return nil
}

// addComputeDigestTransformation adds a ComputeComponentDigest transformation for a
// transferred component that is referenced by another. The descriptor is always taken
// from the Upload transformation's spec (transfer always produces an upload).
// If expectedDigest is non-nil, it is included for verification against the recomputed value.
func addComputeDigestTransformation(baseID string, expectedDigest *descruntime.Digest, tgd *transformv1alpha1.TransformationGraphDefinition) {
	digestID := baseID + "Digest"

	specData := map[string]any{
		"descriptor": fmt.Sprintf("${%sUpload.spec.descriptor}", baseID),
	}
	if expectedDigest != nil {
		specData["expectedDigest"] = map[string]any{
			"hashAlgorithm":          expectedDigest.HashAlgorithm,
			"normalisationAlgorithm": expectedDigest.NormalisationAlgorithm,
			"value":                  expectedDigest.Value,
		}
	}

	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: signingv1alpha1.ComputeComponentDigestV1alpha1,
			ID:   digestID,
		},
		Spec: &runtime.Unstructured{Data: specData},
	})
}
