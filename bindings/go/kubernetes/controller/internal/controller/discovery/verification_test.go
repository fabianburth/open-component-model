package discovery

import (
	"bytes"
	"crypto"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	desc "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/oci"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

// ctfFixture is a writable CTF whose read-only spec a Component points at.
type ctfFixture struct {
	repo *oci.Repository
	spec []byte
}

func newCTFFixture(t *testing.T) *ctfFixture {
	t.Helper()

	return newCTFFixtureAt(t, t.TempDir())
}

// newCTFFixtureAt builds the CTF under dir, so a test can delete it.
func newCTFFixtureAt(t *testing.T, dir string) *ctfFixture {
	t.Helper()
	spec := &ctf.Repository{
		Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
		FilePath:   dir,
		AccessMode: ctf.AccessModeReadWrite,
	}
	repo, err := ocirepository.NewFromCTFRepoV1(t.Context(), spec)
	require.NoError(t, err)
	spec.AccessMode = ctf.AccessModeReadOnly
	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	return &ctfFixture{repo: repo, spec: raw}
}

// localBlob stores content as a local blob of component:version. With
// recordOther the returned resource records a digest of other content.
func (f *ctfFixture) localBlob(t *testing.T, component, version, name, content string, recordOther bool) desc.Resource {
	t.Helper()
	res := &desc.Resource{
		ElementMeta: desc.ElementMeta{ObjectMeta: desc.ObjectMeta{Name: name, Version: version}},
		Type:        "plainText",
		Relation:    desc.LocalRelation,
		Access: &v2.LocalBlob{
			Type:      ocmruntime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
			MediaType: "text/plain",
		},
	}
	stored, err := f.repo.AddLocalResource(t.Context(), component, version, res, inmemory.New(bytes.NewReader([]byte(content))))
	require.NoError(t, err)
	if recordOther {
		stored.Digest.Value = digest.FromString("other").Encoded()
	}

	return *stored
}

func (f *ctfFixture) add(t *testing.T, descs ...*desc.Descriptor) {
	t.Helper()
	for _, d := range descs {
		require.NoError(t, f.repo.AddComponentVersion(t.Context(), d))
	}
}

// digest is the digest of the stored component version, as a reference or a
// Component would record it.
func (f *ctfFixture) digest(t *testing.T, name, version string) *v2.Digest {
	t.Helper()
	stored, err := f.repo.GetComponentVersion(t.Context(), name, version)
	require.NoError(t, err)
	d, err := signing.GenerateDigest(t.Context(), stored, slog.New(slog.DiscardHandler),
		signing.LegacyNormalisationAlgo, crypto.SHA256.String())
	require.NoError(t, err)

	return desc.ConvertToV2Digest(d)
}

func anchoredRef(name, component, version string, dig *v2.Digest) desc.Reference {
	ref := makeRef(name, component, version)
	ref.Digest = *desc.ConvertFromV2Digest(dig)

	return ref
}

func ociResource(name, imageReference string, dig *v2.Digest) desc.Resource {
	res := desc.Resource{
		ElementMeta: desc.ElementMeta{ObjectMeta: desc.ObjectMeta{Name: name, Version: "1.0.0"}},
		Type:        "ociImage",
		Relation:    desc.ExternalRelation,
		Access: &ocmruntime.Raw{
			Type: ocmruntime.NewVersionedType("ociArtifact", "v1"),
			Data: []byte(`{"type":"ociArtifact/v1","imageReference":"` + imageReference + `"}`),
		},
	}
	if dig != nil {
		res.Digest = desc.ConvertFromV2Digest(dig)
	}

	return res
}

func discoveryWithConfig(component, ns, configMap string) *v1alpha1.Discovery {
	d := discoveryFor(component, ns)
	if configMap != "" {
		d.Spec.OCMConfig = []v1alpha1.OCMConfiguration{{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Kind: "ConfigMap", Name: configMap},
		}}
	}

	return d
}

func reconcileOnce(t *testing.T, rec *Reconciler, c client.Client, d *v1alpha1.Discovery) (*v1alpha1.Discovery, error) {
	t.Helper()
	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(d)})
	current := &v1alpha1.Discovery{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(d), current))

	return current, err
}

func accessOf(t *testing.T, res v1alpha1.ResourceInfo) map[string]any {
	t.Helper()
	access := map[string]any{}
	require.NoError(t, json.Unmarshal(res.Access.Raw, &access))

	return access
}

