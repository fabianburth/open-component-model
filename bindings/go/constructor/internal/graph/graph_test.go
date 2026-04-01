package graph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/internal/graph"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockExternalRepoProvider struct {
	repos map[string]repository.ComponentVersionRepository
}

func (m *mockExternalRepoProvider) GetExternalRepository(_ context.Context, name, version string) (repository.ComponentVersionRepository, error) {
	key := name + ":" + version
	if repo, ok := m.repos[key]; ok {
		return repo, nil
	}
	return nil, fmt.Errorf("external repo not found for %s: %w", key, repository.ErrNotFound)
}

func TestBuildGraphDefinition_EmptyConstructor(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	cc := &constructor.ComponentConstructor{}
	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp", nil,
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)
	r.NotNil(tgd)
	r.Empty(tgd.Transformations)
	r.NotNil(tgd.Environment)
}

func TestBuildGraphDefinition_SimpleComponent(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Create a simple component with a file input resource
	fileInput := &runtime.Raw{
		Type: runtime.NewVersionedType("file", "v1"),
		Data: json.RawMessage(`{"path":"data.txt","mediaType":"text/plain"}`),
	}

	cc := &constructor.ComponentConstructor{
		Components: []constructor.Component{
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/my-component",
						Version: "1.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
				Resources: []constructor.Resource{
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "my-resource",
								Version: "1.0.0",
							},
						},
						Type:     "ociImage",
						Relation: constructor.LocalRelation,
						AccessOrInput: constructor.AccessOrInput{
							Input: fileInput,
						},
					},
				},
			},
		},
	}

	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp/workdir", nil,
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)
	r.NotNil(tgd)

	// Should have: FileInput + AddLocalResource + AddComponentVersion + ComputeComponentDigest
	r.Len(tgd.Transformations, 4, "expected 4 transformations")

	// Verify types
	types := make([]string, len(tgd.Transformations))
	for i, t := range tgd.Transformations {
		types[i] = t.Type.String()
	}
	r.Contains(types, "FileInput/v1alpha1")
	r.Contains(types, "OCIAddLocalResource/v1alpha1")
	r.Contains(types, "OCIAddComponentVersion/v1alpha1")
	r.Contains(types, "ComputeComponentDigest/v1alpha1")

	// Verify environment has the component in constructor format (not descriptor format)
	r.NotEmpty(tgd.Environment.Data)
	envData, ok := tgd.Environment.Data["constructExampleComMyComponent100"].(map[string]any)
	r.True(ok, "environment should contain constructor component data")

	// Constructor format: fields at top level (not nested under "component")
	r.Equal("example.com/my-component", envData["name"])
	r.Equal("1.0.0", envData["version"])

	// Provider should be an object with "name", not a plain string
	provider, ok := envData["provider"].(map[string]any)
	r.True(ok, "provider should be an object")
	r.Equal("test-provider", provider["name"])

	// No "meta" or "component" wrapper — these are v2 descriptor artifacts
	r.Nil(envData["meta"], "constructor format should not have meta")
	r.Nil(envData["component"], "constructor format should not have component wrapper")

	// Resource should preserve the input specification, not have a placeholder access
	resources, ok := envData["resources"].([]any)
	r.True(ok, "resources should be an array")
	r.Len(resources, 1)
	res, ok := resources[0].(map[string]any)
	r.True(ok)
	r.NotNil(res["input"], "resource should preserve input spec")
	r.Nil(res["access"], "resource with input should not have a placeholder access")
}

func TestBuildGraphDefinition_ComponentWithSource(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	fileInput := &runtime.Raw{
		Type: runtime.NewVersionedType("file", "v1"),
		Data: json.RawMessage(`{"path":"main.go","mediaType":"text/plain"}`),
	}

	cc := &constructor.ComponentConstructor{
		Components: []constructor.Component{
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/my-component",
						Version: "1.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
				Sources: []constructor.Source{
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "source",
								Version: "1.0.0",
							},
						},
						Type: "git",
						AccessOrInput: constructor.AccessOrInput{
							Input: fileInput,
						},
					},
				},
			},
		},
	}

	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp/workdir", nil,
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)

	// Should have: FileInput(src) + AddLocalSource + AddComponentVersion + ComputeComponentDigest
	r.Len(tgd.Transformations, 4)

	types := make([]string, len(tgd.Transformations))
	for i, t := range tgd.Transformations {
		types[i] = t.Type.String()
	}
	r.Contains(types, "FileInput/v1alpha1")
	r.Contains(types, "OCIAddLocalSource/v1alpha1")
	r.Contains(types, "OCIAddComponentVersion/v1alpha1")
	r.Contains(types, "ComputeComponentDigest/v1alpha1")
}

