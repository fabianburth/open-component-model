package discovery

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	internaldiscovery "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/discovery"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
)

// resourceVerificationLimit bounds the resources verified concurrently in one
// reconcile. Verification can download content, so this caps both the open
// connections and the memory of one Discovery.
const resourceVerificationLimit = 4

// verifyResources verifies every resource of filtered, as the Resource
// controller verifies its one resource, and replaces it with the verified
// resource: a local resource is checked against the digest its repository
// stores it under, any other is checked by its digest processor and gets its
// access pinned to the verified digest. It returns the number of verified
// resources. The first failure fails the whole view.
//
// filtered shares its descriptors with the graph, so every descriptor that is
// written to is replaced by a shallow copy owning its resource slice.
func (r *Reconciler) verifyResources(
	ctx context.Context,
	filtered *internaldiscovery.Filtered,
	opts *resolution.Options,
	cfg *configuration.Configuration,
) (int, error) {
	descriptors := make([]*descruntime.Descriptor, len(filtered.Descriptors))
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(resourceVerificationLimit)
	count := 0
	for i, original := range filtered.Descriptors {
		desc := *original
		desc.Component.Resources = slices.Clone(original.Component.Resources)
		descriptors[i] = &desc
		for j := range desc.Component.Resources {
			count++
			group.Go(func() error {
				res := &desc.Component.Resources[j]
				if err := r.verifyResource(gctx, &desc, res, opts, cfg); err != nil {
					return fmt.Errorf("failed to verify resource %q of %s:%s: %w",
						res.Name, desc.Component.Name, desc.Component.Version, err)
				}
				return nil
			})
		}
	}
	if err := group.Wait(); err != nil {
		return 0, err
	}
	filtered.Descriptors = descriptors

	return count, nil
}

// verifyResource verifies res, a resource of desc, in place. Callers must own
// res exclusively.
func (r *Reconciler) verifyResource(
	ctx context.Context,
	desc *descruntime.Descriptor,
	res *descruntime.Resource,
	opts *resolution.Options,
	cfg *configuration.Configuration,
) error {
	if ocm.IsLocalResource(res) {
		return ocm.VerifyLocalResource(ctx, r.Resolver, opts, desc, res)
	}
	verified, err := ocm.VerifyResource(ctx, opts.PluginManager, res.DeepCopy(), cfg)
	if err != nil {
		return err
	}
	*res = *verified

	return nil
}

// discoveredResources builds one status entry per resource of filtered,
// sorted by component name, component version and resource identity. Every
// entry carries the root repository spec, from which the resolver of the
// Discovery reaches every component version, and the digest its component
// version was anchored to.
func discoveredResources(
	filtered *internaldiscovery.Filtered,
	anchors internaldiscovery.Anchors,
	repositorySpec *apiextensionsv1.JSON,
) ([]v1alpha1.DiscoveredResource, error) {
	type entry struct {
		identity string
		resource v1alpha1.DiscoveredResource
	}
	entries := make([]entry, 0)
	for _, desc := range filtered.Descriptors {
		key := internaldiscovery.ComponentKey{Name: desc.Component.Name, Version: desc.Component.Version}
		for i := range desc.Component.Resources {
			res := &desc.Component.Resources[i]
			info, err := ocm.BuildResourceInfo(res)
			if err != nil {
				return nil, fmt.Errorf("failed to build status of resource %q of %s: %w", res.Name, key, err)
			}
			entries = append(entries, entry{
				identity: res.ToIdentity().String(),
				resource: v1alpha1.DiscoveredResource{
					Component: v1alpha1.ComponentInfo{
						RepositorySpec: repositorySpec.DeepCopy(),
						Component:      key.Name,
						Version:        key.Version,
						Digest:         anchors[key].Digest.DeepCopy(),
					},
					Resource: *info,
				},
			})
		}
	}
	slices.SortStableFunc(entries, func(a, b entry) int {
		return cmp.Or(
			strings.Compare(a.resource.Component.Component, b.resource.Component.Component),
			strings.Compare(a.resource.Component.Version, b.resource.Component.Version),
			strings.Compare(a.identity, b.identity),
		)
	})

	resources := make([]v1alpha1.DiscoveredResource, 0, len(entries))
	for _, e := range entries {
		resources = append(resources, e.resource)
	}

	return resources, nil
}
