package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCIAddLocalSourceType = "OCIAddLocalSource"

// OCIAddLocalSource is a transformer specification to add a local source
// blob to a component version in an OCI repository.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIAddLocalSource struct {
	// +ocm:jsonschema-gen:enum=OCIAddLocalSource/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *OCIAddLocalSourceSpec   `json:"spec"`
	Output *OCIAddLocalSourceOutput `json:"output,omitempty"`
}

// OCIAddLocalSourceOutput is the output specification of the
// OCIAddLocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddLocalSourceOutput struct {
	// Source is the updated source descriptor with populated LocalReference
	Source *v2.Source `json:"source"`
}

// OCIAddLocalSourceSpec is the input specification for the
// OCIAddLocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddLocalSourceSpec struct {
	// Repository is the OCI repository specification
	Repository oci.Repository `json:"repository"`
	// Component is the component name to add the source to.
	Component string `json:"component"`
	// Version is the component version to add the source to.
	Version string `json:"version"`
	// Source is the source descriptor to add.
	// If the Source contains an access specification, it may be used
	// by the underlying implementation to derive metadata to avoid additional compute
	// (such as digest information) or to steer implementation (such as a reference name)
	Source *v2.Source `json:"source"`
	// File is the access specification to the file that should be added
	File v1alpha1.File `json:"file"`
}
