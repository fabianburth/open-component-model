package graph

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolverAndDiscoverer implements both Resolver and Discoverer for the DAG graph discoverer.
// It first checks constructor components, then falls back to external repositories.
type resolverAndDiscoverer struct {
	componentConstructor      *constructor.ComponentConstructor
	externalRepoProvider      ExternalComponentRepositoryProvider
	resolveExternalLocalBlobs bool
}

var (
	_ syncdag.Resolver[string, *ConstructorOrExternalComponent]   = (*resolverAndDiscoverer)(nil)
	_ syncdag.Discoverer[string, *ConstructorOrExternalComponent] = (*resolverAndDiscoverer)(nil)
)

func (d *resolverAndDiscoverer) Resolve(ctx context.Context, id string) (*ConstructorOrExternalComponent, error) {
	comp, err := d.resolveConstructorComponent(id)
	if err == nil {
		return &ConstructorOrExternalComponent{
			ConstructorComponent: comp,
		}, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("error resolving constructor component %q: %w", id, err)
	}

	extComp, err := d.resolveExternalComponent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error resolving external component %q: %w", id, err)
	}
	return &ConstructorOrExternalComponent{
		ExternalComponent: extComp,
	}, nil
}

func (d *resolverAndDiscoverer) Discover(_ context.Context, component *ConstructorOrExternalComponent) ([]string, error) {
	switch {
	case component.ConstructorComponent != nil:
		children := make([]string, len(component.ConstructorComponent.References))
		for i, ref := range component.ConstructorComponent.References {
			children[i] = ref.ToComponentIdentity().String()
		}
		return children, nil
	case component.ExternalComponent != nil:
		children := make([]string, len(component.ExternalComponent.Component.References))
		for i, ref := range component.ExternalComponent.Component.References {
			children[i] = ref.ToComponentIdentity().String()
		}
		return children, nil
	}
	return nil, fmt.Errorf("component must have either a constructor component or an external component")
}

func (d *resolverAndDiscoverer) resolveConstructorComponent(id string) (*constructor.Component, error) {
	for _, comp := range d.componentConstructor.Components {
		if comp.ToIdentity().String() == id {
			return &comp, nil
		}
	}
	return nil, fmt.Errorf("component %s not found in constructor: %w", id, repository.ErrNotFound)
}

func (d *resolverAndDiscoverer) resolveExternalComponent(ctx context.Context, id string) (*DescriptorWithLocalBlobs, error) {
	identity, err := runtime.ParseIdentity(id)
	if err != nil {
		return nil, fmt.Errorf("failed parsing identity %q: %w", id, err)
	}
	repo, err := d.externalRepoProvider.GetExternalRepository(ctx, identity[descriptor.IdentityAttributeName], identity[descriptor.IdentityAttributeVersion])
	if err != nil {
		return nil, fmt.Errorf("error getting external repository for component %q: %w", identity.String(), err)
	}

	desc, err := repo.GetComponentVersion(ctx, identity[descriptor.IdentityAttributeName], identity[descriptor.IdentityAttributeVersion])
	if err != nil {
		return nil, fmt.Errorf("error getting component version %q from repository: %w", identity.String(), err)
	}

	result := &DescriptorWithLocalBlobs{Descriptor: desc}
	if d.resolveExternalLocalBlobs {
		var mu sync.Mutex
		eg, egctx := errgroup.WithContext(ctx)
		for idx, resource := range desc.Component.Resources {
			if err := v2.Scheme.Convert(resource.Access, &v2.LocalBlob{}); err == nil {
				eg.Go(func() error {
					content, res, err := repo.GetLocalResource(egctx, desc.Component.Name, desc.Component.Version, resource.ToIdentity())
					if err != nil {
						return fmt.Errorf("error getting local resource for component %q: %w", identity.String(), err)
					}
					mu.Lock()
					defer mu.Unlock()
					result.Local = append(result.Local, LocalResourceWithContent{
						Index:    idx,
						Content:  content,
						Resource: res,
					})
					return nil
				})
			}
		}
		if err := eg.Wait(); err != nil {
			return nil, fmt.Errorf("error getting local resources for component %q: %w", identity.String(), err)
		}
	}

	return result, nil
}
