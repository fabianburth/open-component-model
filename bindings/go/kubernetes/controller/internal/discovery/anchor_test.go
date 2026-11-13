package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

func testDigest(value string) *v2.Digest {
	return &v2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v4alpha1", Value: value}
}

func digestedReference(name, component, version, value string) descriptor.Reference {
	ref := newReference(name, component, version)
	ref.Digest = *descriptor.ConvertFromV2Digest(testDigest(value))
	return ref
}

// verifyByName accepts a digest whose value is the component name.
func verifyByName(_ context.Context, desc *descriptor.Descriptor, dig *v2.Digest) error {
	if dig.Value != desc.Component.Name {
		return errors.New("digest mismatch")
	}
	return nil
}

func TestAnchor(t *testing.T) {
	root := ComponentKey{Name: "root", Version: "1.0.0"}

	t.Run("anchors every component version to a digest that reaches it", func(t *testing.T) {
		r := require.New(t)
		graph := Graph{Descriptors: []*descriptor.Descriptor{
			newDescriptor("shared", "1.0.0"),
			newDescriptor("b", "1.0.0", withReferences(digestedReference("shared", "shared", "1.0.0", "shared"))),
			newDescriptor("a", "1.0.0", withReferences(digestedReference("shared", "shared", "1.0.0", "shared"))),
			newDescriptor("root", "1.0.0", withReferences(
				digestedReference("b", "b", "1.0.0", "b"),
				digestedReference("a", "a", "1.0.0", "a"),
			)),
		}}

		anchors, unanchored, err := Anchor(t.Context(), graph, root, testDigest("root"), verifyByName)
		r.NoError(err)
		r.Empty(unanchored)
		r.Equal(Anchors{
			root:                               testDigest("root"),
			{Name: "a", Version: "1.0.0"}:      testDigest("a"),
			{Name: "b", Version: "1.0.0"}:      testDigest("b"),
			{Name: "shared", Version: "1.0.0"}: testDigest("shared"),
		}, anchors)
		r.Equal("b", graph.Descriptors[1].Component.Name, "the graph is not reordered")
	})

	t.Run("reports every missing digest", func(t *testing.T) {
		r := require.New(t)
		graph := Graph{Descriptors: []*descriptor.Descriptor{
			newDescriptor("child", "1.0.0"),
			newDescriptor("root", "1.0.0", withReferences(newReference("child", "child", "1.0.0"))),
		}}

		anchors, unanchored, err := Anchor(t.Context(), graph, root, nil, verifyByName)
		r.NoError(err)
		r.Empty(anchors)
		r.Equal([]string{
			"root component version root:1.0.0 has no digest",
			`reference "child" of root:1.0.0 to child:1.0.0 has no digest`,
		}, unanchored)
	})

	t.Run("fails on any mismatching incoming reference", func(t *testing.T) {
		r := require.New(t)
		graph := Graph{Descriptors: []*descriptor.Descriptor{
			newDescriptor("shared", "1.0.0"),
			newDescriptor("a", "1.0.0", withReferences(digestedReference("shared", "shared", "1.0.0", "shared"))),
			newDescriptor("b", "1.0.0", withReferences(digestedReference("shared", "shared", "1.0.0", "tampered"))),
			newDescriptor("root", "1.0.0", withReferences(
				digestedReference("a", "a", "1.0.0", "a"),
				digestedReference("b", "b", "1.0.0", "b"),
			)),
		}}

		_, _, err := Anchor(t.Context(), graph, root, testDigest("root"), verifyByName)
		r.ErrorContains(err, `component version shared:1.0.0 does not match the digest recorded by reference "shared" of b:1.0.0`)
	})

	t.Run("fails on a root that does not match its Component", func(t *testing.T) {
		_, _, err := Anchor(t.Context(), Graph{Descriptors: []*descriptor.Descriptor{newDescriptor("root", "1.0.0")}},
			root, testDigest("other"), verifyByName)
		require.ErrorContains(t, err, "root component version root:1.0.0 does not match")
	})
}