// verifiedGraph is root (OCI resource) -> child (two local blobs), every
// reference and the Component anchored to the stored digests.
type verifiedGraph struct {
	fixture   *ctfFixture
	registry  *testRegistry
	component *v1alpha1.Component
	configMap *corev1.ConfigMap
	rootDig   *v2.Digest
	childDig  *v2.Digest
}

func newVerifiedGraph(t *testing.T, ns string) *verifiedGraph {
	t.Helper()
	g := &verifiedGraph{fixture: newCTFFixture(t), registry: newTestRegistry(t)}

	child := makeDescriptor("ocm.software/child", "1.0.0")
	child.Component.Resources = []desc.Resource{
		g.fixture.localBlob(t, "ocm.software/child", "1.0.0", "zeta", "zeta content", false),
		g.fixture.localBlob(t, "ocm.software/child", "1.0.0", "alpha", "alpha content", false),
	}
	g.fixture.add(t, child)
	g.childDig = g.fixture.digest(t, "ocm.software/child", "1.0.0")

	root := makeDescriptor("ocm.software/root", "1.0.0",
		anchoredRef("child", "ocm.software/child", "1.0.0", g.childDig))
	root.Component.Resources = []desc.Resource{ociResource("image", g.registry.imageReference(), &v2.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "ociArtifactDigest/v1",
		Value:                  g.registry.manifest.Encoded(),
	})}
	g.fixture.add(t, root)
	g.rootDig = g.fixture.digest(t, "ocm.software/root", "1.0.0")

	g.configMap = ocmConfigMap("http", ns, g.registry.ocmConfig)
	g.component = readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", g.fixture.spec)
	g.component.Status.Component.Digest = g.rootDig

	return g
}

func TestVerifiedDiscovery_PublishesPinnedResources(t *testing.T) {
	r := require.New(t)
	const ns = "default"
	g := newVerifiedGraph(t, ns)
	discovery := discoveryWithConfig(g.component.Name, ns, g.configMap.Name)
	rec, c := realPluginReconciler(t, g.configMap, g.component, discovery)

	current, err := reconcileOnce(t, rec, c, discovery)
	r.NoError(err)

	ready := status.FindCondition(current, v1alpha1.ReadyCondition)
	r.Equal(metav1.ConditionTrue, ready.Status, ready.Message)
	r.Contains(ready.Message, "verified 3 resources")

	r.Len(current.Status.Resources, 3)
	type key struct{ component, resource string }
	got := make([]key, 0, len(current.Status.Resources))
	for _, e := range current.Status.Resources {
		got = append(got, key{e.Component.Component, e.Resource.Name})
		r.JSONEq(string(g.fixture.spec), string(e.Component.RepositorySpec.Raw), "entries carry the root repository spec")
		r.Equal("1.0.0", e.Component.Version)
		r.NotNil(e.Resource.Digest)
	}
	r.Equal([]key{
		{"ocm.software/child", "alpha"},
		{"ocm.software/child", "zeta"},
		{"ocm.software/root", "image"},
	}, got, "sorted by component name, version and resource identity")

	alpha, image := current.Status.Resources[0], current.Status.Resources[2]
	r.Equal(g.childDig, alpha.Component.Digest, "a child carries the digest its parent reference records")
	r.Equal(g.rootDig, image.Component.Digest, "the root carries the digest of the Component")
	r.Equal(v2.LocalBlobAccessType+"/"+v2.LocalBlobAccessTypeVersion, accessOf(t, alpha.Resource)["type"])
	r.Equal(digest.FromString("alpha content").String(), accessOf(t, alpha.Resource)["localReference"])
	r.Equal(g.registry.imageReference()+"@"+g.registry.manifest.String(), accessOf(t, image.Resource)["imageReference"],
		"the OCI access is pinned to the verified manifest digest")
	r.Equal(g.registry.manifest.Encoded(), image.Resource.Digest.Value)
}

