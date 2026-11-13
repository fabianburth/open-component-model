package discovery

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	desc "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// ctfRepoAt builds a CTF under dir so the caller can delete it mid-test and
// prove whether a later reconcile touched the repository at all.
func ctfRepoAt(t *testing.T, dir string, descs ...*desc.Descriptor) []byte {
	t.Helper()
	r := require.New(t)

	spec := &ctf.Repository{
		Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
		FilePath:   dir,
		AccessMode: ctf.AccessModeReadWrite,
	}
	repo, err := ocirepository.NewFromCTFRepoV1(t.Context(), spec)
	r.NoError(err)
	for _, d := range descs {
		r.NoError(repo.AddComponentVersion(t.Context(), d))
	}
	spec.AccessMode = ctf.AccessModeReadOnly
	raw, err := json.Marshal(spec)
	r.NoError(err)

	return raw
}

func discoveryFor(componentName, namespace string) *v1alpha1.Discovery {
	return &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: namespace},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: componentName},
		},
	}
}

// TestReconcile_UnchangedRootDigestSkipsTraversal proves the skip by deleting
// the repository between reconciles: a second reconcile that still succeeds
// cannot have fetched anything.
func TestReconcile_UnchangedRootDigestSkipsTraversal(t *testing.T) {
	r := require.New(t)
	const ns = "default"

	dir := t.TempDir()
	f := newCTFFixtureAt(t, dir)
	f.add(t, makeDescriptor("ocm.software/child", "1.0.0"))
	f.add(t, makeDescriptor("ocm.software/root", "1.0.0",
		anchoredRef("child", "ocm.software/child", "1.0.0", f.digest(t, "ocm.software/child", "1.0.0"))))

	component := readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", f.spec)
	component.Status.Component.Digest = f.digest(t, "ocm.software/root", "1.0.0")
	discovery := discoveryFor(component.Name, ns)
	rec, c := realPluginReconciler(t, component, discovery)
	key := client.ObjectKeyFromObject(discovery)

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.NoError(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), key, current))
	r.ElementsMatch([]string{"ocm.software/root", "ocm.software/child"}, componentNamesFromStatus(t, current))
	r.NotEmpty(current.Status.ObservedComponentDigest, "a fully digested graph must record the root digest")

	// Remove the repository. Any traversal from here on must fail.
	r.NoError(os.RemoveAll(dir))

	_, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.NoError(err, "an unchanged root digest must not re-traverse")

	after := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), key, after))
	r.ElementsMatch([]string{"ocm.software/root", "ocm.software/child"}, componentNamesFromStatus(t, after))

	// Control: a changed root digest must re-traverse, and now fail.
	fresh := &v1alpha1.Component{}
	r.NoError(c.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: component.Name}, fresh))
	fresh.Status.Component.Digest.Value = "rootdigest-changed"
	r.NoError(c.Status().Update(t.Context(), fresh))

	_, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.Error(err, "a changed root digest must re-traverse and hit the deleted repository")
}

// TestReconcile_MissingReferenceDigestForcesFullTraversal covers the trust rule:
// GenerateDigest does not enforce IsSafelyDigestible and v2.Reference.Digest is
// omitempty, so an undigested reference is silently absent from the root's
// digest input. One missing digest therefore invalidates the whole chain.
func TestReconcile_MissingReferenceDigestForcesFullTraversal(t *testing.T) {
	r := require.New(t)
	const ns = "default"

	dir := t.TempDir()
	f := newCTFFixtureAt(t, dir)
	f.add(t,
		makeDescriptor("ocm.software/child", "1.0.0"),
		makeDescriptor("ocm.software/root", "1.0.0", makeRef("child", "ocm.software/child", "1.0.0")), // no digest
	)

	component := readyComponentWithSpec("component", ns, "ocm.software/root", "1.0.0", f.spec)
	component.Status.Component.Digest = f.digest(t, "ocm.software/root", "1.0.0")
	discovery := discoveryFor(component.Name, ns)
	// Only Never publishes a graph with a broken digest chain.
	discovery.Spec.VerificationPolicy = v1alpha1.VerificationPolicyNever
	rec, c := realPluginReconciler(t, component, discovery)
	key := client.ObjectKeyFromObject(discovery)

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.NoError(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), key, current))
	r.Empty(current.Status.ObservedComponentDigest, "a broken digest chain must not be recorded")

	r.NoError(os.RemoveAll(dir))

	_, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.Error(err, "without a trusted digest the traversal must run and fail")
}
