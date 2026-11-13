package deployer

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha3"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
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

// fixtureBlob is a local blob resource of the Discovery fixture component.
type fixtureBlob struct {
	name, typ, mediaType string
	content              []byte
}

func manifestBlob(name, manifest string) fixtureBlob {
	return fixtureBlob{name: name, typ: "plainText", mediaType: "application/x-yaml", content: []byte(manifest)}
}

// discoveryEntries stores the blobs as local resources of one component version in a CTF and returns the entries
// a Discovery publishes for them, anchored by the digest of the component version.
func discoveryEntries(ctx SpecContext, namespace string, blobs ...fixtureBlob) []v1alpha1.DiscoveredResource {
	GinkgoHelper()

	componentName := "ocm.software/discovery-deployer-" + namespace
	componentVersion := "v1.0.0"
	tempDir := GinkgoT().TempDir()

	fs, err := filesystem.NewFS(tempDir, os.O_RDWR)
	Expect(err).NotTo(HaveOccurred())
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(tempDir))
	Expect(err).NotTo(HaveOccurred())

	resources := make([]descruntime.Resource, 0, len(blobs))
	for _, b := range blobs {
		res := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: b.name, Version: "1.0.0"},
			},
			Type:     b.typ,
			Relation: descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
				MediaType: b.mediaType,
			},
		}
		added, err := repo.AddLocalResource(ctx, componentName, componentVersion, res, inmemory.New(bytes.NewReader(b.content)))
		Expect(err).NotTo(HaveOccurred())
		resources = append(resources, *added)
	}

	desc := &descruntime.Descriptor{
		Meta: descruntime.Meta{Version: "v2"},
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider:  descruntime.Provider{Name: "ocm.software"},
			Resources: resources,
		},
	}
	Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

	digest, err := signing.GenerateDigest(ctx, desc, slog.New(logr.ToSlogHandler(log.FromContext(ctx))), v4alpha3.Algorithm, crypto.SHA256.String())
	Expect(err).NotTo(HaveOccurred())

	specData, err := json.Marshal(&ctfv1.Repository{
		Type:       runtime.Type{Name: "ctf", Version: "v1"},
		FilePath:   tempDir,
		AccessMode: ctfv1.AccessModeReadOnly,
	})
	Expect(err).NotTo(HaveOccurred())

	entries := make([]v1alpha1.DiscoveredResource, 0, len(resources))
	for _, res := range resources {
		access, err := json.Marshal(res.Access)
		Expect(err).NotTo(HaveOccurred())
		entries = append(entries, v1alpha1.DiscoveredResource{
			Component: v1alpha1.ComponentInfo{
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
				Component:      componentName,
				Version:        componentVersion,
				Digest:         descruntime.ConvertToV2Digest(digest),
			},
			Resource: v1alpha1.ResourceInfo{
				Name:    res.Name,
				Type:    res.Type,
				Version: res.Version,
				Access:  apiextensionsv1.JSON{Raw: access},
				Digest:  descruntime.ConvertToV2Digest(res.Digest),
			},
		})
	}

	return entries
}

// createReadyDiscovery creates a Discovery and publishes the entries in its status, as the Discovery controller
// would once the graph is verified. The Discovery controller does not run in this suite.
func createReadyDiscovery(ctx SpecContext, namespace string, entries []v1alpha1.DiscoveredResource) *v1alpha1.Discovery {
	GinkgoHelper()

	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: namespace},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "unused"}},
	}
	Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, discovery)
	})
	setDiscoveryResources(ctx, discovery, entries)

	return discovery
}

func setDiscoveryResources(ctx SpecContext, discovery *v1alpha1.Discovery, entries []v1alpha1.DiscoveredResource) {
	GinkgoHelper()

	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), discovery)).To(Succeed())
	discovery.Status.Resources = append([]v1alpha1.DiscoveredResource{}, entries...)
	status.MarkReady(recorder, discovery, "discovered")
	discovery.SetObservedGeneration(discovery.GetGeneration())
	Expect(k8sClient.Status().Update(ctx, discovery)).To(Succeed())
}

func createDiscoveryNamespacedDeployer(ctx SpecContext, namespace, discoveryName string) *v1alpha1.NamespacedDeployer {
	GinkgoHelper()

	deployer := &v1alpha1.NamespacedDeployer{
		ObjectMeta: metav1.ObjectMeta{Name: "nd", Namespace: namespace},
		Spec: v1alpha1.NamespacedDeployerSpec{
			DiscoveryRef:       &corev1.LocalObjectReference{Name: discoveryName},
			ServiceAccountName: namespacedDeployerSA,
		},
	}
	Expect(k8sClient.Create(ctx, deployer)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, deployer)
	})

	return deployer
}
