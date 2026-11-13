package discovery

import (
	"context"
	"fmt"
	"slices"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// DigestVerifier checks that desc matches digest.
type DigestVerifier func(ctx context.Context, desc *descriptor.Descriptor, digest *v2.Digest) error

// AnchorPoint is how a component version is chained to the root: the digest
// it was verified against and the reference path from the root that recorded
// that digest. The root has an empty path.
type AnchorPoint struct {
	Digest *v2.Digest
	// ReferencePath lists the identities of the references followed from the
	// root, the last of which recorded Digest.
	ReferencePath []runtime.Identity
}

// Anchors maps every component version of a Graph that is chained to the
// root by digests to its AnchorPoint. A component version that no unbroken
// chain of digests reaches is absent.
type Anchors map[ComponentKey]AnchorPoint

// Anchor verifies every descriptor of graph against every digest that reaches
// it: the root against rootDigest, every other descriptor against the digest
// each of its incoming references records. A component version reached
// through several references must match all of them.
//
// A component version is anchored only through an unbroken chain of digests
// from the root: a digest recorded by a parent that is itself not anchored
// proves nothing, so its target stays unanchored unless another chain
// reaches it.
//
// A missing or incomplete digest is not an error: the gap is returned in
// unanchored, one message per root or reference without a complete digest,
// so callers decide whether a gap fails. A digest that does not match is an
// error naming the component version and the reference that recorded it.
//
// Anchors are propagated breadth-first from the root, following each
// descriptor's references in order, so every anchored component version gets
// the shortest reference path from the root, independently of the order of
// graph.Descriptors.
func Anchor(
	ctx context.Context,
	graph Graph,
	root ComponentKey,
	rootDigest *v2.Digest,
	verify DigestVerifier,
) (anchors Anchors, unanchored []string, err error) {
	byKey := make(map[ComponentKey]*descriptor.Descriptor, len(graph.Descriptors))
	for _, d := range graph.Descriptors {
		byKey[keyOf(d)] = d
	}

	rootDesc, ok := byKey[root]
	if !ok {
		return nil, nil, fmt.Errorf("root component version %s is not part of the graph", root)
	}
	rootAnchored := completeDigest(rootDigest)
	if rootAnchored {
		if err := verify(ctx, rootDesc, rootDigest); err != nil {
			return nil, nil, fmt.Errorf("root component version %s does not match the digest of its Component: %w", root, err)
		}
	} else {
		unanchored = append(unanchored, fmt.Sprintf("root component version %s has no complete digest", root))
	}

	// Every edge is checked, whether or not its parent is anchored. One digest
	// per (component version, digest) is verified once, even when several
	// references record it.
	verified := make(map[ComponentKey]map[v2.Digest]struct{}, len(byKey))
	for _, parent := range sortByComponentKey(slices.Clone(graph.Descriptors)) {
		if err := checkContext(ctx); err != nil {
			return nil, nil, err
		}
		parentKey := keyOf(parent)
		for i := range parent.Component.References {
			ref := &parent.Component.References[i]
			target := ComponentKey{Name: ref.Component, Version: ref.Version}
			child, ok := byKey[target]
			if !ok {
				return nil, nil, fmt.Errorf("component version %s referenced by %s is not part of the graph", target, parentKey)
			}
			dig := referenceDigest(ref)
			if dig == nil {
				unanchored = append(unanchored, fmt.Sprintf("reference %q of %s to %s has no complete digest", ref.Name, parentKey, target))
				continue
			}
			if _, done := verified[target][*dig]; done {
				continue
			}
			if err := verify(ctx, child, dig); err != nil {
				return nil, nil, fmt.Errorf("component version %s does not match the digest recorded by reference %q of %s: %w",
					target, ref.Name, parentKey, err)
			}
			if verified[target] == nil {
				verified[target] = map[v2.Digest]struct{}{}
			}
			verified[target][*dig] = struct{}{}
		}
	}

	anchors = make(Anchors, len(byKey))
	if rootAnchored {
		anchors[root] = AnchorPoint{Digest: rootDigest}
		propagate(byKey, anchors, root)
	}

	return anchors, unanchored, nil
}

// propagate anchors every component version reachable from root over
// references with a complete digest, breadth-first.
func propagate(byKey map[ComponentKey]*descriptor.Descriptor, anchors Anchors, root ComponentKey) {
	queue := []ComponentKey{root}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		parent := anchors[key]
		for i := range byKey[key].Component.References {
			ref := &byKey[key].Component.References[i]
			dig := referenceDigest(ref)
			if dig == nil {
				continue
			}
			target := ComponentKey{Name: ref.Component, Version: ref.Version}
			if _, done := anchors[target]; done {
				continue
			}
			anchors[target] = AnchorPoint{
				Digest:        dig,
				ReferencePath: append(slices.Clone(parent.ReferencePath), ref.ToIdentity()),
			}
			queue = append(queue, target)
		}
	}
}

func keyOf(d *descriptor.Descriptor) ComponentKey {
	return ComponentKey{Name: d.Component.Name, Version: d.Component.Version}
}

// referenceDigest returns the digest ref records, or nil if it is incomplete.
func referenceDigest(ref *descriptor.Reference) *v2.Digest {
	dig := descriptor.ConvertToV2Digest(&ref.Digest)
	if !completeDigest(dig) {
		return nil
	}
	return dig
}

// completeDigest is the one definition of a digest a component version can be
// verified against: a value together with the hash and normalisation
// algorithms that produced it.
func completeDigest(dig *v2.Digest) bool {
	return dig != nil && dig.Value != "" && dig.HashAlgorithm != "" && dig.NormalisationAlgorithm != ""
}
