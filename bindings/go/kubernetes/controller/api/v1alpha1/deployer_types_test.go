package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeployerObjectRefs(t *testing.T) {
	t.Run("Deployer with resourceRef", func(t *testing.T) {
		r := require.New(t)
		deployer := &Deployer{Spec: DeployerSpec{ResourceRef: ObjectKey{Namespace: "ns", Name: "resource"}}}
		r.Equal(ObjectKey{Namespace: "ns", Name: "resource"}, deployer.GetResourceRef())
		r.Nil(deployer.GetDiscoveryRef())
	})

	t.Run("Deployer with discoveryRef", func(t *testing.T) {
		r := require.New(t)
		deployer := &Deployer{Spec: DeployerSpec{DiscoveryRef: &ObjectKey{Namespace: "ns", Name: "discovery"}}}
		r.Equal(ObjectKey{}, deployer.GetResourceRef())
		r.Equal(&ObjectKey{Namespace: "ns", Name: "discovery"}, deployer.GetDiscoveryRef())
	})

	t.Run("NamespacedDeployer with resourceRef", func(t *testing.T) {
		r := require.New(t)
		deployer := &NamespacedDeployer{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns"},
			Spec:       NamespacedDeployerSpec{ResourceRef: corev1.LocalObjectReference{Name: "resource"}},
		}
		r.Equal(ObjectKey{Namespace: "ns", Name: "resource"}, deployer.GetResourceRef())
		r.Nil(deployer.GetDiscoveryRef())
	})

	t.Run("NamespacedDeployer with discoveryRef", func(t *testing.T) {
		r := require.New(t)
		deployer := &NamespacedDeployer{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns"},
			Spec:       NamespacedDeployerSpec{DiscoveryRef: &corev1.LocalObjectReference{Name: "discovery"}},
		}
		r.Equal(&ObjectKey{Namespace: "ns", Name: "discovery"}, deployer.GetDiscoveryRef())
	})
}

// TestDeployerSpecRefPresence verifies that the resourceRef/discoveryRef CEL
// exclusivity rule sees exactly what a Go client set: a zero resourceRef is
// omitted.
func TestDeployerSpecRefPresence(t *testing.T) {
	r := require.New(t)

	data, err := json.Marshal(&DeployerSpec{DiscoveryRef: &ObjectKey{Name: "discovery"}})
	r.NoError(err)
	r.NotContains(string(data), "resourceRef")
	r.Contains(string(data), `"discoveryRef":{"name":"discovery"}`)

	data, err = json.Marshal(&NamespacedDeployerSpec{DiscoveryRef: &corev1.LocalObjectReference{Name: "discovery"}})
	r.NoError(err)
	r.NotContains(string(data), "resourceRef")
	r.Contains(string(data), `"discoveryRef":{"name":"discovery"}`)

	data, err = json.Marshal(&NamespacedDeployerSpec{ResourceRef: corev1.LocalObjectReference{Name: "resource"}})
	r.NoError(err)
	r.Contains(string(data), `"resourceRef":{"name":"resource"}`)
	r.NotContains(string(data), "discoveryRef")
}
