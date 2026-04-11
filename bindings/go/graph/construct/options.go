package construct

import (
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Option configures a constructor operation.
type Option func(*ConstructOptions)

// ConstructOptions holds the configuration for building a transformation graph definition.
type ConstructOptions struct {
	// TargetRepositorySpec is the repository specification for the target.
	// MANDATORY.
	TargetRepositorySpec runtime.Typed

	// WorkingDirectory for resolving relative paths in input specs.
	// OPTIONAL — defaults to "".
	WorkingDirectory string

	// ExternalComponentRepositoryProvider resolves external component references
	// not part of the constructor specification.
	// OPTIONAL.
	ExternalComponentRepositoryProvider constructorruntime.ExternalComponentRepositoryProvider

	// ComponentVersionConflictPolicy determines how to handle conflicts
	// when a component version already exists in the target repository.
	ComponentVersionConflictPolicy ComponentVersionConflictPolicy

	// ExternalComponentVersionCopyPolicy determines how to handle
	// external references to component versions.
	ExternalComponentVersionCopyPolicy ExternalComponentVersionCopyPolicy

	// SkipDigestProcessing skips computation of component digests
	// for referenced components. When true, no ComputeComponentDigest
	// transformation nodes are added to the graph.
	SkipDigestProcessing bool
}

// WithTargetRepository sets the target repository specification. MANDATORY.
func WithTargetRepository(spec runtime.Typed) Option {
	return func(o *ConstructOptions) {
		o.TargetRepositorySpec = spec
	}
}

// WithWorkingDirectory sets the working directory for resolving relative paths.
func WithWorkingDirectory(dir string) Option {
	return func(o *ConstructOptions) {
		o.WorkingDirectory = dir
	}
}

// WithExternalComponentRepository sets the provider for resolving external component references.
func WithExternalComponentRepository(p constructorruntime.ExternalComponentRepositoryProvider) Option {
	return func(o *ConstructOptions) {
		o.ExternalComponentRepositoryProvider = p
	}
}

// WithConflictPolicy sets the component version conflict resolution policy.
func WithConflictPolicy(p ComponentVersionConflictPolicy) Option {
	return func(o *ConstructOptions) {
		o.ComponentVersionConflictPolicy = p
	}
}

// WithExternalCopyPolicy sets the external component version copy policy.
func WithExternalCopyPolicy(p ExternalComponentVersionCopyPolicy) Option {
	return func(o *ConstructOptions) {
		o.ExternalComponentVersionCopyPolicy = p
	}
}

// WithSkipDigestProcessing skips digest computation for referenced components
// when set to true. No ComputeComponentDigest transformation nodes will be added.
func WithSkipDigestProcessing(skip bool) Option {
	return func(o *ConstructOptions) {
		o.SkipDigestProcessing = skip
	}
}

// ComponentVersionConflictPolicy defines the policy for handling component version conflicts
// when interacting with the target repository.
type ComponentVersionConflictPolicy int

const (
	// ComponentVersionConflictAbortAndFail will abort the construction process if a component version already exists.
	ComponentVersionConflictAbortAndFail ComponentVersionConflictPolicy = iota
	// ComponentVersionConflictReplace will replace the existing component version.
	ComponentVersionConflictReplace
	// ComponentVersionConflictSkip will skip the construction if a component version already exists.
	ComponentVersionConflictSkip
)

// ExternalComponentVersionCopyPolicy defines the policy for handling external component version references.
type ExternalComponentVersionCopyPolicy int

const (
	// ExternalComponentVersionCopyPolicySkip will skip the copy of the component version to the target repository.
	ExternalComponentVersionCopyPolicySkip = iota
	// ExternalComponentVersionCopyPolicyCopyOrFail will copy the external component version to the target repository.
	ExternalComponentVersionCopyPolicyCopyOrFail ExternalComponentVersionCopyPolicy = iota
)
