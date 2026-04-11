package internal

import (
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/signing/transformation/spec/v1alpha1"
	signingtransformation "ocm.software/open-component-model/bindings/go/signing/transformation"
	"ocm.software/open-component-model/bindings/go/credentials"
	helmtransformation "ocm.software/open-component-model/bindings/go/helm/input/transformation"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/input/transformation/spec/v1alpha1"
	dirtransformation "ocm.software/open-component-model/bindings/go/input/dir/transformation"
	dirv1alpha1 "ocm.software/open-component-model/bindings/go/input/dir/transformation/spec/v1alpha1"
	filetransformation "ocm.software/open-component-model/bindings/go/input/file/transformation"
	filev1alpha1 "ocm.software/open-component-model/bindings/go/input/file/transformation/spec/v1alpha1"
	utf8transformation "ocm.software/open-component-model/bindings/go/input/utf8/transformation"
	utf8v1alpha1 "ocm.software/open-component-model/bindings/go/input/utf8/transformation/spec/v1alpha1"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	ocitransformer "ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
)

// NewDefaultBuilder creates a builder.Builder pre-configured with all transformers
// needed to execute a constructor transformation graph.
func NewDefaultBuilder(
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialProvider credentials.Resolver,
	digestProcessor repository.ResourceDigestProcessor,
) *builder.Builder {
	transformerScheme := runtime.NewScheme()
	transformerScheme.MustRegisterScheme(ociv1alpha1.Scheme)
	transformerScheme.MustRegisterScheme(ociaccess.Scheme)
	transformerScheme.MustRegisterScheme(filev1alpha1.Scheme)
	transformerScheme.MustRegisterScheme(utf8v1alpha1.Scheme)
	transformerScheme.MustRegisterScheme(dirv1alpha1.Scheme)
	transformerScheme.MustRegisterScheme(helmv1alpha1.Scheme)
	transformerScheme.MustRegisterScheme(signingv1alpha1.Scheme)

	// OCI/CTF component version transformers
	ociAdd := &ocitransformer.AddComponentVersion{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}

	// Local resource transformers
	ociAddResource := &ocitransformer.AddLocalResource{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}

	// Local source transformers
	ociAddSource := &ocitransformer.AddLocalSource{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}

	// Get local resource (for external component copy)
	ociGetResource := &ocitransformer.GetLocalResource{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}

	// Input transformers
	fileInput := &filetransformation.FileInput{Scheme: transformerScheme}
	utf8Input := &utf8transformation.UTF8Input{Scheme: transformerScheme}
	dirInput := &dirtransformation.DirInput{Scheme: transformerScheme}
	helmInput := &helmtransformation.HelmInput{
		Scheme:             transformerScheme,
		CredentialProvider: credentialProvider,
	}

	// Constructor-specific transformers
	computeDigest := &signingtransformation.ComputeComponentDigest{Scheme: transformerScheme}

	b := builder.NewBuilder(transformerScheme).
		// OCI/CTF component version operations
		WithTransformer(&ociv1alpha1.OCIAddComponentVersion{}, ociAdd).
		WithTransformer(&ociv1alpha1.CTFAddComponentVersion{}, ociAdd).
		// OCI/CTF local resource operations
		WithTransformer(&ociv1alpha1.OCIAddLocalResource{}, ociAddResource).
		WithTransformer(&ociv1alpha1.CTFAddLocalResource{}, ociAddResource).
		WithTransformer(&ociv1alpha1.OCIGetLocalResource{}, ociGetResource).
		WithTransformer(&ociv1alpha1.CTFGetLocalResource{}, ociGetResource).
		// OCI/CTF local source operations
		WithTransformer(&ociv1alpha1.OCIAddLocalSource{}, ociAddSource).
		WithTransformer(&ociv1alpha1.CTFAddLocalSource{}, ociAddSource).
		// Input transformers
		WithTransformer(&filev1alpha1.FileInput{}, fileInput).
		WithTransformer(&utf8v1alpha1.UTF8Input{}, utf8Input).
		WithTransformer(&dirv1alpha1.DirInput{}, dirInput).
		WithTransformer(&helmv1alpha1.HelmInput{}, helmInput).
		// Constructor-specific transformers
		WithTransformer(&signingv1alpha1.ComputeComponentDigest{}, computeDigest)

	// Resource digest processing (optional — only when a processor is provided)
	if digestProcessor != nil {
		processDigest := &ocitransformer.ProcessResourceDigest{
			Scheme:             transformerScheme,
			DigestProcessor:    digestProcessor,
			CredentialProvider: credentialProvider,
		}
		b = b.WithTransformer(&ociv1alpha1.ProcessOCIResourceDigest{}, processDigest)
	}

	return b
}
