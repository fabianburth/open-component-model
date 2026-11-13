package discovery

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

var discoveryGVK = schema.GroupVersionKind{
	Group:   v1alpha1.GroupVersion.Group,
	Version: v1alpha1.GroupVersion.Version,
	Kind:    v1alpha1.KindDiscovery,
}

// descriptor is a consumer-shaped v2 component descriptor with arbitrary
// nested JSON (labels, access, references) that must survive the apiserver.
var descriptor = map[string]any{
	"meta": map[string]any{"schemaVersion": "v2"},
	"component": map[string]any{
		"name":     "ocm.software/test",
		"version":  "1.0.0",
		"provider": map[string]any{"name": "ocm.software"},
		"labels": []any{
			map[string]any{
				"name":  "structured-label",
				"value": map[string]any{"nested": map[string]any{"list": []any{1, "two", true}}},
			},
		},
		"resources": []any{
			map[string]any{
				"name":     "image",
				"version":  "1.0.0",
				"type":     "ociImage",
				"relation": "external",
				"access": map[string]any{
					"type":           "ociArtifact",
					"imageReference": "ghcr.io/open-component-model/test:1.0.0",
				},
				"labels": []any{map[string]any{"name": "tier", "value": "platform"}},
			},
		},
		"componentReferences": []any{
			map[string]any{
				"name":          "child",
				"componentName": "ocm.software/child",
				"version":       "2.0.0",
			},
		},
	},
}

func toJSONs(descriptors ...map[string]any) []apiextensionsv1.JSON {
	result := make([]apiextensionsv1.JSON, 0, len(descriptors))
	for _, descriptor := range descriptors {
		raw, err := json.Marshal(descriptor)
		Expect(err).NotTo(HaveOccurred())
		result = append(result, apiextensionsv1.JSON{Raw: raw})
	}
	return result
}

func extractedRecord(values map[string]any) v1alpha1.ExtractedRecord {
	record := make(v1alpha1.ExtractedRecord, len(values))
	for name, value := range values {
		raw, err := json.Marshal(value)
		Expect(err).NotTo(HaveOccurred())
		record[name] = apiextensionsv1.JSON{Raw: raw}
	}
	return record
}

// newUnstructured applies the given spec mutation on a minimal valid Discovery
// expressed as unstructured, bypassing Go client-side serialization
// (omitempty/omitzero) to test server-side admission of explicit values.
func newUnstructured(namespace string, mutate func(spec map[string]any)) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(discoveryGVK)
	obj.SetGenerateName("discovery-")
	obj.SetNamespace(namespace)
	spec := map[string]any{
		"componentRef": map[string]any{"name": "releasechannel"},
	}
	mutate(spec)
	obj.Object["spec"] = spec
	return obj
}

// validSource returns a complete inline source as unstructured.
func validSource() map[string]any {
	return map[string]any{
		"repositorySpec": map[string]any{"type": "OCIRepository", "baseUrl": "ghcr.io/open-component-model"},
		"component":      "ocm.software/test",
		"semver":         ">=1.0.0",
		"interval":       "10m",
	}
}

