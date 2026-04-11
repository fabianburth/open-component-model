package transformation

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AddLocalSource is a transformer that adds local source blobs to component versions
// in OCI and CTF repositories. It downloads the blob from the source's access spec
// and uploads it as a local source to the target repository.
type AddLocalSource struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *AddLocalSource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating add local source transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to add local source transformation: %w", err)
	}

	var repoSpec runtime.Typed
	var contentSpec blobv1alpha1.File
	var component, version string
	var source *v2.Source
	var output any

	switch tr := transformation.(type) {
	case *v1alpha1.OCIAddLocalSource:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		source = tr.Spec.Source
		contentSpec = tr.Spec.File
		if tr.Output == nil {
			tr.Output = &v1alpha1.OCIAddLocalSourceOutput{}
		}
		output = tr.Output
	case *v1alpha1.CTFAddLocalSource:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		source = tr.Spec.Source
		contentSpec = tr.Spec.File
		if tr.Output == nil {
			tr.Output = &v1alpha1.CTFAddLocalSourceOutput{}
		}
		output = tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	// Validate inputs
	if component == "" {
		return nil, fmt.Errorf("component name is required")
	}
	if version == "" {
		return nil, fmt.Errorf("component version is required")
	}
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if contentSpec.URI == "" {
		return nil, fmt.Errorf("file URI is required to access the source data to be uploaded")
	}

	// Resolve credentials if provider available
	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	// Get repository
	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, repoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}

	// Convert v2.Source to runtime.Source
	runtimeSources := descriptor.ConvertFromV2Sources([]v2.Source{*source})
	if len(runtimeSources) == 0 {
		return nil, fmt.Errorf("failed converting source from v2 format")
	}
	runtimeSource := &runtimeSources[0]

	// Get blob from file spec
	content, err := filesystem.GetBlobFromSpec(ctx, &contentSpec)
	if err != nil {
		return nil, fmt.Errorf("failed getting blob from file spec: %w", err)
	}

	// Add local source - this will update the access spec with LocalBlob
	updatedSource, err := repo.AddLocalSource(ctx, component, version, runtimeSource, content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local source %q to component %s:%s: %w",
			runtimeSource.Name, component, version, err)
	}

	v2UpdatedSources, err := descriptor.ConvertToV2Sources(oci.DefaultRepositoryScheme, []descriptor.Source{*updatedSource})
	if err != nil {
		return nil, fmt.Errorf("failed converting updated source to v2 format: %w", err)
	}
	if len(v2UpdatedSources) == 0 {
		return nil, fmt.Errorf("failed converting updated source to v2 format: empty result")
	}
	v2UpdatedSource := &v2UpdatedSources[0]

	// Populate output based on type
	switch out := output.(type) {
	case *v1alpha1.OCIAddLocalSourceOutput:
		out.Source = v2UpdatedSource
	case *v1alpha1.CTFAddLocalSourceOutput:
		out.Source = v2UpdatedSource
	default:
		return nil, fmt.Errorf("unexpected output type: %T", output)
	}

	return transformation, nil
}
