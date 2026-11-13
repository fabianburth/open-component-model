package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
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
			root:                          {Digest: testDigest("root")},
			{Name: "a", Version: "1.0.0"}: {Digest: testDigest("a"), ReferencePath: []runtime.Identity{{"name": "a", "version": "1.0.0"}}},
			{Name: "b", Version: "1.0.0"}: {Digest: testDigest("b"), ReferencePath: []runtime.Identity{{"name": "b", "version": "1.0.0"}}},
			// Breadth-first in reference order: root lists b before a.
			{Name: "shared", Version: "1.0.0"}: {Digest: testDigest("shared"), ReferencePath: []runtime.Identity{
				{"name": "b", "version": "1.0.0"},
				{"name": "shared", "version": "1.0.0"},
			}},
		}, anchors)
		r.Equal("b", graph.Descriptors[1].Component.Name, "the graph is not reordered")
	})

	t.Run("records the shortest reference path, with extra identity", func(t *testing.T) {
		r := require.New(t)
		deep := digestedReference("leaf", "leaf", "1.0.0", "leaf")
		deep.ExtraIdentity = map[string]string{"platform": "linux"}
		graph := Graph{Descriptors: []*descriptor.Descriptor{
			newDescriptor("leaf", "1.0.0"),
			newDescriptor("mid", "1.0.0", withReferences(deep)),
			newDescriptor("root", "1.0.0", withReferences(
				digestedReference("mid", "mid", "1.0.0", "mid"),
				digestedReference("direct", "leaf", "1.0.0", "leaf"),
			)),
		}}

		anchors, unanchored, err := Anchor(t.Context(), graph, root, testDigest("root"), verifyByName)
		r.NoError(err)
		r.Empty(unanchored)
		r.Equal([]runtime.Identity{{"name": "direct", "version": "1.0.0"}},
			anchors[ComponentKey{Name: "leaf", Version: "1.0.0"}].ReferencePath)
	})

	t.Run("does not propagate an anchor from an unanchored parent", func(t *testing.T) {
		r := require.New(t)
		graph := Graph{Descriptors: []*descriptor.Descriptor{
			newDescriptor("b", "1.0.0"),
			// a is reached only through a reference without a digest: unverified.
			newDescriptor("a", "1.0.0", withReferences(digestedReference("b", "b", "1.0.0", "b"))),
			newDescriptor("root", "1.0.0", withReferences(newReference("a", "a", "1.0.0"))),
		}}

		anchors, unanchored, err := Anchor(t.Context(), graph, root, testDigest("root"), verifyByName)
		r.NoError(err)
		r.Equal([]string{`reference "a" of root:1.0.0 to a:1.0.0 has no complete digest`}, unanchored)
		r.NotContains(anchors, ComponentKey{Name: "a", Version: "1.0.0"})
		r.NotContains(anchors, ComponentKey{Name: "b", Version: "1.0.0"},
			"b's digest comes from unverified a, so b must not look anchored")
	})

	t.Run("anchors nothing below a root without a digest", func(t *testing.T) {
		r := require.New(t)
		graph := Graph{Descriptors: []*descriptor.Descriptor{
			newDescriptor("child", "1.0.0"),
			newDescriptor("root", "1.0.0", withReferences(digestedReference("child", "child", "1.0.0", "child"))),
		}}

		anchors, unanchored, err := Anchor(t.Context(), graph, root, nil, verifyByName)
		r.NoError(err)
		r.Empty(anchors)
		r.Equal([]string{"root component version root:1.0.0 has no complete digest"}, unanchored)
	})

	t.Run("treats a digest without algorithms as missing, as DigestsComplete does", func(t *testing.T) {
		r := require.New(t)
		ref := newReference("child", "child", "1.0.0")
		ref.Digest.Value = "child" // no hash / normalisation algorithm
		graph := Graph{Descriptors: []*descriptor.Descriptor{
			newDescriptor("child", "1.0.0"),
			newDescriptor("root", "1.0.0", withReferences(ref)),
		}}

		_, unanchored, err := Anchor(t.Context(), graph, root, testDigest("root"), verifyByName)
		r.NoError(err)
		r.NotEmpty(unanchored)
		r.False(graph.DigestsComplete(), "an unanchored graph must not be treated as fully digested")
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
			"root component version root:1.0.0 has no complete digest",
			`reference "child" of root:1.0.0 to child:1.0.0 has no complete digest`,
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

func TestReferencePaths(t *testing.T) {
	r := require.New(t)
	root := ComponentKey{Name: "root", Version: "1.0.0"}
	graph := Graph{Descriptors: []*descriptor.Descriptor{
		newDescriptor("leaf", "1.0.0"),
		newDescriptor("unanchored", "1.0.0", withReferences(digestedReference("leaf", "leaf", "1.0.0", "leaf"))),
		newDescriptor("anchored", "1.0.0", withReferences(digestedReference("leaf", "leaf", "1.0.0", "leaf"))),
		newDescriptor("root", "1.0.0", withReferences(
			newReference("unanchored", "unanchored", "1.0.0"),
			digestedReference("anchored", "anchored", "1.0.0", "anchored"),
		)),
	}}

	anchors, _, err := Anchor(t.Context(), graph, root, testDigest("root"), verifyByName)
	r.NoError(err)
	r.Equal(map[ComponentKey][]runtime.Identity{
		root:                                   nil,
		{Name: "unanchored", Version: "1.0.0"}: {{"name": "unanchored", "version": "1.0.0"}},
		{Name: "anchored", Version: "1.0.0"}:   {{"name": "anchored", "version": "1.0.0"}},
		// The anchoring path wins over the first path breadth-first.
		{Name: "leaf", Version: "1.0.0"}: {{"name": "anchored", "version": "1.0.0"}, {"name": "leaf", "version": "1.0.0"}},
	}, ReferencePaths(graph, root, anchors))
}
