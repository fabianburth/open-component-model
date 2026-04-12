package internal

import (
	"fmt"

	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const LegacyHelmChartConsumerType = "HelmChartRepository"

// ConsumerIdentityFromURL parses a Helm repository URL into a credential
// consumer identity with the correct type set (OCI or legacy HelmChartRepository).
// Returns nil, nil when helmRepositoryURL is empty (local charts need no credentials).
func ConsumerIdentityFromURL(helmRepositoryURL string) (runtime.Identity, error) {
	if helmRepositoryURL == "" {
		return nil, nil
	}

	identity, err := runtime.ParseURLToIdentity(helmRepositoryURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	if scheme, ok := identity[runtime.IdentityAttributeScheme]; ok && scheme == "oci" {
		identity.SetType(ocicredentialsspecv1.Type)
	} else {
		identity.SetType(runtime.NewUnversionedType(LegacyHelmChartConsumerType))
	}

	return identity, nil
}
