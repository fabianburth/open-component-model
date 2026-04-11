package transformer

import (
	"context"
	"crypto"
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/graph/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComputeComponentDigest is a transformer that computes the normalised digest
// of a component descriptor. The resulting digest can be used when a component
// references another component and needs the referenced component's digest.
type ComputeComponentDigest struct {
	Scheme *runtime.Scheme
}

func (t *ComputeComponentDigest) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.ComputeComponentDigest
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to compute component digest transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for compute component digest transformation")
	}
	if transformation.Spec.Descriptor == nil {
		return nil, fmt.Errorf("descriptor is required for compute component digest transformation")
	}

	componentDigest, err := calculateDigest(transformation.Spec.Descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate component digest: %w", err)
	}

	// Verify against expected digest if specified
	if expected := transformation.Spec.ExpectedDigest; expected != nil {
		if componentDigest.Value != expected.Value ||
			componentDigest.HashAlgorithm != expected.HashAlgorithm ||
			componentDigest.NormalisationAlgorithm != expected.NormalisationAlgorithm {
			return nil, fmt.Errorf("digest mismatch for component %s: expected %s/%s/%s, got %s/%s/%s",
				transformation.Spec.Descriptor.Component.Name,
				expected.HashAlgorithm, expected.NormalisationAlgorithm, expected.Value,
				componentDigest.HashAlgorithm, componentDigest.NormalisationAlgorithm, componentDigest.Value,
			)
		}
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.ComputeComponentDigestOutput{}
	}
	transformation.Output.Digest = *componentDigest

	return &transformation, nil
}

// calculateDigest converts a v2.Descriptor to a runtime descriptor, normalises
// it using the v4alpha1 JSON normalisation algorithm, and computes a SHA-256
// hash of the normalised bytes.
func calculateDigest(desc *v2.Descriptor) (*v2.Digest, error) {
	runtimeDesc, err := descruntime.ConvertFromV2(desc)
	if err != nil {
		return nil, fmt.Errorf("failed to convert v2 descriptor to runtime descriptor: %w", err)
	}

	normalisedData, err := normalisation.Normalisations.Normalise(runtimeDesc, v4alpha1.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("error normalising descriptor %s: %w", desc.Component.ToIdentity().String(), err)
	}

	return &v2.Digest{
		HashAlgorithm:          crypto.SHA256.String(),
		NormalisationAlgorithm: v4alpha1.Algorithm,
		Value:                  digest.SHA256.FromBytes(normalisedData).Encoded(),
	}, nil
}
