package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ProcessOCIResourceDigestType = "ProcessOCIResourceDigest"

// ProcessOCIResourceDigest is a transformer specification that processes
// the digest of an OCI resource. It resolves the manifest digest from
// the OCI registry, sets resource.digest, and pins the imageReference
// to include the resolved digest.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type ProcessOCIResourceDigest struct {
	// +ocm:jsonschema-gen:enum=ProcessOCIResourceDigest/v1alpha1
	Type   runtime.Type                    `json:"type"`
	ID     string                          `json:"id"`
	Spec   *ProcessOCIResourceDigestSpec   `json:"spec"`
	Output *ProcessOCIResourceDigestOutput `json:"output,omitempty"`
}

// ProcessOCIResourceDigestSpec is the input specification for the
// ProcessOCIResourceDigest transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ProcessOCIResourceDigestSpec struct {
	// Resource is the resource descriptor whose digest should be processed.
	Resource *v2.Resource `json:"resource"`
}

// ProcessOCIResourceDigestOutput is the output specification of the
// ProcessOCIResourceDigest transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ProcessOCIResourceDigestOutput struct {
	// Resource is the resource descriptor with digest set and access pinned.
	Resource *v2.Resource `json:"resource"`
}
