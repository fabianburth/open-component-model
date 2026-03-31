package transformation_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm/input/transformation"
	"ocm.software/open-component-model/bindings/go/helm/input/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&v1alpha1.HelmInput{}, v1alpha1.HelmInputV1alpha1)
	return s
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
			Path: "../../testdata/mychart",
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.HelmInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
	// Local charts should not have a Resource set
	r.Nil(out.Output.Resource)

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
			Path: "../../testdata/provenance/mychart-0.1.0.tgz",
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.HelmInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
	r.Nil(out.Output.Resource)

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
			Path:       "../../testdata/mychart",
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
			Path:       "../../testdata/mychart",
			Repository: "example.com/charts/mychart:0.1.0",
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
			Path:       "../../testdata/mychart",
			Repository: "example.com/charts/mychart:9.9.9",
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
			Path:       "../../testdata/mychart",
			Repository: "example.com/charts/mychart",
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

func TestHelmInput_Transform_NeitherPathNorRepo(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.HelmInput{Scheme: scheme}

	step := &v1alpha1.HelmInput{
		Type: v1alpha1.HelmInputV1alpha1,
		ID:   "test-no-path-no-repo",
		Spec: &v1alpha1.HelmInputSpec{},
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	assert.Contains(t, err.Error(), "either path or helmRepository must be specified")
}
