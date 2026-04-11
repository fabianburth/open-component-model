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
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v2"
	"ocm.software/open-component-model/bindings/go/input/dir/transformation"
	"ocm.software/open-component-model/bindings/go/input/dir/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&v1alpha1.DirInput{}, v1alpha1.DirInputV1alpha1)
	return s
}

func makeDirInput(path string, opts map[string]any) *runtime.Raw {
	m := map[string]any{
		"type": "dir/v1",
		"path": path,
	}
	for k, v := range opts {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return &runtime.Raw{
		Type: runtime.NewVersionedType("dir", "v1"),
		Data: data,
	}
}

func TestDirInput_Transform(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "source")
	r.NoError(os.MkdirAll(srcDir, 0755))
	r.NoError(os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0644))
	r.NoError(os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("some data"), 0644))

	transformer := &transformation.DirInput{Scheme: scheme}

	step := &v1alpha1.DirInput{
		Type: v1alpha1.DirInputV1alpha1,
		ID:   "test",
		Spec: &v1alpha1.DirInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeDirInput(srcDir, nil),
				},
			},
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.DirInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)

	// Verify the buffered file content is a valid blob
	blob, err := filesystem.GetBlobFromSpec(ctx, &out.Output.File)
	r.NoError(err)
	rc, err := blob.ReadCloser()
	r.NoError(err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.NotEmpty(data)
}

func TestDirInput_Transform_WithCompression(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "source")
	r.NoError(os.MkdirAll(srcDir, 0755))
	r.NoError(os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("compressible data"), 0644))

	transformer := &transformation.DirInput{Scheme: scheme}

	step := &v1alpha1.DirInput{
		Type: v1alpha1.DirInputV1alpha1,
		ID:   "test-compress",
		Spec: &v1alpha1.DirInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeDirInput(srcDir, map[string]any{"compress": true}),
				},
			},
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.DirInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
}

func TestDirInput_Transform_WithOutputPath(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "source")
	r.NoError(os.MkdirAll(srcDir, 0755))
	r.NoError(os.WriteFile(filepath.Join(srcDir, "source.txt"), []byte("file content"), 0644))

	outputDir := filepath.Join(tempDir, "output")
	r.NoError(os.MkdirAll(outputDir, 0755))

	transformer := &transformation.DirInput{Scheme: scheme}

	step := &v1alpha1.DirInput{
		Type: v1alpha1.DirInputV1alpha1,
		ID:   "test-output-path",
		Spec: &v1alpha1.DirInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeDirInput(srcDir, nil),
				},
			},
			OutputPath:       outputDir,
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.DirInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.Contains(out.Output.File.URI, "file://")
}

func TestDirInput_Transform_WithExcludeFiles(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "source")
	r.NoError(os.MkdirAll(srcDir, 0755))
	r.NoError(os.WriteFile(filepath.Join(srcDir, "keep.txt"), []byte("keep"), 0644))
	r.NoError(os.WriteFile(filepath.Join(srcDir, "exclude.log"), []byte("exclude"), 0644))

	transformer := &transformation.DirInput{Scheme: scheme}

	step := &v1alpha1.DirInput{
		Type: v1alpha1.DirInputV1alpha1,
		ID:   "test-exclude",
		Spec: &v1alpha1.DirInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeDirInput(srcDir, map[string]any{"excludeFiles": []string{"*.log"}}),
				},
			},
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.DirInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
}

func TestDirInput_Transform_WorkingDirectory(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "relative-dir")
	r.NoError(os.MkdirAll(srcDir, 0755))
	r.NoError(os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("relative dir file"), 0644))

	transformer := &transformation.DirInput{Scheme: scheme}

	step := &v1alpha1.DirInput{
		Type: v1alpha1.DirInputV1alpha1,
		ID:   "test-workdir",
		Spec: &v1alpha1.DirInputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeDirInput("relative-dir", nil),
				},
			},
			WorkingDirectory: tempDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.DirInput)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
}

func TestDirInput_Transform_MissingSpec(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.DirInput{Scheme: scheme}

	step := &v1alpha1.DirInput{
		Type: v1alpha1.DirInputV1alpha1,
		ID:   "test-no-spec",
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "spec is required")
}

func TestDirInput_Transform_MissingResource(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.DirInput{Scheme: scheme}

	step := &v1alpha1.DirInput{
		Type: v1alpha1.DirInputV1alpha1,
		ID:   "test-no-resource",
		Spec: &v1alpha1.DirInputSpec{},
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "resource with input is required")
}
