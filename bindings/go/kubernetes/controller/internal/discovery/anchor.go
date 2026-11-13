package discovery

import (
	"context"
	"fmt"
	"slices"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// DigestVerifier checks that desc matches digest.
type DigestVerifier func(ctx context.Context, desc *descriptor.Descriptor, digest *v2.Digest) error

// Anchors maps every component version of a Graph to the digest it was
// verified against: the root to the digest of its Component, every other
// component version to the digest of an incoming reference. A component
// version that no digest reaches is absent.
type Anchors map[ComponentKey]*v2.Digest

// Anchor verifies every descriptor of graph against every digest that reaches
// it: the root against rootDigest, every other descriptor against the digest
// each of its incoming references records. A component version reached
// through several references must match all of them.
//
// A missing digest is not an error, as for a Resource whose component version
// is not safely digestible: the gap is returned in unanchored, one message
// per root or reference without a digest. A digest that does not match is an
// error naming the component version and the reference that recorded it.
//
// References are visited in (name, version) order of their parents, so the
// digest an Anchors entry records does not depend on traversal order.
func Anchor(
	ctx context.Context,
	graph Graph,
	root ComponentKey,
	rootDigest *v2.Digest,
	verify DigestVerifier,
) (anchors Anchors, unanchored []string, err error) {
	byKey := make(map[ComponentKey]*descriptor.Descriptor, len(graph.Descriptors))
	for _, d := range graph.Descriptors {
		byKey[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}] = d
	}

	anchors = make(Anchors, len(byKey))
	rootDesc, ok := byKey[root]
	if !ok {
		return nil, nil, fmt.Errorf("root component version %s is not part of the graph", root)
	}
	if complete(rootDigest) {
		if err := verify(ctx, rootDesc, rootDigest); err != nil {
			return nil, nil, fmt.Errorf("root component version %s does not match the digest of its Component: %w", root, err)
		}
		anchors[root] = rootDigest
	} else {
		unanchored = append(unanchored, fmt.Sprintf("root component version %s has no digest", root))
	}

	// One digest per (component version, digest) is verified once, even when
	// several references record it.
	verified := make(map[ComponentKey]map[v2.Digest]struct{}, len(byKey))
	for _, parent := range sortByComponentKey(slices.Clone(graph.Descriptors)) {
		if err := checkContext(ctx); err != nil {
			return nil, nil, err
		}
		parentKey := ComponentKey{Name: parent.Component.Name, Version: parent.Component.Version}
		for i := range parent.Component.References {
			ref := &parent.Component.References[i]
			target := ComponentKey{Name: ref.Component, Version: ref.Version}
			child, ok := byKey[target]
			if !ok {
				return nil, nil, fmt.Errorf("component version %s referenced by %s is not part of the graph", target, parentKey)
			}
			dig := referenceDigest(ref)
			if dig == nil {
				unanchored = append(unanchored, fmt.Sprintf("reference %q of %s to %s has no digest", ref.Name, parentKey, target))
				continue
			}
			if _, done := verified[target][*dig]; !done {
				if err := verify(ctx, child, dig); err != nil {
					return nil, nil, fmt.Errorf("component version %s does not match the digest recorded by reference %q of %s: %w",
						target, ref.Name, parentKey, err)
				}
				if verified[target] == nil {
					verified[target] = map[v2.Digest]struct{}{}
				}
				verified[target][*dig] = struct{}{}
			}
			if _, exists := anchors[target]; !exists {
				anchors[target] = dig
			}
		}
	}

	return anchors, unanchored, nil
}

// referenceDigest returns the digest ref records, or nil if it is incomplete.
func referenceDigest(ref *descriptor.Reference) *v2.Digest {
	dig := descriptor.ConvertToV2Digest(&ref.Digest)
	if !complete(dig) {
		return nil
	}
	return dig
}

func complete(dig *v2.Digest) bool {
	return dig != nil && dig.Value != "" && dig.HashAlgorithm != "" && dig.NormalisationAlgorithm != ""
}