func TestVerifiedDiscovery_ExtractReadsPinnedAccess(t *testing.T) {
	r := require.New(t)
	const ns = "default"
	g := newVerifiedGraph(t, ns)
	discovery := discoveryWithConfig(g.component.Name, ns, g.configMap.Name)
	discovery.Spec.ResourceSelector = &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "image"}}
	discovery.Spec.Extract = &v1alpha1.Extract{ByResources: map[string]string{"ref": "resource.access.imageReference"}}
	rec, c := realPluginReconciler(t, g.configMap, g.component, discovery)

	current, err := reconcileOnce(t, rec, c, discovery)
	r.NoError(err)
	r.Len(current.Status.Extracted, 1)
	r.JSONEq(`"`+g.registry.imageReference()+"@"+g.registry.manifest.String()+`"`, string(current.Status.Extracted[0]["ref"].Raw))
	r.Len(current.Status.Resources, 1, "only resources surviving the selectors are verified and published")
	r.Contains(status.FindCondition(current, v1alpha1.ReadyCondition).Message, "verified 1 resources")
}

func TestVerifiedDiscovery_TamperedChildDescriptorFails(t *testing.T) {
	r := require.New(t)
	const ns = "default"
	f := newCTFFixture(t)

	original := makeDescriptor("ocm.software/child", "1.0.0")
	f.add(t, original)
	recorded := f.digest(t, "ocm.software/child", "1.0.0")
	tampered := makeDescriptor("ocm.software/child", "1.0.0")
	tampered.Component.Provider.Name = "attacker"
	f.add(t, tampered)

	root := makeDescriptor("ocm.software/root", "1.0.0", anchoredRef("child", "ocm.software/child", "1.0.0", recorded))
	f.add(t, root)
	component := readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", f.spec)
	component.Status.Component.Digest = f.digest(t, "ocm.software/root", "1.0.0")

	discovery := discoveryFor(component.Name, ns)
	discovery.Status.Components = []apiextensionsv1.JSON{{Raw: []byte(`{"kept":true}`)}}
	rec, c := realPluginReconciler(t, component, discovery)

	current, err := reconcileOnce(t, rec, c, discovery)
	r.Error(err)
	r.NotErrorIs(err, reconcile.TerminalError(nil), "a mismatch is retried with backoff")

	ready := status.FindCondition(current, v1alpha1.ReadyCondition)
	r.Equal(metav1.ConditionFalse, ready.Status)
	r.Equal(v1alpha1.GetComponentVersionFailedReason, ready.Reason)
	r.Contains(ready.Message, "ocm.software/child:1.0.0")
	r.Equal([]apiextensionsv1.JSON{{Raw: []byte(`{"kept":true}`)}}, current.Status.Components, "no new payload is published")
	r.Nil(current.Status.Resources)
}

// A child reached through several parents must match every incoming
// reference, not just the first one.
func TestVerifiedDiscovery_EveryIncomingReferenceIsChecked(t *testing.T) {
	r := require.New(t)
	const ns = "default"
	f := newCTFFixture(t)

	f.add(t, makeDescriptor("ocm.software/shared", "1.0.0"))
	good := f.digest(t, "ocm.software/shared", "1.0.0")
	bad := *good
	bad.Value = digest.FromString("other").Encoded()

	f.add(t,
		makeDescriptor("ocm.software/a", "1.0.0", anchoredRef("shared", "ocm.software/shared", "1.0.0", good)),
		makeDescriptor("ocm.software/b", "1.0.0", anchoredRef("shared", "ocm.software/shared", "1.0.0", &bad)),
	)
	f.add(t, makeDescriptor("ocm.software/root", "1.0.0",
		anchoredRef("a", "ocm.software/a", "1.0.0", f.digest(t, "ocm.software/a", "1.0.0")),
		anchoredRef("b", "ocm.software/b", "1.0.0", f.digest(t, "ocm.software/b", "1.0.0")),
	))
	component := readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", f.spec)
	component.Status.Component.Digest = f.digest(t, "ocm.software/root", "1.0.0")
	discovery := discoveryFor(component.Name, ns)
	rec, c := realPluginReconciler(t, component, discovery)

	current, err := reconcileOnce(t, rec, c, discovery)
	r.Error(err)
	ready := status.FindCondition(current, v1alpha1.ReadyCondition)
	r.Equal(v1alpha1.GetComponentVersionFailedReason, ready.Reason)
	r.Contains(ready.Message, "ocm.software/shared:1.0.0")
	r.Contains(ready.Message, "ocm.software/b:1.0.0")
}