var _ = Describe("Discovery API", func() {
	var namespace *corev1.Namespace

	BeforeEach(func(ctx SpecContext) {
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "discovery-"},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
	})

	Context("spec admission", func() {
		It("rejects an empty componentRef name", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["componentRef"] = map[string]any{"name": ""}
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring("name must not be empty")))
		})

		It("rejects an empty extract", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{}
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring(
				"exactly one of byResources, byComponents, or expression must be specified")))
		})

		It("rejects multiple extract modes", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{
					"byResources": map[string]any{"name": "resource.name"},
					"expression":  "components",
				}
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring(
				"exactly one of byResources, byComponents, or expression must be specified")))
		})

		It("accepts an explicitly empty map extraction mode", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{"byResources": map[string]any{}}
			})
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			extract, found, err := unstructured.NestedMap(obj.Object, "spec", "extract")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(extract).To(HaveKey("byResources"))

			typed := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), typed)).To(Succeed())
			Expect(typed.Spec.Extract).NotTo(BeNil())
			Expect(typed.Spec.Extract.ByResources).NotTo(BeNil())
			Expect(typed.Spec.Extract.ByResources).To(BeEmpty())
		})

		It("rejects both componentRef and source", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["source"] = validSource()
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring(
				"exactly one of componentRef and source must be specified")))
		})

		It("rejects neither componentRef nor source", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				delete(spec, "componentRef")
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring(
				"exactly one of componentRef and source must be specified")))
		})

		It("accepts an inline source and defaults its downgrade policy", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				delete(spec, "componentRef")
				spec["source"] = validSource()
			})
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			typed := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), typed)).To(Succeed())
			Expect(typed.Spec.ComponentRef.Name).To(BeEmpty())
			Expect(typed.Spec.Source).NotTo(BeNil())
			Expect(typed.Spec.Source.Component).To(Equal("ocm.software/test"))
			Expect(typed.Spec.Source.Semver).To(Equal(">=1.0.0"))
			Expect(typed.Spec.Source.Interval.Duration).To(Equal(10 * time.Minute))
			Expect(typed.Spec.Source.DowngradePolicy).To(Equal(v1alpha1.DowngradePolicyDeny))
			Expect(typed.Spec.Source.RepositorySpec.Raw).To(MatchJSON(`{"type":"OCIRepository","baseUrl":"ghcr.io/open-component-model"}`))
		})

		It("accepts an inline source from a typed client", func(ctx SpecContext) {
			discovery := &v1alpha1.Discovery{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "discovery-", Namespace: namespace.Name},
				Spec: v1alpha1.DiscoverySpec{
					Source: &v1alpha1.DiscoverySource{
						RepositorySpec: &apiextensionsv1.JSON{Raw: []byte(`{"type":"OCIRepository","baseUrl":"ghcr.io/open-component-model"}`)},
						Component:      "ocm.software/test",
						Semver:         ">=1.0.0",
						Interval:       metav1.Duration{Duration: time.Minute},
					},
				},
			}
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
		})

		DescribeTable("rejects an inline source missing a required field",
			func(ctx SpecContext, field string) {
				obj := newUnstructured(namespace.Name, func(spec map[string]any) {
					delete(spec, "componentRef")
					source := validSource()
					delete(source, field)
					spec["source"] = source
				})
				err := k8sClient.Create(ctx, obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(field))
			},
			Entry("repositorySpec", "repositorySpec"),
			Entry("component", "component"),
			Entry("semver", "semver"),
			Entry("interval", "interval"),
		)

		It("rejects an unknown inline source downgrade policy", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				delete(spec, "componentRef")
				source := validSource()
				source["downgradePolicy"] = "Sometimes"
				spec["source"] = source
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring("downgradePolicy")))
		})

		It("defaults the verification policy to Always", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(map[string]any) {})
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			typed := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), typed)).To(Succeed())
			Expect(typed.Spec.VerificationPolicy).To(Equal(v1alpha1.VerificationPolicyAlways))
		})

		It("accepts the verification policy Never", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["verificationPolicy"] = string(v1alpha1.VerificationPolicyNever)
			})
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			typed := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), typed)).To(Succeed())
			Expect(typed.Spec.VerificationPolicy).To(Equal(v1alpha1.VerificationPolicyNever))
		})

		It("rejects an unknown verification policy", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["verificationPolicy"] = "Sometimes"
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring("verificationPolicy")))
		})

		It("rejects an explicitly empty extraction expression", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{"expression": ""}
			})
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expression"))
		})
	})

	// Status payloads are opaque, consumer-shaped JSON. Without the
	// XPreserveUnknownFields markers the apiserver silently prunes them.
	Context("status admission", func() {
		var discovery *v1alpha1.Discovery

		BeforeEach(func(ctx SpecContext) {
			discovery = &v1alpha1.Discovery{
				GenerateName: "discovery-",
				Namespace:    namespace.Name,
				Spec: v1alpha1.DiscoverySpec{
					ComponentRef: corev1.LocalObjectReference{Name: "releasechannel"},
				},
			}
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
		})

		It("preserves consumer-shaped arbitrary nested JSON", func(ctx SpecContext) {
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fetched)).To(Succeed())
			fetched.Status.Components = toJSONs(descriptor)
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			after := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), after)).To(Succeed())
			Expect(after.Status.Components).To(HaveLen(1))

			raw, err := json.Marshal(descriptor)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Status.Components[0].Raw).To(MatchJSON(raw))
		})

		It("preserves extracted records with arbitrary nested JSON", func(ctx SpecContext) {
			values := map[string]any{
				"imageRef": "ghcr.io/open-component-model/test:1.0.0",
				"metadata": map[string]any{
					"platforms": []any{"linux/amd64", "linux/arm64"},
					"signed":    true,
				},
				"optional": nil,
			}
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fetched)).To(Succeed())
			fetched.Status.Extracted = []v1alpha1.ExtractedRecord{
				extractedRecord(values),
				{},
			}
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			after := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), after)).To(Succeed())
			Expect(after.Status.Extracted).To(HaveLen(2))
			Expect(after.Status.Extracted[1]).To(BeEmpty())
			raw, err := json.Marshal(after.Status.Extracted[0])
			Expect(err).NotTo(HaveOccurred())
			expected, err := json.Marshal(values)
			Expect(err).NotTo(HaveOccurred())
			Expect(raw).To(MatchJSON(expected))
		})

		It("preserves resources independently of components and extracted", func(ctx SpecContext) {
			resources := []v1alpha1.DiscoveredResource{{
				Component: v1alpha1.ComponentInfo{
					RepositorySpec: &apiextensionsv1.JSON{Raw: []byte(`{"type":"OCIRepository","baseUrl":"ghcr.io/open-component-model"}`)},
					Component:      "ocm.software/test",
					Version:        "1.0.0",
					Digest: &v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
						Value:                  "abc",
					},
				},
				Resource: v1alpha1.ResourceInfo{
					Name:          "image",
					Type:          "ociImage",
					Version:       "1.0.0",
					ExtraIdentity: map[string]string{"architecture": "amd64"},
					Access:        apiextensionsv1.JSON{Raw: []byte(`{"type":"ociArtifact","imageReference":"ghcr.io/open-component-model/test:1.0.0"}`)},
					Digest: &v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "genericBlobDigest/v1",
						Value:                  "def",
					},
				},
			}}

			for _, payload := range []func(*v1alpha1.DiscoveryStatus){
				func(status *v1alpha1.DiscoveryStatus) { status.Components = toJSONs(descriptor) },
				func(status *v1alpha1.DiscoveryStatus) {
					status.Components = nil
					status.Extracted = []v1alpha1.ExtractedRecord{{}}
				},
			} {
				fetched := &v1alpha1.Discovery{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fetched)).To(Succeed())
				payload(&fetched.Status)
				fetched.Status.Resources = resources
				Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

				after := &v1alpha1.Discovery{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), after)).To(Succeed())
				Expect(after.Status.Resources).To(HaveLen(1))
				Expect(after.Status.Resources[0].Component.Component).To(Equal("ocm.software/test"))
				Expect(after.Status.Resources[0].Component.Digest).To(Equal(resources[0].Component.Digest))
				Expect(after.Status.Resources[0].Component.RepositorySpec.Raw).To(MatchJSON(resources[0].Component.RepositorySpec.Raw))
				Expect(after.Status.Resources[0].Resource.ExtraIdentity).To(Equal(resources[0].Resource.ExtraIdentity))
				Expect(after.Status.Resources[0].Resource.Access.Raw).To(MatchJSON(resources[0].Resource.Access.Raw))
				Expect(after.Status.Resources[0].Resource.Digest).To(Equal(resources[0].Resource.Digest))
			}
		})

		It("rejects mutually exclusive payload fields", func(ctx SpecContext) {
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), fetched)).To(Succeed())
			fetched.Status.Components = toJSONs(descriptor)
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			fetched.Status.Extracted = []v1alpha1.ExtractedRecord{}
			err := k8sClient.Status().Update(ctx, fetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("components and extracted cannot be set at the same time"))
		})
	})
})
