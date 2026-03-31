package constructor

import (
	"ocm.software/open-component-model/bindings/go/constructor/internal/graph"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
)

// NewDefaultBuilder creates a [builder.Builder] pre-configured with all transformers
// needed to execute a constructor transformation graph.
//
// This includes OCI/CTF component version operations, file/utf8/dir/helm input
// transformers, and the component digest computation transformer.
func NewDefaultBuilder(
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialProvider credentials.Resolver,
) *builder.Builder {
	return graph.NewDefaultBuilder(repoProvider, credentialProvider)
}
