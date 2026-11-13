package deployer

import (
	"context"
	"errors"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	deliveryv1alpha1 "ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries,verbs=get;list;watch

// reconcileDiscoveryDeployment applies every resource the referenced Discovery publishes as one ApplySet. All
// resources are downloaded and decoded before anything is applied, so one that fails leaves the deployed objects as
// they are.
//
// The Discovery status only says which resources to apply. Where they come from is established as for a Resource:
// the root component version of the Discovery is fetched from its Component and its signature verified, and every
// entry's component version is resolved from that root through the entry's reference path, each step anchored by
// the digest its reference records.
func (r *Reconciler) reconcileDiscoveryDeployment(ctx context.Context, deployer deliveryv1alpha1.DeployerObject) (ctrl.Result, error) {
	discovery, err := r.resolveDiscovery(ctx, deployer)
	if discovery == nil || err != nil {
		return ctrl.Result{}, err
	}

	cfg, pm, err := r.configure(ctx, deployer, discovery)
	if err != nil {
		return ctrl.Result{}, err
	}

	root, err := r.resolveDiscoveryRoot(ctx, deployer, discovery, cfg, pm)
	if err != nil {
		return ctrl.Result{}, err
	}

	deployables := make([]deployable, 0, len(discovery.Status.Resources))
	for i := range discovery.Status.Resources {
		d, err := r.loadEntry(ctx, deployer, r.discoveryEntry(discovery, &discovery.Status.Resources[i], root), cfg, pm)
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

// resolveDiscovery fetches the Discovery referenced by the Deployer and validates that it is ready, has observed
// its current spec and has published its resources.
func (r *Reconciler) resolveDiscovery(
	ctx context.Context,
	deployer deliveryv1alpha1.DeployerObject,
) (*deliveryv1alpha1.Discovery, error) {
	key := discoveryKey(deployer)
	discovery, err := util.GetReadyObject[deliveryv1alpha1.Discovery, *deliveryv1alpha1.Discovery](ctx, r.Client, key)
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

	// A Ready condition of an older generation describes resources of an older spec, e.g. other selectors.
	if discovery.Status.ObservedGeneration != discovery.GetGeneration() {
		err := fmt.Errorf("discovery %s has not observed generation %d yet", key, discovery.GetGeneration())
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, err.Error())

		return nil, err
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

// discoveryRoot is the verified root component version of a Discovery, from which every entry is resolved.
type discoveryRoot struct {
	// repositorySpec is the repository the root Component resolved the root from.
	repositorySpec *apiextensionsv1.JSON
	// opts route the root to repositorySpec ahead of configured resolvers, as the Discovery does.
	opts       *resolution.Options
	descriptor *descriptor.Descriptor
	// notSafelyDigestible is the ErrNotSafelyDigestible the signature verification reported, if any.
	notSafelyDigestible error
	// requireDigests rejects every entry that cannot be chained to the root by digests (verificationPolicy Always).
	requireDigests bool
}

// resolveDiscoveryRoot fetches the root component version of the Discovery from its ready root Component and
// verifies its signatures with the verifications of the effective configuration, as the Deployer does for the
// Component of a Resource.
func (r *Reconciler) resolveDiscoveryRoot(
	ctx context.Context,
	deployer deliveryv1alpha1.DeployerObject,
	discovery *deliveryv1alpha1.Discovery,
	cfg *configuration.Configuration,
	pm *manager.PluginManager,
) (*discoveryRoot, error) {
	component, err := util.GetReadyObject[deliveryv1alpha1.Component, *deliveryv1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: discovery.GetNamespace(),
		Name:      discovery.RootComponentName(),
	})
	if err != nil {
		err = fmt.Errorf("failed to get the ready root component of discovery %s: %w", discovery.GetName(), err)
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, err.Error())

		return nil, err
	}
	info := component.Status.Component

	repoSpec, err := decodeRepositorySpec(info.RepositorySpec)
	if err != nil {
		err = fmt.Errorf("failed to decode repository spec of root component %s: %w", component.GetName(), err)
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetRepositoryFailedReason, err.Error())

		return nil, err
	}

	verifications, err := verification.GetVerifications(cfg)
	if err != nil {
		err = fmt.Errorf("failed to get verifications: %w", err)
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetConfigurationFailedReason, err.Error())

		return nil, err
	}

	opts := &resolution.Options{
		RepositorySpec:    repoSpec,
		Configuration:     cfg,
		PluginManager:     pm,
		ComponentPatterns: []string{info.Component},
	}
	desc, err := r.Resolver.GetComponentVersion(ctx, opts, resolution.Verification{Verifications: verifications}, info.Component, info.Version)
	if err != nil && !errors.Is(err, resolution.ErrNotSafelyDigestible) {
		err = fmt.Errorf("failed to verify root component version %s:%s: %w", info.Component, info.Version, err)
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, err
	}

	return &discoveryRoot{
		repositorySpec:      info.RepositorySpec,
		opts:                opts,
		descriptor:          desc,
		notSafelyDigestible: err,
		requireDigests:      discovery.Spec.VerificationPolicy != deliveryv1alpha1.VerificationPolicyNever,
	}, nil
}