func TestVerifiedDiscovery_RootIsAnchoredToComponentDigest(t *testing.T) {
	r := require.New(t)
	const ns = "default"
	f := newCTFFixture(t)
	f.add(t, makeDescriptor("ocm.software/root", "1.0.0"))

	component := readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", f.spec)
	component.Status.Component.Digest = &v2.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: signing.LegacyNormalisationAlgo,
		Value:                  digest.FromString("other").Encoded(),
	}
	discovery := discoveryFor(component.Name, ns)
	rec, c := realPluginReconciler(t, component, discovery)

	current, err := reconcileOnce(t, rec, c, discovery)
	r.Error(err)
	ready := status.FindCondition(current, v1alpha1.ReadyCondition)
	r.Equal(v1alpha1.GetComponentVersionFailedReason, ready.Reason)
	r.Contains(ready.Message, "ocm.software/root:1.0.0")
}

// Under Always, Ready means chained to the verified root: a missing root or
// reference digest fails closed, but not terminally. Under Never every gap is
// reported as a warning, like ErrNotSafelyDigestible on a Resource.
func TestVerifiedDiscovery_MissingDigests(t *testing.T) {
	const ns = "default"
	type refDigest int
	const (
		noDigest refDigest = iota
		validDigest
		tamperedDigest
	)
	build := func(t *testing.T, rootDigest bool, referenceDigest refDigest, policy v1alpha1.VerificationPolicy) (*Reconciler, client.Client, *v1alpha1.Discovery) {
		t.Helper()
		f := newCTFFixture(t)
		child := makeDescriptor("ocm.software/child", "1.0.0")
		child.Component.Resources = []desc.Resource{f.localBlob(t, "ocm.software/child", "1.0.0", "data", "content", false)}
		f.add(t, child)
		ref := makeRef("child", "ocm.software/child", "1.0.0")
		if referenceDigest != noDigest {
			dig := f.digest(t, "ocm.software/child", "1.0.0")
			if referenceDigest == tamperedDigest {
				dig.Value = digest.FromString("other").Encoded()
			}
			ref = anchoredRef("child", "ocm.software/child", "1.0.0", dig)
		}
		f.add(t, makeDescriptor("ocm.software/root", "1.0.0", ref))
		component := readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", f.spec)
		if rootDigest {
			component.Status.Component.Digest = f.digest(t, "ocm.software/root", "1.0.0")
		}
		discovery := discoveryFor(component.Name, ns)
		discovery.Spec.VerificationPolicy = policy
		rec, c := realPluginReconciler(t, component, discovery)

		return rec, c, discovery
	}
	failsClosed := func(t *testing.T, rec *Reconciler, c client.Client, discovery *v1alpha1.Discovery, gap string) {
		t.Helper()
		r := require.New(t)
		current, err := reconcileOnce(t, rec, c, discovery)
		r.Error(err)
		r.NotErrorIs(err, reconcile.TerminalError(nil))
		ready := status.FindCondition(current, v1alpha1.ReadyCondition)
		r.Equal(metav1.ConditionFalse, ready.Status)
		r.Equal(v1alpha1.IncompleteDigestChainReason, ready.Reason)
		r.Contains(ready.Message, gap)
		r.Nil(current.Status.Resources)
		r.Empty(current.Status.ObservedComponentDigest)
	}

	t.Run("Always fails on a root without a digest", func(t *testing.T) {
		rec, c, discovery := build(t, false, validDigest, v1alpha1.VerificationPolicyAlways)
		failsClosed(t, rec, c, discovery, "root component version ocm.software/root:1.0.0 has no complete digest")
	})

	t.Run("Always fails on a reference without a digest", func(t *testing.T) {
		rec, c, discovery := build(t, true, noDigest, v1alpha1.VerificationPolicyAlways)
		failsClosed(t, rec, c, discovery, `reference "child" of ocm.software/root:1.0.0 to ocm.software/child:1.0.0 has no complete digest`)
	})

	t.Run("Always publishes a complete chain with reference paths", func(t *testing.T) {
		r := require.New(t)
		rec, c, discovery := build(t, true, validDigest, v1alpha1.VerificationPolicyAlways)

		current, err := reconcileOnce(t, rec, c, discovery)
		r.NoError(err)
		r.Equal(metav1.ConditionTrue, status.FindCondition(current, v1alpha1.ReadyCondition).Status)
		r.NotEmpty(current.Status.ObservedComponentDigest)
		r.Len(current.Status.Resources, 1)
		entry := current.Status.Resources[0]
		r.Equal("ocm.software/child", entry.Component.Component)
		r.NotNil(entry.Component.Digest)
		r.Equal([]ocmruntime.Identity{{"name": "child", "version": "1.0.0"}}, entry.ReferencePath)
	})

	t.Run("Never warns and is Ready", func(t *testing.T) {
		r := require.New(t)
		rec, c, discovery := build(t, false, noDigest, v1alpha1.VerificationPolicyNever)

		current, err := reconcileOnce(t, rec, c, discovery)
		r.NoError(err)
		r.Equal(metav1.ConditionTrue, status.FindCondition(current, v1alpha1.ReadyCondition).Status)
		r.Empty(current.Status.ObservedComponentDigest, "an incomplete chain never short-circuits")
		r.Len(current.Status.Resources, 1)
		entry := current.Status.Resources[0]
		r.Nil(entry.Component.Digest)
		r.Equal([]ocmruntime.Identity{{"name": "child", "version": "1.0.0"}}, entry.ReferencePath,
			"an unanchored component version still gets a path from the root")

		events := drainEvents(t, rec)
		r.Contains(events, "Warning")
		r.Contains(events, "ocm.software/root:1.0.0")
		r.Contains(events, "ocm.software/child:1.0.0")
	})

	t.Run("Never still fails on a present digest that does not match", func(t *testing.T) {
		r := require.New(t)
		rec, c, discovery := build(t, false, tamperedDigest, v1alpha1.VerificationPolicyNever)

		current, err := reconcileOnce(t, rec, c, discovery)
		r.Error(err)
		ready := status.FindCondition(current, v1alpha1.ReadyCondition)
		r.Equal(metav1.ConditionFalse, ready.Status)
		r.Equal(v1alpha1.GetComponentVersionFailedReason, ready.Reason)
		r.Contains(ready.Message, `does not match the digest recorded by reference "child"`)
	})
}

