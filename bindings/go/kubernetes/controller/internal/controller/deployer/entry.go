package deployer

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	deliveryv1alpha1 "ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// entry is one OCM resource the deployer applies, read from a Resource or from a Discovery.
type entry struct {
	// owner is the object the entry is read from. It names the app labels of the entry's objects.
	owner client.Object
	// component and resource identify the entry on its objects.
	component *deliveryv1alpha1.ComponentInfo
	resource  *deliveryv1alpha1.ResourceInfo
	// identity selects the resource in the component descriptor.
	identity ocmruntime.Identity
	// componentDescriptor fetches the entry's component descriptor and checks its integrity.
	componentDescriptor func(ctx context.Context, cfg *configuration.Configuration, pm *manager.PluginManager) (*descriptor.Descriptor, error)
	// describe names the entry in errors. Empty for the single entry of a Resource.
	describe string
}

// wrap names the entry in err, so a failure among many entries can be told apart.
func (e *entry) wrap(err error) error {
	if e.describe == "" {
		return err
	}

	return fmt.Errorf("%s: %w", e.describe, err)
}

// deployable is an entry with the objects decoded from its resource.
type deployable struct {
	entry               *entry
	componentDescriptor *descriptor.Descriptor
	resource            *descriptor.Resource
	objs                []*unstructured.Unstructured
}

// configure loads the effective OCM configuration and the plugin manager for it.
func (r *Reconciler) configure(
	ctx context.Context,
	deployer deliveryv1alpha1.DeployerObject,
	parent deliveryv1alpha1.ConfigRefProvider,
) (*configuration.Configuration, *manager.PluginManager, error) {
	cfg, err := r.resolveConfiguration(ctx, deployer, parent)
	if err != nil {
		return nil, nil, err
	}

	pm, err := r.PluginManagerFor(ctx, cfg)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetConfigurationFailedReason, err.Error())

		return nil, nil, fmt.Errorf("failed to create plugin manager: %w", err)
	}

	return cfg, pm, nil
}

// loadEntry resolves the entry's component descriptor, downloads the entry's resource and decodes its objects.
func (r *Reconciler) loadEntry(
	ctx context.Context,
	deployer deliveryv1alpha1.DeployerObject,
	e *entry,
	cfg *configuration.Configuration,
	pm *manager.PluginManager,
) (deployable, error) {
	repoResolver, err := r.createRepositoryResolver(ctx, deployer, e, cfg, pm)
	if err != nil {
		return deployable{}, err
	}

	componentDescriptor, matchedResource, err := r.resolveComponentAndMatchResource(ctx, deployer, e, cfg, pm)
	if err != nil {
		return deployable{}, err
	}

	key := buildResourceCacheKey(matchedResource, componentDescriptor, cfg, e.identity.String())

	objs, err := r.DownloadCache.Load(key, func() ([]*unstructured.Unstructured, error) {
		return r.DownloadResourceWithOCM(ctx, repoResolver, componentDescriptor, matchedResource, cfg, pm)
	})
	if err != nil {
		err = e.wrap(err)
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return deployable{}, fmt.Errorf("failed to download resource from OCM or retrieve it from the cache: %w", err)
	}

	return deployable{entry: e, componentDescriptor: componentDescriptor, resource: matchedResource, objs: objs}, nil
}

// deploy applies the objects of all deployables as one ApplySet, which prunes what is no longer part of it, and
// tracks them.
func (r *Reconciler) deploy(ctx context.Context, deployer deliveryv1alpha1.DeployerObject, deployables []deployable) error {
	if err := r.applyWithApplySet(ctx, deployer, deployables); err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ApplyFailed, err.Error())

		return fmt.Errorf("failed to apply resources: %w", err)
	}

	var objs []*unstructured.Unstructured
	for _, d := range deployables {
		objs = append(objs, d.objs...)
	}

	// Track the applied objects for the dynamic informer manager
	if err := r.trackConcurrently(ctx, deployer, objs); err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceNotSynced, err.Error())

		return fmt.Errorf("failed to sync deployed resources: %w", err)
	}

	updateDeployedObjectStatusReferences(objs, deployer)

	return nil
}
