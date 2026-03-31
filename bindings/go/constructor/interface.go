package constructor

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceInputMethodResult is the return value of a ResourceInputMethod.
// It MUST contain one of
// - ProcessedResource: The processed resource with the access type set to the resulting access type
// - ProcessedBlobData: The local blob data that is expected to be uploaded when uploading the component version
//
// If the input method does not support the given input specification it MAY reject the request,
// but if a ResourceInputMethodResult is returned, it MUST at least contain one of the above.
//
// If the ResourceInputMethodResult.ProcessedResource is set, the access type of the resource MUST be set to the resulting access type
// If the ResourceInputMethodResult.ProcessedBlobData is set, the access type of the blob must be uploaded as a local resource
// with the relation `local`, the media type derived from blob.MediaTypeAware, and the resource version defaulted
// to the component version.
type ResourceInputMethodResult struct {
	ProcessedResource *descriptor.Resource
	ProcessedBlobData blob.ReadOnlyBlob
}

// ResourceInputMethod is the interface for processing a resource with an input method declared as per
// [spec.Resource.Input]. Note that spec.Resource's who have their access predefined, are never processed
// with a ResourceInputMethod, but are directly added to the component version repository.
// any spec.Resource passed MUST have its [spec.Resource.Input] field set.
// If the input method does not support the given input specification it MAY reject the request
//
// The method will get called with the raw specification specified in the constructor and is expected
// to return a ResourceInputMethodResult or an error.
//
// A method can be supplied with credentials from any credentials system by requesting a consumer identity with
// ResourceInputMethod.GetCredentialConsumerIdentity. The resulting identity MAY be used to uniquely identify the consuming
// method and to request credentials from the credentials system. The credentials system is not part of this interface and
// is expected to be supplied by the caller of the input method.
//
// The resolved credentials MAY be passed to the input method via the credentials map, but a method MAY
// work without credentials as well.
type ResourceInputMethod interface {
	// ResourceConsumerIdentityProvider that resolves the identity of the given resource to use for credential resolution.
	// These can then be passed to ProcessResource.
	ResourceConsumerIdentityProvider
	ProcessResource(ctx context.Context, resource *constructor.Resource, credentials map[string]string) (result *ResourceInputMethodResult, err error)
}

// SourceInputMethodResult is the return value of a SourceInputMethod.
// It MUST contain one of
// - ProcessedSource: The processed source with the access type set to the resulting access type
// - ProcessedBlobData: The local blob data that is expected to be uploaded when uploading the component version
//
// If the input method does not support the given input specification it MAY reject the request,
// but if a SourceInputMethodResult is returned, it MUST at least contain one of the above.
//
// If the ProcessedSource.ProcessedSource is set, the access type of the source MUST be set to the resulting access type
// If the ProcessedSource.ProcessedBlobData is set, the access type of the blob must be uploaded as a local resource
// with the relation `local`, the media type derived from blob.MediaTypeAware, and the resource version defaulted
// to the component version.
type SourceInputMethodResult struct {
	ProcessedSource   *descriptor.Source
	ProcessedBlobData blob.ReadOnlyBlob
}

// SourceInputMethod is the interface for processing a source with an input method declared as per
// [spec.Source.Input]. Note that spec.Source's who have their access predefined, are never processed
// with a ResourceInputMethod, but are directly added to the component version repository.
// any spec.Source passed MUST have its [spec.Source.Input] field set.
// If the input method does not support the given input specification it MAY reject the request
//
// The method will get called with the raw specification specified in the constructor and is expected
// to return a SourceInputMethodResult or an error.
//
// A method can be supplied with credentials from any credentials system by requesting a consumer identity with
// SourceInputMethod.GetCredentialConsumerIdentity. The resulting identity MAY be used to uniquely identify the consuming
// method and to request credentials from the credentials system. The credentials system is not part of this interface and
// is expected to be supplied by the caller of the input method.
//
// The resolved credentials MAY be passed to the input method via the credentials map, but a method MAY
// work without credentials as well.
type SourceInputMethod interface {
	// SourceConsumerIdentityProvider that resolves the identity of the given source to use for credential resolution.
	// These can then be passed to ProcessSource.
	SourceConsumerIdentityProvider
	ProcessSource(ctx context.Context, source *constructor.Source, credentials map[string]string) (result *SourceInputMethodResult, err error)
}

type ResourceDigestProcessor interface {
	// GetResourceDigestProcessorCredentialConsumerIdentity resolves the identity of the given resource to use for credential resolution
	// for the digest processor. The identity returned MAY be used to resolve credentials for the digest processor.
	// Note that this is not the same as ResourceConsumerIdentityProvider, because it uses the descriptor resource,
	// and not the constructor resource.
	GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (identity runtime.Identity, err error)
	// ProcessResourceDigest processes the given resource and returns a new resource with the digest information set.
	// The resource returned MUST have its digest information filled appropriately or the method MUST return an error.
	// The resource passed MUST have an access set that can be used to interpret the resource and provide the digest.
	ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error)
}

type ExternalComponentRepositoryProvider interface {
	// GetExternalRepository returns the target ocm repository for the given component specification in the constructor.
	GetExternalRepository(ctx context.Context, name, version string) (repository.ComponentVersionRepository, error)
}

type ResourceConsumerIdentityProvider interface {
	// GetResourceCredentialConsumerIdentity resolves the identity of the given [constructor.Resource] to use for credential resolution.
	GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructor.Resource) (identity runtime.Identity, err error)
}

type SourceConsumerIdentityProvider interface {
	// GetSourceCredentialConsumerIdentity resolves the identity of the given [constructor.Source] to use for credential resolution.
	GetSourceCredentialConsumerIdentity(ctx context.Context, source *constructor.Source) (identity runtime.Identity, err error)
}
