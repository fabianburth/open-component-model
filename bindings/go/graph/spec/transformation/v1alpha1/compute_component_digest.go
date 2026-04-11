package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ComputeComponentDigestType = "ComputeComponentDigest"

// ComputeComponentDigest is a transformer specification that computes the
// normalised digest of a component descriptor. This is needed when a component
// B references component A and B's descriptor needs A's digest.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type ComputeComponentDigest struct {
	// +ocm:jsonschema-gen:enum=ComputeComponentDigest/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id"`
	Spec   *ComputeComponentDigestSpec   `json:"spec"`
	Output *ComputeComponentDigestOutput `json:"output,omitempty"`
}

// ComputeComponentDigestSpec is the input specification for the ComputeComponentDigest transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ComputeComponentDigestSpec struct {
	// Descriptor is the finalized v2 component descriptor to compute the digest for.
	Descriptor *v2.Descriptor `json:"descriptor"`
	// ExpectedDigest is an optional digest to verify against the computed digest.
	// If set, the transformation will fail with a digest mismatch error when the
	// computed digest does not match the expected one.
	ExpectedDigest *v2.Digest `json:"expectedDigest,omitempty"`
}

// ComputeComponentDigestOutput is the output specification of the ComputeComponentDigest transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ComputeComponentDigestOutput struct {
	// Digest is the computed digest of the component descriptor.
	Digest v2.Digest `json:"digest"`
}
