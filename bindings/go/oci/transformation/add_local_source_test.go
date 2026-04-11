package transformation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockSourceRepository implements ComponentVersionRepository for testing source operations
type mockSourceRepository struct {
	repository.ComponentVersionRepository
	addedSource *descriptor.Source
	addedBlob   blob.ReadOnlyBlob
	component   string
	version     string
}

func (m *mockSourceRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, nil
}

func (m *mockSourceRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return nil, nil, nil
}

func (m *mockSourceRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	m.component = component
	m.version = version
	m.addedSource = src
	m.addedBlob = content

	// Return updated source with LocalBlob access
	updated := src.DeepCopy()
	updated.Access = &v2.LocalBlob{
		Type: runtime.Type{
			Name:    v2.LocalBlobAccessType,
			Version: v2.LocalBlobAccessTypeVersion,
		},
		MediaType:      "application/octet-stream",
		LocalReference: "sha256:test-digest",
	}
	return updated, nil
}

func (m *mockSourceRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, nil
}

// mockSourceRepoProvider implements ComponentVersionRepositoryProvider for testing
type mockSourceRepoProvider struct {
	repo *mockSourceRepository
}

func (m *mockSourceRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m *mockSourceRepoProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	return m.repo, nil
}

func (m *mockSourceRepoProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func TestAddLocalSource_Transform_OCI(t *testing.T) {
	ctx := context.Background()

	// Create temporary file with test data
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test-source.bin")
	testBlobData := []byte("test source blob content")
	err := os.WriteFile(testFile, testBlobData, 0644)
	require.NoError(t, err)

	mockRepo := &mockSourceRepository{}
	mockProvider := &mockSourceRepoProvider{repo: mockRepo}

	// Create a combined scheme with both v1alpha1 and v2 types
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddLocalSource{}, v1alpha1.OCIAddLocalSourceV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddLocalSource{}, v1alpha1.CTFAddLocalSourceV1alpha1)

	transformer := &AddLocalSource{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create transformation spec
	spec := &v1alpha1.OCIAddLocalSource{
		Type: runtime.NewVersionedType(v1alpha1.OCIAddLocalSourceType, v1alpha1.Version),
		ID:   "test-transform",
		Spec: &v1alpha1.OCIAddLocalSourceSpec{
			Repository: ocispec.Repository{
				Type: runtime.Type{
					Name:    ocispec.Type,
					Version: "v1",
				},
				BaseUrl: "ghcr.io/test/components",
			},
			Component: "ocm.software/test-component",
			Version:   "1.0.0",
			Source: &v2.Source{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "git",
			},
			File: blobv1alpha1.File{
				Type: runtime.Type{
					Name:    blobv1alpha1.FileType,
					Version: blobv1alpha1.Version,
				},
				URI:       "file://" + testFile,
				MediaType: "application/test",
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.OCIAddLocalSource)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Source)

	// Verify updated source has LocalBlob access
	assert.Equal(t, v2.LocalBlobAccessType, transformed.Output.Source.Access.GetType().Name)

	// Verify repository interactions
	assert.Equal(t, "ocm.software/test-component", mockRepo.component)
	assert.Equal(t, "1.0.0", mockRepo.version)
	assert.NotNil(t, mockRepo.addedSource)
	assert.Equal(t, "test-source", mockRepo.addedSource.Name)
}

func TestAddLocalSource_Transform_CTF(t *testing.T) {
	ctx := context.Background()

	// Create temporary file with test data
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test-ctf-source.bin")
	testBlobData := []byte("test source blob content for CTF")
	err := os.WriteFile(testFile, testBlobData, 0644)
	require.NoError(t, err)

	mockRepo := &mockSourceRepository{}
	mockProvider := &mockSourceRepoProvider{repo: mockRepo}

	// Create a combined scheme with both v1alpha1 and v2 types
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddLocalSource{}, v1alpha1.OCIAddLocalSourceV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddLocalSource{}, v1alpha1.CTFAddLocalSourceV1alpha1)

	transformer := &AddLocalSource{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create CTF transformation spec
	spec := &v1alpha1.CTFAddLocalSource{
		Type: runtime.NewVersionedType(v1alpha1.CTFAddLocalSourceType, v1alpha1.Version),
		ID:   "test-ctf-transform",
		Spec: &v1alpha1.CTFAddLocalSourceSpec{
			Repository: ctfspec.Repository{
				Type: runtime.Type{
					Name:    ctfspec.Type,
					Version: "v1",
				},
				FilePath: "/tmp/test-archive.tar",
			},
			Component: "ocm.software/ctf-component",
			Version:   "2.0.0",
			Source: &v2.Source{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "ctf-source",
						Version: "2.0.0",
					},
				},
				Type: "git",
			},
			File: blobv1alpha1.File{
				Type: runtime.Type{
					Name:    blobv1alpha1.FileType,
					Version: blobv1alpha1.Version,
				},
				URI:       "file://" + testFile,
				MediaType: "application/octet-stream",
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.CTFAddLocalSource)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Source)

	// Verify repository interactions
	assert.Equal(t, "ocm.software/ctf-component", mockRepo.component)
	assert.Equal(t, "2.0.0", mockRepo.version)
}

func TestAddLocalSource_Transform_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		spec        *v1alpha1.OCIAddLocalSourceSpec
		expectedErr string
	}{
		{
			name: "missing component",
			spec: &v1alpha1.OCIAddLocalSourceSpec{
				Component: "",
				Version:   "1.0.0",
				Source:    &v2.Source{},
			},
			expectedErr: "component name is required",
		},
		{
			name: "missing version",
			spec: &v1alpha1.OCIAddLocalSourceSpec{
				Component: "test",
				Version:   "",
				Source:    &v2.Source{},
			},
			expectedErr: "component version is required",
		},
		{
			name: "missing source",
			spec: &v1alpha1.OCIAddLocalSourceSpec{
				Component: "test",
				Version:   "1.0.0",
				Source:    nil,
			},
			expectedErr: "source is required",
		},
		{
			name: "missing file URI",
			spec: &v1alpha1.OCIAddLocalSourceSpec{
				Component: "test",
				Version:   "1.0.0",
				Source:    &v2.Source{},
				File: blobv1alpha1.File{
					URI: "",
				},
			},
			expectedErr: "file URI is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockSourceRepository{}
			mockProvider := &mockSourceRepoProvider{repo: mockRepo}

			// Create a combined scheme with both v1alpha1 and v2 types
			combinedScheme := runtime.NewScheme()
			v2.MustAddToScheme(combinedScheme)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddLocalSource{}, v1alpha1.OCIAddLocalSourceV1alpha1)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddLocalSource{}, v1alpha1.CTFAddLocalSourceV1alpha1)

			transformer := &AddLocalSource{
				Scheme:       combinedScheme,
				RepoProvider: mockProvider,
			}

			spec := &v1alpha1.OCIAddLocalSource{
				Type: runtime.NewVersionedType(v1alpha1.OCIAddLocalSourceType, v1alpha1.Version),
				Spec: tt.spec,
			}

			result, err := transformer.Transform(ctx, spec)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