func TestBuildGraphDefinition_ComponentWithByReferenceResource(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	ociAccess := &runtime.Raw{
		Type: runtime.NewVersionedType("ociImage", "v1"),
		Data: json.RawMessage(`{"imageReference":"ghcr.io/test/image:1.0.0"}`),
	}

	cc := &constructor.ComponentConstructor{
		Components: []constructor.Component{
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/my-component",
						Version: "1.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
				Resources: []constructor.Resource{
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "image",
								Version: "1.0.0",
							},
						},
						Type:     "ociImage",
						Relation: constructor.ExternalRelation,
						AccessOrInput: constructor.AccessOrInput{
							Access: ociAccess,
						},
					},
				},
			},
		},
	}

	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp/workdir", nil,
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)

	// By-reference resource: ProcessOCIResourceDigest + AddComponentVersion + ComputeComponentDigest
	r.Len(tgd.Transformations, 3)

	types := make([]string, len(tgd.Transformations))
	for i, t := range tgd.Transformations {
		types[i] = t.Type.String()
	}
	r.Contains(types, "ProcessOCIResourceDigest/v1alpha1")
	r.Contains(types, "OCIAddComponentVersion/v1alpha1")
	r.Contains(types, "ComputeComponentDigest/v1alpha1")
}

func TestBuildGraphDefinition_ComponentWithReference(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Component A (referenced) and Component B (references A)
	cc := &constructor.ComponentConstructor{
		Components: []constructor.Component{
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/comp-a",
						Version: "1.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
			},
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/comp-b",
						Version: "2.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
				References: []constructor.Reference{
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "comp-a-ref",
								Version: "1.0.0",
							},
						},
						Component: "example.com/comp-a",
					},
				},
			},
		},
	}

	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp/workdir", nil,
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)
	r.NotNil(tgd)

	// Component A: AddComponentVersion + ComputeComponentDigest
	// Component B: AddComponentVersion + ComputeComponentDigest
	// Total: 4 transformations
	r.Len(tgd.Transformations, 4)

	// Check that B's upload references A's digest
	var compBUpload *runtime.Unstructured
	for _, t := range tgd.Transformations {
		if t.Type.String() == "OCIAddComponentVersion/v1alpha1" {
			spec := t.Spec.Data
			if descSpec, ok := spec["descriptor"].(map[string]any); ok {
				if compMap, ok := descSpec["component"].(map[string]any); ok {
					if refs, ok := compMap["componentReferences"]; ok && refs != nil {
						compBUpload = t.Spec
						break
					}
				}
			}
		}
	}
	r.NotNil(compBUpload, "should find comp-b upload with componentReferences")

	// Verify environment has both descriptors
	r.Len(tgd.Environment.Data, 2)
}

func TestBuildGraphDefinition_ExternalComponentSkip(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Component A references external component E
	cc := &constructor.ComponentConstructor{
		Components: []constructor.Component{
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/comp-a",
						Version: "1.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
				References: []constructor.Reference{
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "ext-ref",
								Version: "1.0.0",
							},
						},
						Component: "example.com/external",
					},
				},
			},
		},
	}

	extRepo := &mockComponentVersionRepo{
		descriptors: map[string]*descriptor.Descriptor{
			"example.com/external:1.0.0": {
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "example.com/external",
							Version: "1.0.0",
						},
					},
					Provider: descriptor.Provider{Name: "external-provider"},
				},
			},
		},
	}

	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp/workdir",
		&mockExternalRepoProvider{repos: map[string]repository.ComponentVersionRepository{
			"example.com/external:1.0.0": extRepo,
		}},
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)
	r.NotNil(tgd)

	// External component with Skip policy: only ComputeComponentDigest (no upload)
	// Constructor component A: AddComponentVersion + ComputeComponentDigest
	// External component E: ComputeComponentDigest only (no upload with Skip policy)
	r.Len(tgd.Transformations, 3)

	// Verify environment has both descriptors
	r.Len(tgd.Environment.Data, 2)
}

