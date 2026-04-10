package transformation

import (
	"context"
	"errors"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	"ocm.software/open-component-model/bindings/go/helm/input/transformation/spec/v1alpha1"
	helmtransformation "ocm.software/open-component-model/bindings/go/helm/transformation"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
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

	// Convert to v1.Helm spec for GetV1HelmBlob
	v1Helm := helmv1.Helm{
		Path:           spec.Path,
		Repository:     spec.Repository,
		HelmRepository: spec.HelmRepository,
		Version:        spec.Version,
		CACert:         spec.CACert,
		CACertFile:     spec.CACertFile,
	}

	// Create a temporary directory for helm processing
	tmpDir, err := os.MkdirTemp("", "helm-input-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary directory for helm input: %w", err)
	}

	// Resolve credentials if credential provider is available and this is a remote chart
	var opts []helminput.Option
	if spec.WorkingDirectory != "" {
		opts = append(opts, helminput.WithWorkingDirectory(spec.WorkingDirectory))
	}
	if t.CredentialProvider != nil && spec.HelmRepository != "" {
		identity, err := runtime.ParseURLToIdentity(spec.HelmRepository)
		if err == nil && identity != nil {
			creds, err := t.CredentialProvider.Resolve(ctx, identity)
			if err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials for helm repository: %w", err)
			}
			if creds != nil {
				opts = append(opts, helminput.WithCredentials(creds))
			}
		}
	}

	helmBlob, chart, err := helminput.GetV1HelmBlob(ctx, v1Helm, tmpDir, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed getting helm blob: %w", err)
	}

	// Determine output path
	outputPath, err := helmtransformation.DetermineOutputPath(spec.OutputPath, "helm-input")
	if err != nil {
		return nil, fmt.Errorf("failed determining output path: %w", err)
	}

	// Buffer blob to file spec
	fileSpec, err := filesystem.BlobToSpec(helmBlob, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering blob to file: %w", err)
	}

	// Populate output
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.HelmInputOutput{}
	}
	transformation.Output.File = *fileSpec
	transformation.Output.Resource = spec.Resource

	// If Repository is set, create a resource access pointing to the remote helm chart
	if spec.Repository != "" {
		resource, err := createRemoteResource(chart, spec.Repository)
		if err != nil {
			return nil, fmt.Errorf("failed creating remote resource access: %w", err)
		}
		transformation.Output.Resource = resource
	}

	return &transformation, nil
}

// createRemoteResource creates a v2.Resource with OCI access for a helm chart stored in a remote repository.
func createRemoteResource(chart *helminput.ReadOnlyChart, repository string) (*v2.Resource, error) {
	ref, err := looseref.ParseReference(repository)
	if err != nil {
		return nil, fmt.Errorf("failed to parse target access image reference %q: %w", repository, err)
	}

	if ref.Tag == "" {
		return nil, fmt.Errorf("tag is required for remote helm repository")
	}

	// Ensure the tag matches the chart version
	if ref.Tag != chart.Version {
		return nil, fmt.Errorf("provided version %q does not match tag %q", ref.Tag, chart.Version)
	}

	ociAccess := &ocispec.OCIImage{
		ImageReference: ref.String(),
	}

	// Set the default type for OCIImage
	if _, err := access.Scheme.DefaultType(ociAccess); err != nil {
		return nil, fmt.Errorf("error setting default type for OCIImage: %w", err)
	}

	// Convert typed access to runtime.Raw for the v2.Resource
	var rawAccess runtime.Raw
	if err := access.Scheme.Convert(ociAccess, &rawAccess); err != nil {
		return nil, fmt.Errorf("error converting OCIImage access to raw: %w", err)
	}

	return &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{
				Name:    chart.Name,
				Version: chart.Version,
			},
		},
		Type:     helminput.HelmRepositoryType,
		Relation: v2.ExternalRelation,
		Access:   &rawAccess,
	}, nil
}