func TestVerifiedDiscovery_TamperedResource(t *testing.T) {
	const ns = "default"
	build := func(t *testing.T, policy v1alpha1.VerificationPolicy) (*Reconciler, client.Client, *v1alpha1.Discovery) {
		t.Helper()
		f := newCTFFixture(t)
		root := makeDescriptor("ocm.software/root", "1.0.0")
		root.Component.Resources = []desc.Resource{
			f.localBlob(t, "ocm.software/root", "1.0.0", "data", "content", true),
			// Never must not touch the network: this registry does not exist.
			ociResource("image", "registry.invalid/test/app:1.0.0", nil),
		}
		f.add(t, root)
		component := readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", f.spec)
		component.Status.Component.Digest = f.digest(t, "ocm.software/root", "1.0.0")
		discovery := discoveryFor(component.Name, ns)
		discovery.Spec.VerificationPolicy = policy
		rec, c := realPluginReconciler(t, component, discovery)

		return rec, c, discovery
	}

	t.Run("Always fails closed", func(t *testing.T) {
		r := require.New(t)
		rec, c, discovery := build(t, v1alpha1.VerificationPolicyAlways)

		current, err := reconcileOnce(t, rec, c, discovery)
		r.Error(err)
		r.NotErrorIs(err, reconcile.TerminalError(nil))
		ready := status.FindCondition(current, v1alpha1.ReadyCondition)
		r.Equal(metav1.ConditionFalse, ready.Status)
		r.Equal(v1alpha1.GetOCMResourceFailedReason, ready.Reason)
		r.Nil(current.Status.Resources)
		r.Nil(current.Status.Components)
	})

	t.Run("Never publishes unpinned", func(t *testing.T) {
		r := require.New(t)
		rec, c, discovery := build(t, v1alpha1.VerificationPolicyNever)

		current, err := reconcileOnce(t, rec, c, discovery)
		r.NoError(err)
		ready := status.FindCondition(current, v1alpha1.ReadyCondition)
		r.Equal(metav1.ConditionTrue, ready.Status)
		r.Contains(ready.Message, "verification skipped")
		r.Len(current.Status.Resources, 2)
		r.Equal("data", current.Status.Resources[0].Resource.Name)
		r.Equal("registry.invalid/test/app:1.0.0", accessOf(t, current.Status.Resources[1].Resource)["imageReference"])
	})
}
