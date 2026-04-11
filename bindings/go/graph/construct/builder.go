package construct

import (
	graph "ocm.software/open-component-model/bindings/go/graph/construct/internal"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
)

// NewDefaultBuilder creates a [builder.Builder] pre-configured with all transformers
// needed to execute a constructor transformation graph.
//
// This includes OCI/CTF component version operations, file/utf8/dir/helm input
// transformers, the component digest computation transformer, and optionally
// resource digest processing (when digestProcessor is non-nil).
func NewDefaultBuilder(
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialProvider credentials.Resolver,
	digestProcessor repository.ResourceDigestProcessor,
) *builder.Builder {
	return graph.NewDefaultBuilder(repoProvider, credentialProvider, digestProcessor)
}