// discoveryEntry turns a resource published by the Discovery into an entry. Its component version is resolved from
// the verified root through the entry's reference path and must be the component version, with the digest, the
// Discovery published.
func (r *Reconciler) discoveryEntry(discovery *deliveryv1alpha1.Discovery, res *deliveryv1alpha1.DiscoveredResource, root *discoveryRoot) *entry {
	identity := ocmruntime.Identity{descriptor.IdentityAttributeName: res.Resource.Name}
	if res.Resource.Version != "" {
		identity[descriptor.IdentityAttributeVersion] = res.Resource.Version
	}
	for k, v := range res.Resource.ExtraIdentity {
		identity[k] = v
	}

	return &entry{
		owner:             discovery,
		component:         &res.Component,
		resource:          &res.Resource,
		identity:          identity,
		repositorySpec:    root.repositorySpec,
		componentPatterns: root.opts.ComponentPatterns,
		describe: fmt.Sprintf("resource %s of component %s:%s",
			identity.String(), res.Component.Component, res.Component.Version),
		componentDescriptor: func(ctx context.Context, _ *configuration.Configuration, _ *manager.PluginManager) (*descriptor.Descriptor, error) {
			return r.resolveDiscoveredComponent(ctx, discovery, res, root)
		},
	}
}

// resolveDiscoveredComponent resolves the component version of res from the verified root through its reference
// path. Like the reference path of a Resource, a step without a digest is reported as ErrNotSafelyDigestible,
// unless the Discovery requires a complete digest chain.
func (r *Reconciler) resolveDiscoveredComponent(
	ctx context.Context,
	discovery *deliveryv1alpha1.Discovery,
	res *deliveryv1alpha1.DiscoveredResource,
	root *discoveryRoot,
) (*descriptor.Descriptor, error) {
	desc, _, err := ocm.ResolveReferencePath(ctx, r.Resolver, root.descriptor, res.ReferencePath, root.opts)
	if err != nil && !errors.Is(err, resolution.ErrNotSafelyDigestible) {
		return nil, fmt.Errorf("failed to resolve reference path %v: %w", res.ReferencePath, err)
	}
	notSafelyDigestible := errors.Join(root.notSafelyDigestible, err)
	if err != nil && root.requireDigests {
		return nil, fmt.Errorf("reference path %v is not chained to the root by digests (verificationPolicy %s): %s",
			res.ReferencePath, deliveryv1alpha1.VerificationPolicyAlways, err.Error())
	}

	// The component version the Discovery published must be the one its path leads to from the current root. A
	// root that moved on is drift the Discovery has not caught up with yet.
	if desc.Component.Name != res.Component.Component || desc.Component.Version != res.Component.Version {
		return nil, fmt.Errorf("%w: discovery %s published %s:%s, its reference path %v leads to %s:%s",
			ErrComponentVersionDrift, discovery.GetName(), res.Component.Component, res.Component.Version,
			res.ReferencePath, desc.Component.Name, desc.Component.Version)
	}

	if res.Component.Digest == nil {
		if root.requireDigests {
			return nil, fmt.Errorf("discovery %s published no digest for component version %s:%s (verificationPolicy %s)",
				discovery.GetName(), desc.Component.Name, desc.Component.Version, deliveryv1alpha1.VerificationPolicyAlways)
		}

		return desc, errors.Join(notSafelyDigestible,
			fmt.Errorf("%w: no digest published for component version %s:%s", resolution.ErrNotSafelyDigestible,
				desc.Component.Name, desc.Component.Version))
	}
	if err := resolution.VerifyDigest(ctx, desc, res.Component.Digest); err != nil {
		return nil, fmt.Errorf("component version %s:%s does not match the digest discovery %s published: %w",
			desc.Component.Name, desc.Component.Version, discovery.GetName(), err)
	}
	return desc, notSafelyDigestible
}
