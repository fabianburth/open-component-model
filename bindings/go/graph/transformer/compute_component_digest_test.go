package transformer_test

import (
	"context"
	"crypto"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/graph/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/graph/transformer"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&v1alpha1.ComputeComponentDigest{}, v1alpha1.ComputeComponentDigestV1alpha1)
	return s
}

func TestComputeComponentDigest_Transform(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	desc := &v2.Descriptor{
		Meta: v2.Meta{Version: "v2"},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "ocm.software/test-component",
					Version: "1.0.0",
				},
			},
			Provider:           "test-provider",
			Resources:          []v2.Resource{},
			Sources:            []v2.Source{},
			References:         []v2.Reference{},
			RepositoryContexts: []*runtime.Raw{},
		},
	}

	xformer := &transformer.ComputeComponentDigest{Scheme: scheme}

	step := &v1alpha1.ComputeComponentDigest{
		Type: v1alpha1.ComputeComponentDigestV1alpha1,
		ID:   "test",
		Spec: &v1alpha1.ComputeComponentDigestSpec{
			Descriptor: desc,
		},
	}

	result, err := xformer.Transform(ctx, step)
	r.NoError(err)

	out, ok := result.(*v1alpha1.ComputeComponentDigest)
	r.True(ok)
	r.NotNil(out.Output)
	r.Equal(crypto.SHA256.String(), out.Output.Digest.HashAlgorithm)
	r.Equal(v4alpha1.Algorithm, out.Output.Digest.NormalisationAlgorithm)
	r.NotEmpty(out.Output.Digest.Value)

	// Verify the digest matches what we compute directly.
	runtimeDesc, err := descruntime.ConvertFromV2(desc)
	r.NoError(err)
	normalisedData, err := normalisation.Normalisations.Normalise(runtimeDesc, v4alpha1.Algorithm)
	r.NoError(err)
	expectedValue := digest.SHA256.FromBytes(normalisedData).Encoded()
	r.Equal(expectedValue, out.Output.Digest.Value)
}

func TestComputeComponentDigest_Transform_MissingSpec(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	xformer := &transformer.ComputeComponentDigest{Scheme: scheme}

	step := &v1alpha1.ComputeComponentDigest{
		Type: v1alpha1.ComputeComponentDigestV1alpha1,
		ID:   "test-no-spec",
	}

	_, err := xformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "spec is required")
}

func TestComputeComponentDigest_Transform_MissingDescriptor(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	xformer := &transformer.ComputeComponentDigest{Scheme: scheme}

	step := &v1alpha1.ComputeComponentDigest{
		Type: v1alpha1.ComputeComponentDigestV1alpha1,
		ID:   "test-no-desc",
		Spec: &v1alpha1.ComputeComponentDigestSpec{},
	}

	_, err := xformer.Transform(ctx, step)
	r.Error(err)
	r.Contains(err.Error(), "descriptor is required")
}

func TestComputeComponentDigest_Transform_Deterministic(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	scheme := newScheme()

	desc := &v2.Descriptor{
		Meta: v2.Meta{Version: "v2"},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "ocm.software/deterministic",
					Version: "2.0.0",
				},
			},
			Provider:           "provider",
			Resources:          []v2.Resource{},
			Sources:            []v2.Source{},
			References:         []v2.Reference{},
			RepositoryContexts: []*runtime.Raw{},
		},
	}

	xformer := &transformer.ComputeComponentDigest{Scheme: scheme}

	step := &v1alpha1.ComputeComponentDigest{
		Type: v1alpha1.ComputeComponentDigestV1alpha1,
		ID:   "test-deterministic",
		Spec: &v1alpha1.ComputeComponentDigestSpec{
			Descriptor: desc,
		},
	}

	result1, err := xformer.Transform(ctx, step)
	r.NoError(err)
	out1 := result1.(*v1alpha1.ComputeComponentDigest)

	// Reset output for a second run.
	step.Output = nil
	result2, err := xformer.Transform(ctx, step)
	r.NoError(err)
	out2 := result2.(*v1alpha1.ComputeComponentDigest)

	r.Equal(out1.Output.Digest, out2.Output.Digest)
}
