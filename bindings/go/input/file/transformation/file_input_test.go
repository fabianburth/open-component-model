package transformation_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/input/file/transformation"
	"ocm.software/open-component-model/bindings/go/input/file/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&v1alpha1.FileInput{}, v1alpha1.FileInputV1alpha1)
	return s
}

func makeFileInput(path, mediaType string, compress bool) *runtime.Raw {
	data, _ := json.Marshal(map[string]any{
		"type":      "file/v1",
		"path":      path,
		"mediaType": mediaType,
		"compress":  compress,
	})
	return &runtime.Raw{
		Type: runtime.NewVersionedType("file", "v1"),
		Data: data,
	}
}

func TestFileInput_Transform(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "hello.txt")
	r.NoError(os.WriteFile(testFile, []byte("hello world"), 0644))

	transformer := &transformation.FileInput{Scheme: scheme}

	step := &v1alpha1.FileInput{
		Type: v1alpha1.FileInputV1alpha1,
		ID:   "test",
		Spec: &v1alpha1.FileInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeFileInput(testFile, "text/plain", false),
				},
			},
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.FileInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
	r.Equal("text/plain", out.Output.File.MediaType)

	// Verify the buffered file content
	blob, err := filesystem.GetBlobFromSpec(ctx, &out.Output.File)
	r.NoError(err)
	rc, err := blob.ReadCloser()
	r.NoError(err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal("hello world", string(data))
}

func TestFileInput_Transform_WithCompression(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "data.txt")
	r.NoError(os.WriteFile(testFile, []byte("compressible data"), 0644))

	transformer := &transformation.FileInput{Scheme: scheme}

	step := &v1alpha1.FileInput{
		Type: v1alpha1.FileInputV1alpha1,
		ID:   "test-compress",
		Spec: &v1alpha1.FileInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeFileInput(testFile, "", true),
				},
			},
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.FileInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
}

func TestFileInput_Transform_WithOutputPath(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "source.txt")
	r.NoError(os.WriteFile(testFile, []byte("file content"), 0644))

	outputDir := filepath.Join(tempDir, "output")
	r.NoError(os.MkdirAll(outputDir, 0755))

	transformer := &transformation.FileInput{Scheme: scheme}

	step := &v1alpha1.FileInput{
		Type: v1alpha1.FileInputV1alpha1,
		ID:   "test-output-path",
		Spec: &v1alpha1.FileInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeFileInput(testFile, "", false),
				},
			},
			OutputPath:       outputDir,
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.FileInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.Contains(out.Output.File.URI, "file://")
}

func TestFileInput_Transform_WorkingDirectory(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "relative.txt")
	r.NoError(os.WriteFile(testFile, []byte("relative file"), 0644))

	transformer := &transformation.FileInput{Scheme: scheme}

	step := &v1alpha1.FileInput{
		Type: v1alpha1.FileInputV1alpha1,
		ID:   "test-workdir",
		Spec: &v1alpha1.FileInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeFileInput("relative.txt", "", false),
				},
			},
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.FileInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
}

func TestFileInput_Transform_MissingSpec(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.FileInput{Scheme: scheme}

	step := &v1alpha1.FileInput{
		Type: v1alpha1.FileInputV1alpha1,
		ID:   "test-no-spec",
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "spec is required")
}

func TestFileInput_Transform_MissingResource(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.FileInput{Scheme: scheme}

	step := &v1alpha1.FileInput{
		Type: v1alpha1.FileInputV1alpha1,
		ID:   "test-no-resource",
		Spec: &v1alpha1.FileInputSpec{},
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "resource with input is required")
}
