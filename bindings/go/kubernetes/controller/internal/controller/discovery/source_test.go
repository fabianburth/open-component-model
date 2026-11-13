package discovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	desc "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// These specs run the Discovery reconciler against envtest, restricted to one
// namespace. The Repository and Component controllers do not run: the specs
// set the generated Component's status themselves. The rate limiter admits a
// single fast retry, so every reconcile after it is triggered by a watch.
// sourceSpecTimeout bounds each spec: Eventually with a context has no
// default timeout.
const sourceSpecTimeout = time.Minute

var _ = Describe("Discovery with an inline source", Ordered, ContinueOnFailure, func() {
	const rootComponent = "ocm.software/root"

	var (
		namespace string
		repoSpec  []byte
	)

	BeforeAll(func(ctx SpecContext) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "discovery-source-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.GetName()

		repoSpec = sourceCTF(ctx, makeDescriptor(rootComponent, "1.0.0"))

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

		mgr, err := ctrl.NewManager(testEnv.Config, ctrl.Options{
			Scheme:  scheme,
			Metrics: metricserver.Options{BindAddress: "0"},
			Cache: cache.Options{
				DefaultNamespaces: map[string]cache.Config{namespace: {}},
			},
			Controller: config.Controller{SkipNameValidation: ptr.To(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		mgrCtx, cancel := context.WithCancel(context.Background())
		Expect((&Reconciler{
			BaseReconciler: &ocm.BaseReconciler{
				Client:        mgr.GetClient(),
				Scheme:        scheme,
				EventRecorder: &record.FakeRecorder{},
				NewPluginManager: func(ctx context.Context, cfg *genericv1.Config) (*manager.PluginManager, error) {
					return setup.NewPluginManager(ctx, cfg, slog.New(slog.DiscardHandler))
				},
				RateLimit: ocm.RateLimit{QPS: 0.001, Burst: 1},
			},
			Resolver: resolution.NewResolver(new(logr.Discard())),
		}).SetupWithManager(mgrCtx, mgr, 1)).To(Succeed())

		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(done)
			Expect(mgr.Start(mgrCtx)).To(Succeed())
		}()

		DeferCleanup(func() {
			cancel()
			<-done
		})
	})

	newSourceDiscovery := func(ctx SpecContext, name string, mutate func(*v1alpha1.Discovery)) *v1alpha1.Discovery {
		discovery := &v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: v1alpha1.DiscoverySpec{
				Source: &v1alpha1.DiscoverySource{
					RepositorySpec: &apiextensionsv1.JSON{Raw: repoSpec},
					Component:      rootComponent,
					Semver:         "1.0.0",
					Interval:       metav1.Duration{Duration: 10 * time.Minute},
				},
				VerificationPolicy: v1alpha1.VerificationPolicyNever,
			},
		}
		if mutate != nil {
			mutate(discovery)
		}
		Expect(k8sClient.Create(ctx, discovery)).To(Succeed())

		return discovery
	}

	getGenerated := func(ctx context.Context, discovery *v1alpha1.Discovery) (*v1alpha1.Repository, *v1alpha1.Component) {
		GinkgoHelper()
		repository := &v1alpha1.Repository{}
		component := &v1alpha1.Component{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: discovery.GetName() + "-repository"}, repository)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: discovery.GetName() + "-component"}, component)).To(Succeed())
		}).WithContext(ctx).Should(Succeed())

		return repository, component
	}

	It("generates an owned Repository and Component from the source", func(ctx SpecContext) {
		ocmConfig := []v1alpha1.OCMConfiguration{{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Kind: "Secret", Name: "credentials"},
			Policy:                        v1alpha1.ConfigurationPolicyDoNotPropagate,
		}}
		discovery := newSourceDiscovery(ctx, "generates", func(d *v1alpha1.Discovery) {
			d.Spec.Source.SemverFilter = ".*-rc.*"
			d.Spec.Source.DowngradePolicy = v1alpha1.DowngradePolicyAllow
			d.Spec.OCMConfig = ocmConfig
		})

		repository, component := getGenerated(ctx, discovery)

		Expect(repository.Spec.RepositorySpec.Raw).To(MatchJSON(repoSpec))
		Expect(repository.Spec.Interval.Duration).To(Equal(10 * time.Minute))
		Expect(repository.Spec.OCMConfig).To(Equal(ocmConfig))

		Expect(component.Spec.RepositoryRef.Name).To(Equal(repository.GetName()))
		Expect(component.Spec.Component).To(Equal(rootComponent))
		Expect(component.Spec.Semver).To(Equal("1.0.0"))
		Expect(component.Spec.SemverFilter).To(Equal(".*-rc.*"))
		Expect(component.Spec.DowngradePolicy).To(Equal(v1alpha1.DowngradePolicyAllow))
		Expect(component.Spec.Interval.Duration).To(Equal(10 * time.Minute))
		Expect(component.Spec.OCMConfig).To(Equal(ocmConfig))

		for _, obj := range []client.Object{repository, component} {
			Expect(obj.GetOwnerReferences()).To(ConsistOf(metav1.OwnerReference{
				APIVersion:         v1alpha1.GroupVersion.String(),
				Kind:               "Discovery",
				Name:               discovery.GetName(),
				UID:                discovery.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}), "generated %T must be controlled by the Discovery", obj)
		}

		Eventually(func(g Gomega) {
			fresh := &v1alpha1.Discovery{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fresh)).To(Succeed())
			ready := status.FindCondition(fresh, v1alpha1.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Reason).To(Equal(v1alpha1.ResourceIsNotAvailable))
		}).WithContext(ctx).Should(Succeed(), "the Discovery waits for the generated Component")
	}, SpecTimeout(sourceSpecTimeout))

	It("re-applies the desired spec on source changes and drift", func(ctx SpecContext) {
		discovery := newSourceDiscovery(ctx, "drift", nil)
		_, component := getGenerated(ctx, discovery)
		Expect(component.Spec.Semver).To(Equal("1.0.0"))

		By("changing source.semver on the Discovery")
		Eventually(func(g Gomega) {
			fresh := &v1alpha1.Discovery{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fresh)).To(Succeed())
			fresh.Spec.Source.Semver = ">=1.0.0"
			g.Expect(k8sClient.Update(ctx, fresh)).To(Succeed())
		}).WithContext(ctx).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(component), component)).To(Succeed())
			g.Expect(component.Spec.Semver).To(Equal(">=1.0.0"))
		}).WithContext(ctx).Should(Succeed())

		By("editing the generated Component")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(component), component)).To(Succeed())
			component.Spec.Semver = "9.9.9"
			g.Expect(k8sClient.Update(ctx, component)).To(Succeed())
		}).WithContext(ctx).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(component), component)).To(Succeed())
			g.Expect(component.Spec.Semver).To(Equal(">=1.0.0"))
		}).WithContext(ctx).Should(Succeed(), "the controller reverts edits to the generated Component")
	}, SpecTimeout(sourceSpecTimeout))

	It("reconciles when the generated Component becomes ready", func(ctx SpecContext) {
		discovery := newSourceDiscovery(ctx, "ready", nil)
		_, component := getGenerated(ctx, discovery)

		Eventually(func(g Gomega) {
			fresh := &v1alpha1.Discovery{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fresh)).To(Succeed())
			ready := status.FindCondition(fresh, v1alpha1.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Reason).To(Equal(v1alpha1.ResourceIsNotAvailable))
		}).WithContext(ctx).Should(Succeed())

		By("marking the generated Component ready, as the Component controller would")
		old := component.DeepCopy()
		component.Status.Component = v1alpha1.ComponentInfo{
			RepositorySpec: &apiextensionsv1.JSON{Raw: repoSpec},
			Component:      rootComponent,
			Version:        "1.0.0",
		}
		status.MarkReady(&record.FakeRecorder{}, component, "test")
		Expect(k8sClient.Status().Patch(ctx, component, client.MergeFrom(old))).To(Succeed())

		Eventually(func(g Gomega) {
			fresh := &v1alpha1.Discovery{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fresh)).To(Succeed())
			g.Expect(status.IsReady(fresh)).To(BeTrue(), "conditions: %v", fresh.Status.Conditions)
			g.Expect(fresh.Status.Components).To(HaveLen(1))
		}).WithContext(ctx).Should(Succeed())
	}, SpecTimeout(sourceSpecTimeout))

	It("deletes the generated objects when switching to componentRef", func(ctx SpecContext) {
		discovery := newSourceDiscovery(ctx, "switch", nil)
		repository, component := getGenerated(ctx, discovery)

		Eventually(func(g Gomega) {
			fresh := &v1alpha1.Discovery{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fresh)).To(Succeed())
			fresh.Spec.Source = nil
			fresh.Spec.ComponentRef = corev1.LocalObjectReference{Name: "external"}
			g.Expect(k8sClient.Update(ctx, fresh)).To(Succeed())
		}).WithContext(ctx).Should(Succeed())

		for _, obj := range []client.Object{component, repository} {
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
			}).WithContext(ctx).Should(Satisfy(apierrors.IsNotFound), "generated %T must be deleted", obj)
		}
	}, SpecTimeout(sourceSpecTimeout))
})

// sourceCTF builds a read-only CTF repository holding descs and returns its
// spec as JSON.
func sourceCTF(ctx context.Context, descs ...*desc.Descriptor) []byte {
	GinkgoHelper()

	spec := &ctf.Repository{
		Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
		FilePath:   GinkgoT().TempDir(),
		AccessMode: ctf.AccessModeReadWrite,
	}
	repo, err := ocirepository.NewFromCTFRepoV1(ctx, spec)
	Expect(err).NotTo(HaveOccurred())
	for _, d := range descs {
		Expect(repo.AddComponentVersion(ctx, d)).To(Succeed())
	}
	spec.AccessMode = ctf.AccessModeReadOnly
	raw, err := json.Marshal(spec)
	Expect(err).NotTo(HaveOccurred())

	return raw
}
