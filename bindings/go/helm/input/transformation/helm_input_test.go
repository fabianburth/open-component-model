package transformation_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
	"ocm.software/open-component-model/bindings/go/helm/input/transformation"
	"ocm.software/open-component-model/bindings/go/helm/input/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&v1alpha1.HelmInput{}, v1alpha1.HelmInputV1alpha1)
	return s
}

func makeHelmInput(opts map[string]any) *runtime.Raw {
	m := map[string]any{
		"type": "helm/v1",
	}
	for k, v := range opts {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return &runtime.Raw{
		Type: runtime.NewVersionedType("helm", "v1"),
		Data: data,
	}
}

func TestHelmInput_Transform_LocalChart(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-local-chart",
		Spec: &v1alpha1.HelmInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeHelmInput(map[string]any{"path": "../../testdata/mychart"}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.HelmInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)

	// Verify the buffered file content is accessible
	blob, err := filesystem.GetBlobFromSpec(ctx, &out.Output.File)
	r.NoError(err)
	r.NotNil(blob)

	// Cleanup the temp file
	filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
	t.Cleanup(func() { _ = os.Remove(filePath) })
}

func TestHelmInput_Transform_LocalChartTgz(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-local-chart-tgz",
		Spec: &v1alpha1.HelmInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeHelmInput(map[string]any{"path": "../../testdata/provenance/mychart-0.1.0.tgz"}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.HelmInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)

	filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
	assert.FileExists(t, filePath)
	t.Cleanup(func() { _ = os.Remove(filePath) })
}

func TestHelmInput_Transform_WithOutputPath(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	outputDir := t.TempDir()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-output-path",
		Spec: &v1alpha1.HelmInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeHelmInput(map[string]any{"path": "../../testdata/mychart"}),
				},
			},
			OutputPath: outputDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.HelmInput)
	r.True(ok)
	r.NotNil(out.Output)

	// Verify the output file is in the specified directory
	filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
	assert.True(t, strings.HasPrefix(filePath, outputDir))
	assert.FileExists(t, filePath)
}

func TestHelmInput_Transform_WithRepository(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-with-repository",
		Spec: &v1alpha1.HelmInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeHelmInput(map[string]any{
						"path":       "../../testdata/mychart",
						"repository": "example.com/charts/mychart:0.1.0",
					}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.HelmInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)

	// Resource should be populated when Repository is set
	r.NotNil(out.Output.Resource)
	assert.Equal(t, "helmChart", out.Output.Resource.Type)
	assert.Equal(t, "mychart", out.Output.Resource.Name)
	assert.Equal(t, "0.1.0", out.Output.Resource.Version)
	assert.NotNil(t, out.Output.Resource.Access)

	filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
	t.Cleanup(func() { _ = os.Remove(filePath) })
}

func TestHelmInput_Transform_RepositoryVersionMismatch(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-version-mismatch",
		Spec: &v1alpha1.HelmInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeHelmInput(map[string]any{
						"path":       "../../testdata/mychart",
						"repository": "example.com/charts/mychart:9.9.9",
					}),
				},
			},
		},
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	assert.Contains(t, err.Error(), "does not match tag")
}

func TestHelmInput_Transform_RepositoryMissingTag(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-missing-tag",
		Spec: &v1alpha1.HelmInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeHelmInput(map[string]any{
						"path":       "../../testdata/mychart",
						"repository": "example.com/charts/mychart",
					}),
				},
			},
		},
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	assert.Contains(t, err.Error(), "tag is required")
}

func TestHelmInput_Transform_MissingSpec(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-no-spec",
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	assert.Contains(t, err.Error(), "spec is required")
}

func TestHelmInput_Transform_MissingResource(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-no-resource",
		Spec: &v1alpha1.HelmInputSpec{},
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	assert.Contains(t, err.Error(), "resource with input is required")
}
