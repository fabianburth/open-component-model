package deployer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const exactlyOneRefMessage = "exactly one of resourceRef and discoveryRef must be set"

// The admission specs create with dry-run, so accepted objects are validated by the apiserver but never
// reach the reconciler.
var _ = Describe("Deployer API", func() {
	var namespace *corev1.Namespace

	BeforeEach(func(ctx SpecContext) {
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "deployer-api-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
	})

	Context("Deployer", func() {
		newDeployer := func(spec v1alpha1.DeployerSpec) *v1alpha1.Deployer {
			return &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "deployer-api-"},
				Spec:       spec,
			}
		}

		It("accepts a resourceRef", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.DeployerSpec{
				ResourceRef: v1alpha1.ObjectKey{Namespace: namespace.GetName(), Name: "resource"},
			})
			Expect(k8sClient.Create(ctx, deployer, client.DryRunAll)).To(Succeed())
		})

		It("accepts a discoveryRef", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.DeployerSpec{
				DiscoveryRef: &v1alpha1.ObjectKey{Namespace: namespace.GetName(), Name: "discovery"},
			})
			Expect(k8sClient.Create(ctx, deployer, client.DryRunAll)).To(Succeed())
			Expect(deployer.GetDiscoveryRef()).To(Equal(&v1alpha1.ObjectKey{Namespace: namespace.GetName(), Name: "discovery"}))
		})

		It("rejects both resourceRef and discoveryRef", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.DeployerSpec{
				ResourceRef:  v1alpha1.ObjectKey{Namespace: namespace.GetName(), Name: "resource"},
				DiscoveryRef: &v1alpha1.ObjectKey{Namespace: namespace.GetName(), Name: "discovery"},
			})
			err := k8sClient.Create(ctx, deployer, client.DryRunAll)
			Expect(errors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(exactlyOneRefMessage))
		})

		It("rejects neither resourceRef nor discoveryRef", func(ctx SpecContext) {
			err := k8sClient.Create(ctx, newDeployer(v1alpha1.DeployerSpec{}), client.DryRunAll)
			Expect(errors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(exactlyOneRefMessage))
		})

		It("rejects a discoveryRef without a name", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.DeployerSpec{
				DiscoveryRef: &v1alpha1.ObjectKey{Namespace: namespace.GetName()},
			})
			err := k8sClient.Create(ctx, deployer, client.DryRunAll)
			Expect(errors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("spec.discoveryRef.name: Required value"))
		})
	})

	Context("NamespacedDeployer", func() {
		newDeployer := func(spec v1alpha1.NamespacedDeployerSpec) *v1alpha1.NamespacedDeployer {
			return &v1alpha1.NamespacedDeployer{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "deployer-api-", Namespace: namespace.GetName()},
				Spec:       spec,
			}
		}

		It("accepts a resourceRef", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.NamespacedDeployerSpec{
				ResourceRef: corev1.LocalObjectReference{Name: "resource"},
			})
			Expect(k8sClient.Create(ctx, deployer, client.DryRunAll)).To(Succeed())
			Expect(deployer.GetDiscoveryRef()).To(BeNil())
		})

		It("accepts a discoveryRef", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.NamespacedDeployerSpec{
				DiscoveryRef: &corev1.LocalObjectReference{Name: "discovery"},
			})
			Expect(k8sClient.Create(ctx, deployer, client.DryRunAll)).To(Succeed())
			Expect(deployer.GetDiscoveryRef()).To(Equal(&v1alpha1.ObjectKey{Namespace: namespace.GetName(), Name: "discovery"}))
		})

		It("rejects both resourceRef and discoveryRef", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.NamespacedDeployerSpec{
				ResourceRef:  corev1.LocalObjectReference{Name: "resource"},
				DiscoveryRef: &corev1.LocalObjectReference{Name: "discovery"},
			})
			err := k8sClient.Create(ctx, deployer, client.DryRunAll)
			Expect(errors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(exactlyOneRefMessage))
		})

		It("rejects neither resourceRef nor discoveryRef", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.NamespacedDeployerSpec{ServiceAccountName: "any"})
			err := k8sClient.Create(ctx, deployer, client.DryRunAll)
			Expect(errors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(exactlyOneRefMessage))
		})

		It("rejects a discoveryRef without a name", func(ctx SpecContext) {
			deployer := newDeployer(v1alpha1.NamespacedDeployerSpec{
				DiscoveryRef: &corev1.LocalObjectReference{},
			})
			err := k8sClient.Create(ctx, deployer, client.DryRunAll)
			Expect(errors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("discoveryRef.name must be set"))
		})
	})
})
