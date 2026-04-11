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
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
	"ocm.software/open-component-model/bindings/go/input/utf8/transformation"
	"ocm.software/open-component-model/bindings/go/input/utf8/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&v1alpha1.UTF8Input{}, v1alpha1.UTF8InputV1alpha1)
	return s
}

func makeUTF8Input(opts map[string]any) *runtime.Raw {
	m := map[string]any{
		"type": "utf8/v1",
	}
	for k, v := range opts {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return &runtime.Raw{
		Type: runtime.NewVersionedType("utf8", "v1"),
		Data: data,
	}
}

func TestUTF8Input_Transform_Text(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-text",
		Spec: &v1alpha1.UTF8InputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeUTF8Input(map[string]any{"text": "hello world"}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.UTF8Input)
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

func TestUTF8Input_Transform_JSON(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-json",
		Spec: &v1alpha1.UTF8InputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeUTF8Input(map[string]any{"json": json.RawMessage(`{"name":"test","value":42}`)}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.UTF8Input)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
	r.Equal("application/json", out.Output.File.MediaType)

	blob, err := filesystem.GetBlobFromSpec(ctx, &out.Output.File)
	r.NoError(err)
	rc, err := blob.ReadCloser()
	r.NoError(err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(`{"name":"test","value":42}`, string(data))
}

func TestUTF8Input_Transform_FormattedJSON(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-formatted-json",
		Spec: &v1alpha1.UTF8InputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeUTF8Input(map[string]any{"formattedJson": json.RawMessage(`{"name":"test","value":42}`)}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.UTF8Input)
	r.True(ok)
	r.NotNil(out.Output)
	r.Equal("application/json", out.Output.File.MediaType)

	blob, err := filesystem.GetBlobFromSpec(ctx, &out.Output.File)
	r.NoError(err)
	rc, err := blob.ReadCloser()
	r.NoError(err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	r.NoError(err)

	expected := "{\n  \"name\": \"test\",\n  \"value\": 42\n}"
	r.Equal(expected, string(data))
}

func TestUTF8Input_Transform_YAML(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-yaml",
		Spec: &v1alpha1.UTF8InputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeUTF8Input(map[string]any{"yaml": json.RawMessage(`{"test":"value"}`)}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.UTF8Input)
	r.True(ok)
	r.NotNil(out.Output)
	r.Equal("application/x-yaml", out.Output.File.MediaType)

	blob, err := filesystem.GetBlobFromSpec(ctx, &out.Output.File)
	r.NoError(err)
	rc, err := blob.ReadCloser()
	r.NoError(err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Contains(string(data), "test: value")
}

func TestUTF8Input_Transform_WithCompression(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-compress",
		Spec: &v1alpha1.UTF8InputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeUTF8Input(map[string]any{"text": "compressible data", "compress": true}),
				},
			},
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.UTF8Input)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotEmpty(out.Output.File.URI)
}

func TestUTF8Input_Transform_WithOutputPath(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	outputDir := filepath.Join(t.TempDir(), "output")
	r.NoError(os.MkdirAll(outputDir, 0755))

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-output-path",
		Spec: &v1alpha1.UTF8InputSpec{
			Resource: &constructorv1.Resource{
				AccessOrInput: constructorv1.AccessOrInput{
					Input: makeUTF8Input(map[string]any{"text": "output path test"}),
				},
			},
			OutputPath: outputDir,
		},
	}

	result, err := transformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.UTF8Input)
	r.True(ok)
	r.NotNil(out.Output)
	r.Contains(out.Output.File.URI, "file://")
}

func TestUTF8Input_Transform_MissingSpec(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-no-spec",
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "spec is required")
}

func TestUTF8Input_Transform_MissingResource(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	transformer := &transformation.UTF8Input{Scheme: scheme}

	step := &v1alpha1.UTF8Input{
		Type: v1alpha1.UTF8InputV1alpha1,
		ID:   "test-no-resource",
		Spec: &v1alpha1.UTF8InputSpec{},
	}

	_, err := transformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "resource with input is required")
}
