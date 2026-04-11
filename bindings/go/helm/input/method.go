package input

import (
	"context"
	"fmt"
	"os"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ constructorruntime.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod processes Helm chart input specifications into blobs.
type InputMethod struct {
	TempFolder       string
	WorkingDirectory string
}

// NewInputMethod creates a new InputMethod with the given temp folder.
// If tempFolder is empty, the system default temp directory is used.
func NewInputMethod(tempFolder string) (*InputMethod, error) {
	if tempFolder == "" {
		tempFolder = os.TempDir()
	}
	return &InputMethod{TempFolder: tempFolder}, nil
}

func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	var helm v1.Helm
	if err := Scheme.Convert(resource.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if helm.HelmRepository == "" {
		return nil, nil
	}

	identity, err := runtime.ParseURLToIdentity(helm.HelmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	return identity, nil
}

func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials map[string]string) (*constructorruntime.ResourceInputMethodResult, error) {
	var helm v1.Helm
	if err := Scheme.Convert(resource.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	var opts []Option
	if len(credentials) > 0 {
		opts = append(opts, WithCredentials(credentials))
	}
	if i.WorkingDirectory != "" {
		opts = append(opts, WithWorkingDirectory(i.WorkingDirectory))
	}

	helmBlob, chart, err := GetV1HelmBlob(ctx, helm, i.TempFolder, opts...)
	if err != nil {
		return nil, fmt.Errorf("error getting helm blob based on resource input specification: %w", err)
	}

	// If Repository is set, create a resource access pointing to the remote helm chart.
	if helm.Repository != "" {
		processedResource, err := createRemoteHelmResource(chart, helm.Repository)
		if err != nil {
			return nil, fmt.Errorf("error creating remote helm resource access: %w", err)
		}
		return &constructorruntime.ResourceInputMethodResult{
			ProcessedResource: processedResource,
			ProcessedBlobData: helmBlob,
		}, nil
	}

	return &constructorruntime.ResourceInputMethodResult{ProcessedBlobData: helmBlob}, nil
}

// createRemoteHelmResource creates a descriptor.Resource with OCI access for a helm chart stored in a remote repository.
func createRemoteHelmResource(chart *ReadOnlyChart, repository string) (*descriptor.Resource, error) {
	ref, err := looseref.ParseReference(repository)
	if err != nil {
		return nil, fmt.Errorf("failed to parse target access image reference %q: %w", repository, err)
	}

	if ref.Tag == "" {
		return nil, fmt.Errorf("tag is required for remote helm repository")
	}

	if ref.Tag != chart.Version {
		return nil, fmt.Errorf("provided version %q does not match tag %q", ref.Tag, chart.Version)
	}

	ociAccess := &ocispec.OCIImage{
		ImageReference: ref.String(),
	}

	if _, err := access.Scheme.DefaultType(ociAccess); err != nil {
		return nil, fmt.Errorf("error setting default type for OCIImage: %w", err)
	}

	var rawAccess runtime.Raw
	if err := access.Scheme.Convert(ociAccess, &rawAccess); err != nil {
		return nil, fmt.Errorf("error converting OCIImage access to raw: %w", err)
	}

	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    chart.Name,
				Version: chart.Version,
			},
		},
		Type:     HelmRepositoryType,
		Relation: descriptor.ExternalRelation,
		Access:   &rawAccess,
	}, nil
}
