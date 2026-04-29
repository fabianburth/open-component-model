package transformation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	"ocm.software/open-component-model/bindings/go/helm/input/transformation/spec/v1alpha1"
	helmtransformation "ocm.software/open-component-model/bindings/go/helm/transformation"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// HelmInput is a transformer that processes a Helm chart from a local filesystem
// path or a remote Helm repository and buffers it to an output file for downstream consumption.
type HelmInput struct {
	Scheme             *runtime.Scheme
	CredentialProvider credentials.Resolver
}

func (t *HelmInput) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.HelmInput
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to helm input transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for helm input transformation")
	}

	spec := transformation.Spec

	if spec.Resource == nil || spec.Resource.Input == nil {
		return nil, fmt.Errorf("resource with input is required for helm input transformation")
	}

	// Create a temporary directory for helm processing
	tmpDir, err := os.MkdirTemp("", "helm-input-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary directory for helm input: %w", err)
	}

	method, err := helminput.NewInputMethod(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("failed creating helm input method: %w", err)
	}
	method.WorkingDirectory = spec.WorkingDirectory

	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: spec.Resource.Input,
		},
	}

	// Resolve credentials if credential provider is available
	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := method.GetResourceCredentialConsumerIdentity(ctx, resource); err != nil {
			return nil, fmt.Errorf("failed getting resource consumer identity for credential resolution: %w", err)
		} else if consumerId != nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	result, err := method.ProcessResource(ctx, resource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed processing helm input: %w", err)
	}

	// Determine output path
	outputPath, err := helmtransformation.DetermineOutputPath(spec.OutputPath, "helm-input")
	if err != nil {
		return nil, fmt.Errorf("failed determining output path: %w", err)
	}

	// Buffer blob to file spec
	fileSpec, err := filesystem.BlobToSpec(result.ProcessedBlobData, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering blob to file: %w", err)
	}

	// Populate output
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.HelmInputOutput{}
	}
	transformation.Output.File = *fileSpec

	// If the InputMethod returned a processed resource (remote chart with OCI access), use it.
	// Otherwise, convert the constructor v1 resource to v2.
	if result.ProcessedResource != nil {
		transformation.Output.Resource = convertDescriptorResourceToV2(result.ProcessedResource)
	} else {
		transformation.Output.Resource = constructorv1.ConvertResourceToV2(spec.Resource)
	}

	return &transformation, nil
}

// convertDescriptorResourceToV2 converts a descriptor.Resource to a v2.Resource.
func convertDescriptorResourceToV2(res *descriptor.Resource) *v2.Resource {
	if res == nil {
		return nil
	}
	v2Res := &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{
				Name:    res.Name,
				Version: res.Version,
			},
			ExtraIdentity: res.ExtraIdentity.DeepCopy(),
		},
		Type:     res.Type,
		Relation: v2.ResourceRelation(res.Relation),
	}
	if res.Access != nil {
		data, err := json.Marshal(res.Access)
		if err == nil {
			v2Res.Access = &runtime.Raw{Data: data}
		}
	}
	return v2Res
}