func TestBuildGraphDefinition_CELExpressionWiring(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	fileInput := &runtime.Raw{
		Type: runtime.NewVersionedType("file", "v1"),
		Data: json.RawMessage(`{"path":"data.txt"}`),
	}

	cc := &constructor.ComponentConstructor{
		Components: []constructor.Component{
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/my-component",
						Version: "1.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
				Resources: []constructor.Resource{
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "my-resource",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: constructor.LocalRelation,
						AccessOrInput: constructor.AccessOrInput{
							Input: fileInput,
						},
					},
				},
			},
		},
	}

	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp/workdir", nil,
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)

	// Find the AddLocalResource transformation and verify it references the input output
	for _, t := range tgd.Transformations {
		if t.Type.String() == "OCIAddLocalResource/v1alpha1" {
			file := t.Spec.Data["file"]
			r.NotNil(file)
			fileStr, ok := file.(string)
			r.True(ok)
			r.Contains(fileStr, ".output.file}")
			r.Contains(fileStr, "${")
		}
	}
}

// mockComponentVersionRepo implements repository.ComponentVersionRepository for testing.
type mockComponentVersionRepo struct {
	descriptors map[string]*descriptor.Descriptor
}

func (m *mockComponentVersionRepo) GetComponentVersion(_ context.Context, name, version string) (*descriptor.Descriptor, error) {
	key := name + ":" + version
	if desc, ok := m.descriptors[key]; ok {
		return desc, nil
	}
	return nil, repository.ErrNotFound
}

func (m *mockComponentVersionRepo) AddComponentVersion(_ context.Context, _ *descriptor.Descriptor) error {
	return nil
}

func (m *mockComponentVersionRepo) ListComponentVersions(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *mockComponentVersionRepo) AddLocalResource(_ context.Context, _, _ string, _ *descriptor.Resource, _ blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, nil
}

func (m *mockComponentVersionRepo) AddLocalSource(_ context.Context, _, _ string, _ *descriptor.Source, _ blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, nil
}

func (m *mockComponentVersionRepo) GetLocalResource(_ context.Context, _, _ string, _ runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return nil, nil, nil
}

func (m *mockComponentVersionRepo) GetLocalSource(_ context.Context, _, _ string, _ runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, nil
}

func TestBuildGraphDefinition_MixedInputAndAccessResources(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	fileInput := &runtime.Raw{
		Type: runtime.NewVersionedType("file", "v1"),
		Data: json.RawMessage(`{"path":"data.txt","mediaType":"text/plain"}`),
	}
	ociAccess := &runtime.Raw{
		Type: runtime.NewVersionedType("ociImage", "v1"),
		Data: json.RawMessage(`{"imageReference":"ghcr.io/test/image:1.0.0"}`),
	}

	cc := &constructor.ComponentConstructor{
		Components: []constructor.Component{
			{
				ComponentMeta: constructor.ComponentMeta{
					ObjectMeta: constructor.ObjectMeta{
						Name:    "example.com/mixed",
						Version: "1.0.0",
					},
				},
				Provider: constructor.Provider{Name: "test-provider"},
				Resources: []constructor.Resource{
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "local-resource",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: constructor.LocalRelation,
						AccessOrInput: constructor.AccessOrInput{
							Input: fileInput,
						},
					},
					{
						ElementMeta: constructor.ElementMeta{
							ObjectMeta: constructor.ObjectMeta{
								Name:    "external-image",
								Version: "1.0.0",
							},
						},
						Type:     "ociImage",
						Relation: constructor.ExternalRelation,
						AccessOrInput: constructor.AccessOrInput{
							Access: ociAccess,
						},
					},
				},
			},
		},
	}

	target := &oci.Repository{
		Type:    runtime.NewVersionedType("OCIRepository", "v1"),
		BaseUrl: "ghcr.io/test",
	}

	tgd, err := graph.BuildGraphDefinition(ctx, cc, target, "/tmp/workdir", nil,
		graph.ExternalComponentVersionCopyPolicySkip,
		graph.ComponentVersionConflictReplace, false)
	r.NoError(err)
	r.NotNil(tgd)

	// Should have: FileInput + AddLocalResource + ProcessOCIResourceDigest + AddComponentVersion + ComputeComponentDigest
	r.Len(tgd.Transformations, 5)

	// Verify environment preserves both input and access correctly
	envData, ok := tgd.Environment.Data["constructExampleComMixed100"].(map[string]any)
	r.True(ok, "environment should contain constructor component data")

	resources, ok := envData["resources"].([]any)
	r.True(ok)
	r.Len(resources, 2)

	// First resource (input-based) should have input, no access
	inputRes, ok := resources[0].(map[string]any)
	r.True(ok)
	r.NotNil(inputRes["input"], "input resource should preserve input spec")
	r.Nil(inputRes["access"], "input resource should not have access")

	// Second resource (access-based) should have access, no input
	accessRes, ok := resources[1].(map[string]any)
	r.True(ok)
	r.Nil(accessRes["input"], "access resource should not have input")
	r.NotNil(accessRes["access"], "access resource should preserve access spec")
}
