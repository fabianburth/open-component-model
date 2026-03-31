package graph

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
)

// ComponentVersionConflictPolicy defines the policy for handling component version conflicts.
type ComponentVersionConflictPolicy int

const (
	ComponentVersionConflictAbortAndFail ComponentVersionConflictPolicy = iota
	ComponentVersionConflictReplace
	ComponentVersionConflictSkip
)

// ExternalComponentVersionCopyPolicy defines the policy for external component version references.
type ExternalComponentVersionCopyPolicy int

const (
	ExternalComponentVersionCopyPolicySkip ExternalComponentVersionCopyPolicy = iota
	ExternalComponentVersionCopyPolicyCopyOrFail
)

// ExternalComponentRepositoryProvider returns the repository for resolving external components.
type ExternalComponentRepositoryProvider interface {
	GetExternalRepository(ctx context.Context, name, version string) (repository.ComponentVersionRepository, error)
}

// ConstructorOrExternalComponent holds either a constructor component or an external component.
type ConstructorOrExternalComponent struct {
	ConstructorComponent *constructor.Component
	ExternalComponent    *DescriptorWithLocalBlobs
}

// DescriptorWithLocalBlobs pairs an external component descriptor with its fetched local blob resources.
type DescriptorWithLocalBlobs struct {
	*descriptor.Descriptor
	Local []LocalResourceWithContent
}

// LocalResourceWithContent pairs a local resource with its blob content.
type LocalResourceWithContent struct {
	Index    int
	Content  blob.ReadOnlyBlob
	Resource *descriptor.Resource
}
