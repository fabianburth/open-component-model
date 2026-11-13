package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	internaldiscovery "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/discovery"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Reconciler reconciles Discovery objects: it resolves the complete
// transitive component graph of the referenced ready Component, anchors every
// descriptor to the digest that reaches it, evaluates the compiled
// selector/extraction query against it, verifies the surviving resources, and
// atomically publishes the resulting payload, resources and conditions into
// the Discovery status.
//
// The request-scoped plugin manager is built with the reconcile context so
// cancellation propagates into plugin calls. Repository access happens
// through the shared Resolver, rooted at the referenced Component's
// repository spec.
type Reconciler struct {
	*ocm.BaseReconciler

	// Resolver fetches and verifies component versions through the shared,
	// cached repository resolvers.
	Resolver *resolution.Resolver
}

var _ ocm.Reconciler = (*Reconciler)(nil)

// rootComponentIndex keys Discoveries by the name of their root Component:
// the referenced one, or the one generated from their inline source.
const rootComponentIndex = "rootComponentName"

// SetupWithManager sets up the Discovery controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, concurrency int) error {
	// Build index for discoveries that reference a component to make sure that we
	// get notified when a component changes.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Discovery{}, rootComponentIndex, func(obj client.Object) []string {
		discovery, ok := obj.(*v1alpha1.Discovery)
		if !ok {
			return nil
		}

		return []string{discovery.RootComponentName()}
	}); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Discovery{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha1.Component{},
			handler.EnqueueRequestsFromMapFunc(r.mapComponentToDiscoveries),
			builder.WithPredicates(ocm.ComponentInfoChangedPredicate{})).
		// Spec edits of the objects generated from an inline source enqueue
		// the Discovery, which re-applies its desired spec.
		Owns(&v1alpha1.Component{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&v1alpha1.Repository{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: concurrency,
			RateLimiter:             r.RateLimit.Limiter(),
		}).
		Complete(r)
}

