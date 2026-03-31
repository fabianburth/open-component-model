package builtin

import (
	"fmt"
	"log/slog"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	helmdigest "ocm.software/open-component-model/bindings/go/helm/digest"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocicredentialplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/credentials/oci"
	ociplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/oci"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/rsa"
)

func Register(manager *manager.PluginManager, filesystemConfig *filesystemv1alpha1.Config, logger *slog.Logger) error {
	if err := ocicredentialplugin.Register(manager.CredentialRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register OCI inbuilt credential plugin: %w", err)
	}

	if err := ociplugin.Register(
		manager.ComponentVersionRepositoryRegistry,
		manager.ResourcePluginRegistry,
		manager.DigestProcessorRegistry,
		manager.BlobTransformerRegistry,
		manager.ComponentListerRegistry,
		filesystemConfig,
		logger,
	); err != nil {
		return fmt.Errorf("could not register OCI inbuilt plugin: %w", err)
	}

	if err := manager.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(
		helmdigest.NewDigestProcessor(filesystemConfig.TempFolder),
	); err != nil {
		return fmt.Errorf("could not register helm digest processor plugin: %w", err)
	}
	if err := rsa.Register(manager.SigningRegistry, filesystemConfig); err != nil {
		return fmt.Errorf("could not register RSA signing plugin: %w", err)
	}

	return nil
}
