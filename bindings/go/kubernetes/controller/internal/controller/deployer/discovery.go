package deployer

import (
	"context"
	"errors"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	deliveryv1alpha1 "ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries,verbs=get;list;watch

// reconcileDiscoveryDeployment applies every resource the referenced Discovery publishes as one ApplySet. All
// resources are downloaded and decoded before anything is applied, so one that fails leaves the deployed objects as
// they are.
func (r *Reconciler) reconcileDiscoveryDeployment(ctx context.Context, deployer deliveryv1alpha1.DeployerObject) (ctrl.Result, error) {
	discovery, err := r.resolveDiscovery(ctx, deployer)
	if discovery == nil || err != nil {
		return ctrl.Result{}, err
	}

	cfg, pm, err := r.configure(ctx, deployer, discovery)
	if err != nil {
		return ctrl.Result{}, err
	}

	deployables := make([]deployable, 0, len(discovery.Status.Resources))
	for i := range discovery.Status.Resources {
		d, err := r.loadEntry(ctx, deployer, r.discoveryEntry(discovery, &discovery.Status.Resources[i]), cfg, pm)
		if err != nil {
			return ctrl.Result{}, err
		}
		deployables = append(deployables, d)
	}

	if err := r.deploy(ctx, deployer, deployables); err != nil {
		return ctrl.Result{}, err
	}

	status.MarkReady(r.EventRecorder, deployer, "Applied %d resources from discovery %s", len(deployables), discovery.GetName())

	return ctrl.Result{}, nil
}

// resolveDiscovery fetches the Discovery referenced by the Deployer and validates that it is ready and has published
// its resources.
func (r *Reconciler) resolveDiscovery(
	ctx context.Context,
	deployer deliveryv1alpha1.DeployerObject,
) (*deliveryv1alpha1.Discovery, error) {
	discovery, err := util.GetReadyObject[deliveryv1alpha1.Discovery, *deliveryv1alpha1.Discovery](ctx, r.Client, discoveryKey(deployer))
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, err.Error())

		// Return the error so that the deployer is retried with backoff, see resolveResource.
		if _, ok := errors.AsType[util.NotReadyError](err); ok {
			log.FromContext(ctx).Info("discovery is not available", "error", err)

			return nil, err
		}
		if _, ok := errors.AsType[util.DeletionError](err); ok {
			log.FromContext(ctx).Info("discovery is not available", "error", err)

			return nil, err
		}

		return nil, fmt.Errorf("failed to get ready discovery: %w", err)
	}

	// An absent list is not computed yet, an empty one means there is nothing to deploy.
	if discovery.Status.Resources == nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, "discovery has not published its resources")

		return nil, errors.New("failed to get ready discovery: discovery has not published its resources")
	}

	return discovery, nil
}

// discoveryKey returns the referenced Discovery. An empty namespace defaults to the deployer's namespace.
func discoveryKey(deployer deliveryv1alpha1.DeployerObject) client.ObjectKey {
	ref := deployer.GetDiscoveryRef()
	namespace := ref.Namespace
	if namespace == "" {
		namespace = deployer.GetNamespace()
	}

	return client.ObjectKey{Namespace: namespace, Name: ref.Name}
}

// discoveryEntry turns a resource published by the Discovery into an entry. The component descriptor is fetched
// again and anchored by the digest the Discovery recorded for the component version, so the descriptor the resource
// is downloaded with is the one the Discovery verified.
func (r *Reconciler) discoveryEntry(discovery *deliveryv1alpha1.Discovery, res *deliveryv1alpha1.DiscoveredResource) *entry {
	identity := ocmruntime.Identity{descriptor.IdentityAttributeName: res.Resource.Name}
	if res.Resource.Version != "" {
		identity[descriptor.IdentityAttributeVersion] = res.Resource.Version
	}
	for k, v := range res.Resource.ExtraIdentity {
		identity[k] = v
	}

	return &entry{
		owner:     discovery,
		component: &res.Component,
		resource:  &res.Resource,
		identity:  identity,
		describe: fmt.Sprintf("resource %s of component %s:%s",
			identity.String(), res.Component.Component, res.Component.Version),
		componentDescriptor: func(ctx context.Context, cfg *configuration.Configuration, pm *manager.PluginManager) (*descriptor.Descriptor, error) {
			repoSpec, err := decodeRepositorySpec(res.Component.RepositorySpec)
			if err != nil {
				return nil, fmt.Errorf("failed to decode repository spec: %w", err)
			}

			desc, err := r.Resolver.GetComponentVersion(ctx, &resolution.Options{
				RepositorySpec: repoSpec,
				Configuration:  cfg,
				PluginManager:  pm,
			}, resolution.Verification{Digest: res.Component.Digest}, res.Component.Component, res.Component.Version)
			if err != nil {
				return nil, err
			}

			if res.Component.Digest == nil {
				// Without a digest the descriptor cannot be anchored. Like a component version that is not safely
				// digestible, it is used anyway and reported.
				return desc, fmt.Errorf("%w: no digest recorded to verify the component version against", resolution.ErrNotSafelyDigestible)
			}

			return desc, nil
		},
	}
}
