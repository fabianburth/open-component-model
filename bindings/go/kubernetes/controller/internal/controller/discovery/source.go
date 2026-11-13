package discovery

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const (
	// sourceFieldManager owns the fields of the Repository and Component
	// generated from a Discovery's inline source.
	sourceFieldManager = "discovery-controller"

	// generateSourceFailedReason marks a Discovery whose generated Repository
	// or Component could not be applied or deleted.
	generateSourceFailedReason = "GenerateSourceFailed"
)

// generatedRepositoryName is the name of the Repository generated from the
// inline source of the named Discovery.
func generatedRepositoryName(discovery string) string {
	return discovery + "-repository"
}

// generatedComponentName is the name of the Component generated from the
// inline source of the named Discovery.
func generatedComponentName(discovery string) string {
	return discovery + "-component"
}

// rootComponentName is the name of the Component whose graph the Discovery
// discovers: the generated one for an inline source, the referenced one
// otherwise.
func rootComponentName(discovery *v1alpha1.Discovery) string {
	if discovery.Spec.Source != nil {
		return generatedComponentName(discovery.GetName())
	}

	return discovery.Spec.ComponentRef.Name
}

// reconcileSource converges the objects generated from the inline source: it
// server-side applies the Repository and Component while a source is set,
// and deletes them once the Discovery no longer has one.
func (r *Reconciler) reconcileSource(ctx context.Context, discovery *v1alpha1.Discovery) error {
	if discovery.Spec.Source == nil {
		return r.deleteGenerated(ctx, discovery)
	}

	source := discovery.Spec.Source
	repository := &v1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedRepositoryName(discovery.GetName()),
			Namespace: discovery.GetNamespace(),
		},
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: source.RepositorySpec,
			OCMConfig:      discovery.Spec.OCMConfig,
			Interval:       source.Interval,
		},
	}
	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedComponentName(discovery.GetName()),
			Namespace: discovery.GetNamespace(),
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef:   corev1.LocalObjectReference{Name: repository.GetName()},
			Component:       source.Component,
			DowngradePolicy: source.DowngradePolicy,
			Semver:          source.Semver,
			SemverFilter:    source.SemverFilter,
			OCMConfig:       discovery.Spec.OCMConfig,
			Interval:        source.Interval,
		},
	}

	for _, obj := range []client.Object{repository, component} {
		if err := r.applyGenerated(ctx, discovery, obj); err != nil {
			return err
		}
	}

	return nil
}

// applyGenerated server-side applies obj as controlled by the Discovery. The
// apply forces ownership, so edits by others to the applied fields are
// reverted on the next reconcile.
func (r *Reconciler) applyGenerated(ctx context.Context, discovery *v1alpha1.Discovery, obj client.Object) error {
	if err := controllerutil.SetControllerReference(discovery, obj, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on %T %s: %w", obj, obj.GetName(), err)
	}

	gvk, err := r.GroupVersionKindFor(obj)
	if err != nil {
		return fmt.Errorf("failed to resolve kind of %T: %w", obj, err)
	}

	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("failed to convert %T %s: %w", obj, obj.GetName(), err)
	}
	// Status belongs to the child's own controller, and the zero creation
	// timestamp would serialise as null.
	delete(content, "status")
	unstructured.RemoveNestedField(content, "metadata", "creationTimestamp")

	desired := &unstructured.Unstructured{Object: content}
	desired.SetGroupVersionKind(gvk)

	if err := r.Apply(ctx, client.ApplyConfigurationFromUnstructured(desired),
		client.FieldOwner(sourceFieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply %s %s: %w", gvk.Kind, obj.GetName(), err)
	}

	return nil
}

// deleteGenerated deletes the objects generated from a former inline source.
// Only objects the Discovery controls are deleted, and never the Component
// it now references.
func (r *Reconciler) deleteGenerated(ctx context.Context, discovery *v1alpha1.Discovery) error {
	generated := []client.Object{
		&v1alpha1.Component{ObjectMeta: metav1.ObjectMeta{Name: generatedComponentName(discovery.GetName())}},
		&v1alpha1.Repository{ObjectMeta: metav1.ObjectMeta{Name: generatedRepositoryName(discovery.GetName())}},
	}
	for _, obj := range generated {
		if obj.GetName() == discovery.Spec.ComponentRef.Name {
			continue
		}

		key := client.ObjectKey{Namespace: discovery.GetNamespace(), Name: obj.GetName()}
		if err := r.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return fmt.Errorf("failed to get generated %T %s: %w", obj, key, err)
		}
		if !metav1.IsControlledBy(obj, discovery) || !obj.GetDeletionTimestamp().IsZero() {
			continue
		}

		if err := r.Delete(ctx, obj, client.Preconditions{UID: new(obj.GetUID())}); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete generated %T %s: %w", obj, key, err)
		}
	}

	return nil
}