// mapComponentToDiscoveries maps a Component to the Discoveries that reference
// it as their graph root, via the same-namespace componentRef index.
func (r *Reconciler) mapComponentToDiscoveries(ctx context.Context, obj client.Object) []reconcile.Request {
	component, ok := obj.(*v1alpha1.Component)
	if !ok {
		return nil
	}

	referencing := &v1alpha1.DiscoveryList{}
	if err := r.List(ctx, referencing,
		client.InNamespace(component.GetNamespace()),
		client.MatchingFields{rootComponentIndex: component.GetName()}); err != nil {
		log.FromContext(ctx).Error(err, "failed to list discoveries referencing component as root",
			"component", client.ObjectKeyFromObject(component))

		return nil
	}

	requests := make([]reconcile.Request, 0, len(referencing.Items))
	for i := range referencing.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&referencing.Items[i]),
		})
	}

	return requests
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries,verbs=get;list;watch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries/finalizers,verbs=update
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=repositories;components,verbs=get;list;watch;create;update;patch;delete

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	discovery := &v1alpha1.Discovery{}
	if err := r.Get(ctx, req.NamespacedName, discovery); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deleting and suspended objects exit without status advancement: the old
	// conditions and payload are retained as-is.
	if !discovery.GetDeletionTimestamp().IsZero() {
		logger.Info("discovery is being deleted, skipping reconciliation")
		return ctrl.Result{}, nil
	}
	if discovery.Spec.Suspend {
		logger.Info("discovery is suspended, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	old := discovery.DeepCopy()
	result, err := r.reconcile(ctx, discovery)
	if perr := r.publish(ctx, old, discovery); perr != nil {
		// log the error of the reconcile since we weren't able to update the state of the object.
		if err != nil {
			logger.Error(err, "reconciliation failed and the status could not be published")
		}

		return ctrl.Result{}, fmt.Errorf("updating status failed for discovery: %w", perr)
	}

	return result, err
}

// reconcile executes the discovery pipeline against discovery and mutates its
// status in memory. Status publication happens separately in Reconcile.
//
// The following errors are Terminal: selector and extraction compilation and
// evaluation and payload to large. The rest, backoff, including descriptor
// and resource verification failures: they fail closed and keep the last
// published payload.
//
//nolint:funlen,cyclop // the pipeline is intentionally linear; splitting it would obscure the failure-semantics
func (r *Reconciler) reconcile(ctx context.Context, discovery *v1alpha1.Discovery) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.reconcileSource(ctx, discovery); err != nil {
		reason := v1alpha1.GenerateSourceFailedReason
		if errors.Is(err, errGeneratedObjectConflict) {
			reason = v1alpha1.GeneratedObjectConflictReason
		}
		status.MarkNotReady(r.EventRecorder, discovery, reason, err.Error())

		return ctrl.Result{}, err
	}

	component, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: discovery.GetNamespace(),
		Name:      discovery.RootComponentName(),
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.ResourceIsNotAvailable, err.Error())
		logger.Info("component is not available", "error", err)

		return ctrl.Result{}, fmt.Errorf("failed to get ready component: %w", err)
	}

	info := component.Status.Component
	if info.Component == "" || info.Version == "" || info.RepositorySpec == nil {
		err := fmt.Errorf("component %s has no complete resolved identity and repository spec", component.GetName())
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.ResourceIsNotAvailable, err.Error())

		return ctrl.Result{}, err
	}

	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), discovery, component)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get effective config: %w", err)
	}

	// Record the effective config in memory. Reconcile publishes the mutated
	// status whether or not a later step fails, so the used configuration
	// references stay observable without an extra round trip.
	discovery.Status.EffectiveOCMConfig = configs

	if upToDate(discovery, info) {
		logger.V(1).Info("root component digest unchanged, skipping re-discovery",
			"digest", discovery.Status.ObservedComponentDigest)

		// Purely watch-driven: no periodic requeue.
		return ctrl.Result{}, nil
	}

	query, err := internaldiscovery.Compile(ctx, &discovery.Spec)
	if err != nil {
		status.MarkAsStalled(r.EventRecorder, discovery, v1alpha1.SelectorFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	cfg, err := configuration.LoadConfigurations(ctx, r.Client, discovery.GetNamespace(), configs)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to load configurations: %w", err)
	}

	pm, err := r.PluginManagerFor(ctx, cfg)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to create plugin manager: %w", err)
	}

	spec := &runtime.Raw{}
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Decode(bytes.NewReader(info.RepositorySpec.Raw), spec); err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	// The root pattern routes the root to the repository the Component
	// resolved it from, ahead of any configured resolver.
	resolveOpts := &resolution.Options{
		RepositorySpec:    spec,
		Configuration:     cfg,
		PluginManager:     pm,
		ComponentPatterns: []string{info.Component},
	}
	resolver, err := r.Resolver.RepositoryResolver(ctx, resolveOpts)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to create repository resolver: %w", err)
	}

	root := internaldiscovery.ComponentKey{Name: info.Component, Version: info.Version}
	graph, err := internaldiscovery.Traverse(ctx, root, resolver)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.ResolutionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to resolve the transitive component graph: %w", err)
	}
	logger.V(1).Info("resolved transitive component graph", "components", len(graph.Descriptors))

	// The Component verified the root signature; anchoring every descriptor to
	// the digest that reaches it extends that trust to the whole graph.
	anchors, unanchored, err := internaldiscovery.Anchor(ctx, *graph, root, info.Digest, resolution.VerifyDigest)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to verify the component graph: %w", err)
	}
	// Under Always, Ready means every published component version is chained
	// to the verified root, so a gap in the chain fails closed. Under Never
	// it is reported only; the digests that are present were checked anyway.
	if len(unanchored) > 0 {
		gaps := fmt.Errorf("%w: %s", resolution.ErrNotSafelyDigestible, strings.Join(unanchored, "; "))
		if discovery.Spec.VerificationPolicy != v1alpha1.VerificationPolicyNever {
			status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.IncompleteDigestChainReason,
				gaps.Error()+" (set verificationPolicy Never to accept an incomplete digest chain)")

			return ctrl.Result{}, fmt.Errorf("failed to chain the component graph to its root: %w", gaps)
		}
		event.New(r.EventRecorder, discovery, nil, v1alpha1.EventSeverityError, "%s", gaps.Error())
	}

	filtered, err := query.Filter(ctx, *graph)
	if err != nil {
		status.MarkAsStalled(r.EventRecorder, discovery, v1alpha1.SelectorFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	verified := -1
	if discovery.Spec.VerificationPolicy != v1alpha1.VerificationPolicyNever {
		if verified, err = r.verifyResources(ctx, filtered, resolveOpts, cfg); err != nil {
			status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetOCMResourceFailedReason, err.Error())

			return ctrl.Result{}, fmt.Errorf("failed to verify resources: %w", err)
		}
	}

	payload, err := query.Project(ctx, filtered)
	if err != nil {
		status.MarkAsStalled(r.EventRecorder, discovery, v1alpha1.ExtractFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	resources, err := discoveredResources(filtered, anchors, internaldiscovery.ReferencePaths(*graph, root, anchors), info.RepositorySpec)
	if err != nil {
		status.MarkAsStalled(r.EventRecorder, discovery, v1alpha1.MarshalFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	if err := r.setPayload(discovery, payload, resources); err != nil {
		reason := v1alpha1.MarshalFailedReason
		if errors.Is(err, errPayloadTooLarge) {
			reason = v1alpha1.PayloadTooLargeReason
		}
		status.MarkAsStalled(r.EventRecorder, discovery, reason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	// Only a complete chain lets the root digest stand for the whole graph.
	discovery.Status.ObservedComponentDigest = ""
	if len(unanchored) == 0 {
		discovery.Status.ObservedComponentDigest = digestKey(info.Digest)
	}

	status.MarkReady(r.EventRecorder, discovery, "%s, %s", payloadMessage(payload), verificationMessage(verified))
	// Discovery advances the observed generation here because it does not use
	// the shared UpdateBeforePatch defer the other controllers rely on.
	discovery.SetObservedGeneration(discovery.GetGeneration())
	event.New(r.EventRecorder, discovery, discovery.GetVID(), v1alpha1.EventSeverityInfo,
		"Reconciliation finished, no further runs scheduled until next change")

	// Purely watch-driven: no periodic requeue.
	return ctrl.Result{}, nil
}

// maxPayloadBytes etcd cap.
const maxPayloadBytes = 1 << 20

// errPayloadTooLarge happens if the status would be too large to store.
var errPayloadTooLarge = errors.New("status payload exceeds the size limit")

// setPayload swaps the status payload in one in-memory update: resources are
// set, the selected payload field is set (possibly to an empty list), the
// other field is cleared. If the payload was too large, return
// errPayloadTooLarge instead and leave the status untouched, so we can surface
// that and set the object to Ready=False.
func (r *Reconciler) setPayload(
	discovery *v1alpha1.Discovery,
	payload *internaldiscovery.Payload,
	resources []v1alpha1.DiscoveredResource,
) error {
	if resources == nil {
		resources = []v1alpha1.DiscoveredResource{}
	}
	raw, err := json.Marshal(resources)
	if err != nil {
		return fmt.Errorf("failed to marshal resources: %w", err)
	}
	// this is a rough estimate, but is enough for our purposes.
	size := len(raw)

	if payload.Extracted != nil {
		records := make([]v1alpha1.ExtractedRecord, 0, len(payload.Extracted))
		for _, record := range payload.Extracted {
			typed := make(v1alpha1.ExtractedRecord, len(record))
			for name, value := range record {
				raw, err := json.Marshal(value)
				if err != nil {
					return fmt.Errorf("failed to marshal extracted field %q: %w", name, err)
				}
				size += len(name) + len(raw)
				typed[name] = apiextensionsv1.JSON{Raw: raw}
			}
			records = append(records, typed)
		}
		if size > maxPayloadBytes {
			return fmt.Errorf("%w: %d extracted records and %d resources total %d bytes, limit is %d",
				errPayloadTooLarge, len(records), len(resources), size, maxPayloadBytes)
		}
		discovery.Status.Extracted = records
		discovery.Status.Components = nil
		discovery.Status.Resources = resources

		return nil
	}

	components := make([]apiextensionsv1.JSON, 0, len(payload.Components))
	for _, raw := range payload.Components {
		size += len(raw)
		components = append(components, apiextensionsv1.JSON{Raw: raw})
	}
	if size > maxPayloadBytes {
		return fmt.Errorf("%w: %d descriptors and %d resources total %d bytes, limit is %d",
			errPayloadTooLarge, len(components), len(resources), size, maxPayloadBytes)
	}
	discovery.Status.Components = components
	discovery.Status.Extracted = nil
	discovery.Status.Resources = resources

	return nil
}

// upToDate checks whether the digest of the root component is the same
// as last time we encountered it. If yes, the traverse can be skipped.
//
// A component descriptor digest covers its references' digests, which cover
// theirs, so an unchanged root digest means an unchanged transitive graph. The
// recorded digest is only written when that chain was complete (see
// internaldiscovery.Anchor), so an empty value here always forces a full
// re-discovery.
func upToDate(discovery *v1alpha1.Discovery, info v1alpha1.ComponentInfo) bool {
	if discovery.Status.ObservedComponentDigest == "" {
		return false
	}
	if discovery.Status.ObservedGeneration != discovery.GetGeneration() {
		return false
	}
	if discovery.Status.Components == nil && discovery.Status.Extracted == nil {
		return false
	}
	if discovery.Status.Resources == nil {
		return false
	}
	ready := status.FindCondition(discovery, v1alpha1.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return false
	}

	return discovery.Status.ObservedComponentDigest == digestKey(info.Digest)
}

// digestKey renders a digest as a comparable key. The algorithms are part of it
// because the same component yields a different value under a different
// normalisation.
func digestKey(digest *v2.Digest) string {
	if digest == nil || digest.Value == "" {
		return ""
	}

	return fmt.Sprintf("%s:%s:%s", digest.NormalisationAlgorithm, digest.HashAlgorithm, digest.Value)
}

// payloadMessage returns the Ready condition message for a successful
// evaluation. Every stage noun pluralises with a bare "s".
func payloadMessage(payload *internaldiscovery.Payload) string {
	if payload.EmptyStage != "" {
		return fmt.Sprintf("discovery succeeded, but no %ss matched the %s selector",
			payload.EmptyStage, payload.EmptyStage)
	}

	return fmt.Sprintf("discovery succeeded with %d results", resultCount(payload))
}

// verificationMessage reports how many resources were verified; a negative
// count means verification was skipped.
func verificationMessage(verified int) string {
	if verified < 0 {
		return "resource verification skipped (verificationPolicy Never)"
	}

	return fmt.Sprintf("verified %d resources", verified)
}

func resultCount(payload *internaldiscovery.Payload) int {
	if payload.Extracted != nil {
		return len(payload.Extracted)
	}
	return len(payload.Components)
}
