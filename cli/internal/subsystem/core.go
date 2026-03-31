package subsystem

import (
	"errors"

	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	"ocm.software/open-component-model/bindings/go/input/dir"
	"ocm.software/open-component-model/bindings/go/input/file"
	"ocm.software/open-component-model/bindings/go/input/utf8"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

// NewRegistryFromPluginManager creates a new subsystem registry populated with
// all core subsystems and registers the plugin manager's schemes with each subsystem.
func NewRegistryFromPluginManager(pm *manager.PluginManager) (*Registry, error) {
	registry := NewRegistry()

	// Create subsystems locally
	ocmRepository := NewSubsystem(
		"ocm-repository",
		"Repositories for storing and managing OCM component versions.",
	)
	ocmRepositoryLister := NewSubsystem(
		"ocm-repository-lister",
		"Listers for listing OCM component repositories. Can be seen as repository of versioned repositories",
	)
	ocmResourceRepository := NewSubsystem(
		"access",
		"Access methods define how OCM resources are accessed and retrieved from their origin.",
	)
	input := NewSubsystem(
		"input",
		"Input methods define how content is sourced and ingested into an OCM component version.",
	)
	credentialRepository := NewSubsystem(
		"credential-repository",
		"Repositories for storing and managing credentials so they can be referenced in the OCM credential graph.",
	)
	signingHandler := NewSubsystem(
		"signing",
		"Signing handlers are responsible for signing and verification of component versions.",
	)

	// Register plugin manager schemes
	if err := errors.Join(
		ocmRepository.Scheme.RegisterScheme(pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryScheme()),
		ocmRepositoryLister.Scheme.RegisterScheme(pm.ComponentListerRegistry.GetComponentVersionRepositoryScheme()),
		ocmResourceRepository.Scheme.RegisterScheme(pm.ResourcePluginRegistry.ResourceScheme()),
		credentialRepository.Scheme.RegisterScheme(pm.CredentialRepositoryRegistry.RepositoryScheme()),
		signingHandler.Scheme.RegisterScheme(pm.SigningRegistry.ResourceScheme()),
	); err != nil {
		return nil, err
	}

	// Register input type schemes directly for documentation/discovery.
	// The actual input processing is done by transformers in the graph builder,
	// but we still want `describe types input` to list available input types.
	if err := errors.Join(
		input.Scheme.RegisterScheme(file.Scheme),
		input.Scheme.RegisterScheme(utf8.Scheme),
		input.Scheme.RegisterScheme(dir.Scheme),
		input.Scheme.RegisterScheme(helminput.Scheme),
	); err != nil {
		return nil, err
	}

	// Register all subsystems
	registry.Register(ocmRepository)
	registry.Register(ocmRepositoryLister)
	registry.Register(ocmResourceRepository)
	registry.Register(input)
	registry.Register(credentialRepository)
	registry.Register(signingHandler)

	return registry, nil
}
