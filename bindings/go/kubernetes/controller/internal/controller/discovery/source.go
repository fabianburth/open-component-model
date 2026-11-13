package discovery

import (
	"context"
	"errors"
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

// sourceFieldManager owns the fields of the Repository and Component
// generated from a Discovery's inline source.
const sourceFieldManager = "discovery-controller"

// errGeneratedObjectConflict reports an object with a generated name that the
// Discovery does not control.
var errGeneratedObjectConflict = errors.New("object exists and is not controlled by the discovery")

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
			Name:      v1alpha1.GeneratedRepositoryName(discovery.GetName()),
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
			Name:      v1alpha1.GeneratedComponentName(discovery.GetName()),
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

	// Nothing is written unless the Discovery controls every object it would
	// write: applying with forced ownership would otherwise adopt and rewrite
	// a foreign object that happens to have a generated name.
	generated := []client.Object{repository, component}
	for _, obj := range generated {
		if err := r.checkControlled(ctx, discovery, obj); err != nil {
			return err
		}
	}
	for _, obj := range generated {
		if err := r.applyGenerated(ctx, discovery, obj); err != nil {
			return err
		}
	}

	return nil
}

// checkControlled fails with errGeneratedObjectConflict if an object with the
// name of desired exists and is not controlled by the Discovery.
func (r *Reconciler) checkControlled(ctx context.Context, discovery *v1alpha1.Discovery, desired client.Object) error {
	existing, ok := desired.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("unexpected type %T after deep copy", desired)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to get %T %s: %w", desired, desired.GetName(), err)
	}
	if !metav1.IsControlledBy(existing, discovery) {
		return fmt.Errorf("%w: %T %s", errGeneratedObjectConflict, desired, desired.GetName())
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
// Only objects the Discovery controls are deleted. A Discovery that now
// references the generated Component keeps both: the Component still
// references the generated Repository.
func (r *Reconciler) deleteGenerated(ctx context.Context, discovery *v1alpha1.Discovery) error {
	if discovery.Spec.ComponentRef.Name == v1alpha1.GeneratedComponentName(discovery.GetName()) {
		return nil
	}

	generated := []client.Object{
		&v1alpha1.Component{ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.GeneratedComponentName(discovery.GetName())}},
		&v1alpha1.Repository{ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.GeneratedRepositoryName(discovery.GetName())}},
	}
	for _, obj := range generated {
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
