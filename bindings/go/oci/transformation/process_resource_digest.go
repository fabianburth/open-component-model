package transformation

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ProcessResourceDigest is a transformer that processes the digest of an OCI
// resource. It resolves the manifest digest from the registry, sets
// resource.digest, and pins the imageReference to include the resolved digest.
type ProcessResourceDigest struct {
	Scheme             *runtime.Scheme
	DigestProcessor    repository.ResourceDigestProcessor
	CredentialProvider credentials.Resolver
}

func (t *ProcessResourceDigest) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.ProcessOCIResourceDigest
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to process resource digest transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for process resource digest transformation")
	}

	resource := transformation.Spec.Resource
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}

	targetResource := descriptor.ConvertFromV2Resource(resource)

	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.DigestProcessor.GetResourceDigestProcessorCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	processedResource, err := t.DigestProcessor.ProcessResourceDigest(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed processing resource digest for %v: %w", resource.ToIdentity(), err)
	}

	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, processedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.ProcessOCIResourceDigestOutput{}
	}
	transformation.Output.Resource = v2Resource

	return &transformation, nil
}
