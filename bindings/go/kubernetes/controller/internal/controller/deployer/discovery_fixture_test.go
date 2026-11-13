package deployer

import (
	"bytes"
	"crypto"
	"encoding/json"
	"log/slog"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

// rootComponentObject is the name of the root Component of the Discovery fixtures.
const rootComponentObject = "root"

// fixtureBlob is a local blob resource of the Discovery fixture component.
type fixtureBlob struct {
	name, typ, mediaType string
	content              []byte
}

func manifestBlob(name, manifest string) fixtureBlob {
	return fixtureBlob{name: name, typ: "plainText", mediaType: "application/x-yaml", content: []byte(manifest)}
}

// fixtureCTF is a CTF a Discovery fixture graph is stored in.
type fixtureCTF struct {
	repo *oci.Repository
	spec *apiextensionsv1.JSON
}

func newFixtureCTF() *fixtureCTF {
	GinkgoHelper()

	tempDir := GinkgoT().TempDir()
	fs, err := filesystem.NewFS(tempDir, os.O_RDWR)
	Expect(err).NotTo(HaveOccurred())
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(tempDir))
	Expect(err).NotTo(HaveOccurred())
	specData, err := json.Marshal(&ctfv1.Repository{
		Type:       runtime.Type{Name: "ctf", Version: "v1"},
		FilePath:   tempDir,
		AccessMode: ctfv1.AccessModeReadOnly,
	})
	Expect(err).NotTo(HaveOccurred())

	return &fixtureCTF{repo: repo, spec: &apiextensionsv1.JSON{Raw: specData}}
}

// add stores the blobs as local resources of name:version and the component version with the references. mutate
// may change the descriptor, e.g. sign it, before it is stored.
func (f *fixtureCTF) add(
	ctx SpecContext,
	name, version string,
	blobs []fixtureBlob,
	refs []descruntime.Reference,
	mutate ...func(*descruntime.Descriptor),
) *descruntime.Descriptor {
	GinkgoHelper()

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
		added, err := f.repo.AddLocalResource(ctx, name, version, res, inmemory.New(bytes.NewReader(b.content)))
		Expect(err).NotTo(HaveOccurred())
		resources = append(resources, *added)
	}

	desc := &descruntime.Descriptor{
		Meta: descruntime.Meta{Version: "v2"},
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: name, Version: version},
			},
			Provider:   descruntime.Provider{Name: "ocm.software"},
			Resources:  resources,
			References: refs,
		},
	}
	for _, m := range mutate {
		m(desc)
	}
	Expect(f.repo.AddComponentVersion(ctx, desc)).To(Succeed())

	return desc
}

// info is the component info of desc as the Discovery publishes it: the repository of the root and the digest of
// the component version.
func (f *fixtureCTF) info(ctx SpecContext, desc *descruntime.Descriptor) v1alpha1.ComponentInfo {
	GinkgoHelper()

	return v1alpha1.ComponentInfo{
		RepositorySpec: f.spec.DeepCopy(),
		Component:      desc.Component.Name,
		Version:        desc.Component.Version,
		Digest:         componentDigest(ctx, desc),
	}
}

func componentDigest(ctx SpecContext, desc *descruntime.Descriptor) *v2.Digest {
	GinkgoHelper()

	digest, err := signing.GenerateDigest(ctx, desc, slog.New(logr.ToSlogHandler(log.FromContext(ctx))), v4alpha3.Algorithm, crypto.SHA256.String())
	Expect(err).NotTo(HaveOccurred())

	return descruntime.ConvertToV2Digest(digest)
}

// digestedReference references desc by name, recording its digest.
func digestedReference(ctx SpecContext, name string, desc *descruntime.Descriptor) descruntime.Reference {
	GinkgoHelper()

	ref := descruntime.Reference{Component: desc.Component.Name, Digest: *descruntime.ConvertFromV2Digest(componentDigest(ctx, desc))}
	ref.Name = name
	ref.Version = desc.Component.Version

	return ref
}

// entries returns the entries a Discovery publishes for the resources of desc, reached through path.
func (f *fixtureCTF) entries(ctx SpecContext, desc *descruntime.Descriptor, path ...runtime.Identity) []v1alpha1.DiscoveredResource {
	GinkgoHelper()

	component := f.info(ctx, desc)
	entries := make([]v1alpha1.DiscoveredResource, 0, len(desc.Component.Resources))
	for _, res := range desc.Component.Resources {
		access, err := json.Marshal(res.Access)
		Expect(err).NotTo(HaveOccurred())
		entries = append(entries, v1alpha1.DiscoveredResource{
			Component: *component.DeepCopy(),
			Resource: v1alpha1.ResourceInfo{
				Name:    res.Name,
				Type:    res.Type,
				Version: res.Version,
				Access:  apiextensionsv1.JSON{Raw: access},
				Digest:  descruntime.ConvertToV2Digest(res.Digest),
			},
			ReferencePath: path,
		})
	}

	return entries
}

// discoveryEntries stores the blobs as local resources of one root component version in a new CTF and returns the
// entries a Discovery publishes for them.
func discoveryEntries(ctx SpecContext, namespace string, blobs ...fixtureBlob) []v1alpha1.DiscoveredResource {
	GinkgoHelper()

	f := newFixtureCTF()

	return f.entries(ctx, f.add(ctx, "ocm.software/discovery-deployer-"+namespace, "v1.0.0", blobs, nil))
}

// createReadyDiscovery creates a ready root Component for the root component of the entries and a Discovery of it
// that publishes the entries, as the Discovery controller would once the graph is verified. Neither controller runs
// in this suite.
func createReadyDiscovery(ctx SpecContext, namespace string, entries []v1alpha1.DiscoveredResource, mutate ...func(*v1alpha1.Discovery)) *v1alpha1.Discovery {
	GinkgoHelper()

	return createReadyDiscoveryOf(ctx, namespace, entries[0].Component, entries, mutate...)
}

// createReadyDiscoveryOf is createReadyDiscovery for entries of the graph of root.
func createReadyDiscoveryOf(
	ctx SpecContext,
	namespace string,
	root v1alpha1.ComponentInfo,
	entries []v1alpha1.DiscoveredResource,
	mutate ...func(*v1alpha1.Discovery),
) *v1alpha1.Discovery {
	GinkgoHelper()

	component := test.MockComponent(ctx, rootComponentObject, namespace, &test.MockComponentOptions{
		Client:     k8sClient,
		Recorder:   recorder,
		Info:       *root.DeepCopy(),
		Repository: "unused",
	})
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, component)
	})

	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: namespace},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: component.GetName()}},
	}
	Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, discovery)
	})
	setDiscoveryResources(ctx, discovery, entries, mutate...)

	return discovery
}

func setDiscoveryResources(ctx SpecContext, discovery *v1alpha1.Discovery, entries []v1alpha1.DiscoveredResource, mutate ...func(*v1alpha1.Discovery)) {
	GinkgoHelper()

	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), discovery)).To(Succeed())
	discovery.Status.Resources = append([]v1alpha1.DiscoveredResource{}, entries...)
	for _, m := range mutate {
		m(discovery)
	}
	status.MarkReady(recorder, discovery, "discovered")
	discovery.SetObservedGeneration(discovery.GetGeneration())
	Expect(k8sClient.Status().Update(ctx, discovery)).To(Succeed())
}

// withEffectiveConfig propagates the .ocmconfig in secret through the Discovery, as from its root Component.
func withEffectiveConfig(secret *corev1.Secret) func(*v1alpha1.Discovery) {
	return func(d *v1alpha1.Discovery) {
		d.Status.EffectiveOCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(secret)}
	}
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
