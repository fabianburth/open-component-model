package constructor

import (
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
)

// Type aliases for backward compatibility.
// The canonical definitions now live in the constructor/runtime module.

type ResourceInputMethodResult = constructor.ResourceInputMethodResult

type ResourceInputMethod = constructor.ResourceInputMethod

type SourceInputMethodResult = constructor.SourceInputMethodResult

type SourceInputMethod = constructor.SourceInputMethod

// ResourceDigestProcessor is an alias for repository.ResourceDigestProcessor.
// Deprecated: Use repository.ResourceDigestProcessor directly.
type ResourceDigestProcessor = repository.ResourceDigestProcessor

type ExternalComponentRepositoryProvider = constructor.ExternalComponentRepositoryProvider

type ResourceConsumerIdentityProvider = constructor.ResourceConsumerIdentityProvider

type SourceConsumerIdentityProvider = constructor.SourceConsumerIdentityProvider
