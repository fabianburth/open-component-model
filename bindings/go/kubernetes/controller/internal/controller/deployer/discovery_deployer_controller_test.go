package deployer

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ = Describe("Deployer with a discoveryRef", func() {
	var namespace *corev1.Namespace

	BeforeEach(func(ctx SpecContext) {
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "discovery-deployer-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	})

	It("applies every entry of the Discovery and prunes the entries that drop out", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		entries := discoveryEntries(ctx, ns,
			manifestBlob("cm-a", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-a\n"),
			manifestBlob("cm-b", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-b\n"),
		)
		discovery := createReadyDiscovery(ctx, ns, entries)

		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		Expect(deployer.GetConditions()).To(ContainElement(HaveField("Message", "Applied 2 resources from discovery "+discovery.GetName())))

		By("applying both entries, each object annotated with its own entry")
		for name, resource := range map[string]string{"dd-a": "cm-a", "dd-b": "cm-b"} {
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, cm)).To(Succeed())
			Expect(cm.GetAnnotations()).To(HaveKeyWithValue(annotationResourceIdentityPrefix+"name", resource))
			Expect(cm.GetAnnotations()).To(HaveKeyWithValue(annotationComponentName, entries[0].Component.Component))
			Expect(cm.GetLabels()).To(HaveKeyWithValue(managedByLabel, namespacedDeployerManager))
			Expect(metav1.GetControllerOf(cm)).To(HaveField("UID", deployer.GetUID()))
		}

		By("pruning the objects of an entry that drops out of the Discovery")
		setDiscoveryResources(ctx, discovery, entries[:1])
		Eventually(func(ctx context.Context) bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-b"}, &corev1.ConfigMap{}))
		}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(BeTrue())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-a"}, &corev1.ConfigMap{})).To(Succeed())
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		Expect(deployer.GetConditions()).To(ContainElement(HaveField("Message", "Applied 1 resources from discovery "+discovery.GetName())))
	})

	It("applies nothing when an entry cannot be decoded as manifests", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		entries := discoveryEntries(ctx, ns,
			manifestBlob("cm-kept", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-kept\ndata:\n  v: one\n"),
			manifestBlob("cm-new", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-new\n"),
			fixtureBlob{name: "image", typ: "ociImage", mediaType: "application/vnd.oci.image.layout.v1+tar", content: []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0xfe}},
		)
		discovery := createReadyDiscovery(ctx, ns, entries[:1])

		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		kept := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-kept"}, kept)).To(Succeed())

		By("adding a manifest and an image entry to the Discovery")
		setDiscoveryResources(ctx, discovery, []v1alpha1.DiscoveredResource{entries[1], entries[2]})
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.GetOCMResourceFailedReason, "resource name=image,version=1.0.0 of component")

		By("leaving the existing objects untouched and applying none of the new ones")
		current := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kept), current)).To(Succeed())
		Expect(current.GetResourceVersion()).To(Equal(kept.GetResourceVersion()))
		Expect(errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-new"}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("rejects a component version that does not match the entry's digest", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		entries := discoveryEntries(ctx, ns,
			manifestBlob("cm-tampered", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-tampered\n"),
		)
		entries[0].Component.Digest.Value = fmt.Sprintf("%064d", 0)
		discovery := createReadyDiscovery(ctx, ns, entries)

		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.GetComponentVersionFailedReason, "verifying digest")
		Expect(errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-tampered"}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("resolves the root component from the Discovery's repository ahead of configured resolvers", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		entries := discoveryEntries(ctx, ns,
			manifestBlob("cm-routed", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-routed\n"),
		)

		By("configuring a path matcher that sends the root component to a repository without it, as the Discovery may")
		elsewhere := discoveryEntries(ctx, ns+"-elsewhere", manifestBlob("unrelated", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-unrelated\n"))
		ocmConfig, err := json.Marshal(map[string]any{
			"type": "generic.config.ocm.software/v1",
			"configurations": []any{map[string]any{
				"type": "resolvers.config.ocm.software/v1alpha1",
				"resolvers": []any{map[string]any{
					"repository":           json.RawMessage(elsewhere[0].Component.RepositorySpec.Raw),
					"componentNamePattern": entries[0].Component.Component,
				}},
			}},
		})
		Expect(err).NotTo(HaveOccurred())
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "resolvers", Namespace: ns},
			Data:       map[string]string{".ocmconfig": string(ocmConfig)},
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())

		discovery := createReadyDiscovery(ctx, ns, entries, func(d *v1alpha1.Discovery) {
			d.Status.EffectiveOCMConfig = []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
					Name:       cm.GetName(),
					Namespace:  ns,
				},
				Policy: v1alpha1.ConfigurationPolicyPropagate,
			}}
		})

		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		Expect(deployer.Status.EffectiveOCMConfig).To(ContainElement(HaveField("Name", cm.GetName())))
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-routed"}, &corev1.ConfigMap{})).To(Succeed())
	})

	It("waits for the Discovery to become ready", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		discovery := &v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: ns},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "unused"}},
		}
		Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, discovery)
		})

		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.ResourceIsNotAvailable, "not ready")
	})

	It("waits for the Discovery to observe its current generation", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		entries := discoveryEntries(ctx, ns,
			manifestBlob("cm-gen", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-gen\n"),
		)
		discovery := createReadyDiscovery(ctx, ns, entries)

		By("changing the spec without the Discovery controller observing it")
		discovery.Spec.ResourceSelector = &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "other"}}
		Expect(k8sClient.Update(ctx, discovery)).To(Succeed())

		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.ResourceIsNotAvailable, "has not observed generation")
		Expect(errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-gen"}, &corev1.ConfigMap{}))).To(BeTrue())

		By("applying once the Discovery observed it")
		setDiscoveryResources(ctx, discovery, entries)
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
	})

	It("resolves a referenced component version from the verified root through its reference path", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		f := newFixtureCTF()
		child := f.add(ctx, "ocm.software/child-"+ns, "1.0.0",
			[]fixtureBlob{manifestBlob("cm-child", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-child\ndata:\n  from: genuine\n")}, nil)
		root, publicKey := signedRoot(ctx, f, "ocm.software/root-"+ns, digestedReference(ctx, "child", child))
		entries := f.entries(ctx, child, runtime.Identity{"name": "child", "version": "1.0.0"})
		secret := test.SetupSignatureVerificationConfig(ctx, k8sClient, ns, "verification", test.SignatureVerification{
			Signature: rootSignature, Algorithm: signingv1alpha1.AlgorithmRSASSAPSS, PublicKey: publicKey,
		})

		By("pointing the entry's repository spec at another repository holding a different child")
		attacker := newFixtureCTF()
		attacker.add(ctx, child.Component.Name, child.Component.Version,
			[]fixtureBlob{manifestBlob("cm-child", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-child\ndata:\n  from: attacker\n")}, nil)
		entries[0].Component.RepositorySpec = attacker.spec

		discovery := createReadyDiscoveryOf(ctx, ns, f.info(ctx, root), entries, withEffectiveConfig(secret))
		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})

		By("applying the child reached from the root, ignoring the repository spec in the status")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-child"}, cm)).To(Succeed())
		Expect(cm.Data).To(HaveKeyWithValue("from", "genuine"))
	})

	It("rejects an entry whose status points at another component version", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		f := newFixtureCTF()
		child := f.add(ctx, "ocm.software/child-"+ns, "1.0.0",
			[]fixtureBlob{manifestBlob("cm-child", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-child\ndata:\n  from: genuine\n")}, nil)
		root := f.add(ctx, "ocm.software/root-"+ns, "1.0.0", nil, []descruntime.Reference{digestedReference(ctx, "child", child)})

		By("publishing the repository spec and digest of a component version the root does not reference")
		attacker := newFixtureCTF()
		forged := attacker.add(ctx, child.Component.Name, child.Component.Version,
			[]fixtureBlob{manifestBlob("cm-child", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-child\ndata:\n  from: attacker\n")}, nil)
		entries := attacker.entries(ctx, forged, runtime.Identity{"name": "child", "version": "1.0.0"})

		discovery := createReadyDiscoveryOf(ctx, ns, f.info(ctx, root), entries)
		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.GetComponentVersionFailedReason, "does not match the digest discovery")
		Expect(errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-child"}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("rejects a root whose signature does not verify", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		f := newFixtureCTF()
		root, _ := signedRoot(ctx, f, "ocm.software/root-"+ns, descruntime.Reference{}, manifestBlob("cm-root",
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-root\n"))
		_, otherKey := test.SignComponent(ctx, rootSignature, signingv1alpha1.AlgorithmRSASSAPSS, []byte("other"), pm)
		secret := test.SetupSignatureVerificationConfig(ctx, k8sClient, ns, "verification", test.SignatureVerification{
			Signature: rootSignature, Algorithm: signingv1alpha1.AlgorithmRSASSAPSS, PublicKey: otherKey,
		})

		discovery := createReadyDiscoveryOf(ctx, ns, f.info(ctx, root), f.entries(ctx, root), withEffectiveConfig(secret))
		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.GetComponentVersionFailedReason, "failed to verify root component version")
		Expect(errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-root"}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("rejects two entries rendering the same object", func(ctx SpecContext) {
		ns := namespace.GetName()
		createServiceAccount(ctx, ns, true)
		entries := discoveryEntries(ctx, ns,
			manifestBlob("cm-a", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dup\ndata:\n  from: a\n"),
			manifestBlob("cm-b", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dup\ndata:\n  from: b\n"),
		)
		discovery := createReadyDiscovery(ctx, ns, entries)
		deployer := createDiscoveryNamespacedDeployer(ctx, ns, discovery.GetName())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.ApplyFailed,
			"ConfigMap "+ns+"/dup is rendered by both resource name=cm-a")
		Expect(errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dup"}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("applies a Discovery with a cluster-scoped Deployer", func(ctx SpecContext) {
		ns := namespace.GetName()
		entries := discoveryEntries(ctx, ns,
			manifestBlob("cm-cluster", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dd-cluster\n  namespace: %s\n", ns)),
		)
		discovery := createReadyDiscovery(ctx, ns, entries)

		deployer := &v1alpha1.Deployer{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "discovery-deployer-"},
			Spec: v1alpha1.DeployerSpec{
				DiscoveryRef: &v1alpha1.ObjectKey{Namespace: ns, Name: discovery.GetName()},
			},
		}
		Expect(k8sClient.Create(ctx, deployer)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, deployer)
		})
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dd-cluster"}, cm)).To(Succeed())
		Expect(cm.GetLabels()).To(HaveKeyWithValue(managedByLabel, deployerManager))

		By("pruning the object when the Deployer is deleted")
		test.DeleteObject(ctx, k8sClient, deployer)
		Eventually(func(ctx context.Context) bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{}))
		}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(BeTrue())
	})
})

// rootSignature is the name of the signature of signedRoot.
const rootSignature = "discovery-root"

// signedRoot stores a root component version with ref (if named) and blobs, signed with a new key, and returns it
// with the public key that verifies it.
func signedRoot(ctx SpecContext, f *fixtureCTF, name string, ref descruntime.Reference, blobs ...fixtureBlob) (*descruntime.Descriptor, string) {
	GinkgoHelper()

	var refs []descruntime.Reference
	if ref.Name != "" {
		refs = append(refs, ref)
	}
	var publicKey string
	root := f.add(ctx, name, "1.0.0", blobs, refs, func(desc *descruntime.Descriptor) {
		normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
		Expect(err).NotTo(HaveOccurred())
		var signature descruntime.Signature
		signature, publicKey = test.SignComponent(ctx, rootSignature, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)
		desc.Signatures = append(desc.Signatures, signature)
	})

	return root, publicKey
}
