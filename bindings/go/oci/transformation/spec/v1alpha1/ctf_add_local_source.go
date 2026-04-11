package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFAddLocalSourceType = "CTFAddLocalSource"

// CTFAddLocalSource is a transformer specification to add a local source
// blob to a component version in a CTF repository.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFAddLocalSource struct {
	// +ocm:jsonschema-gen:enum=CTFAddLocalSource/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *CTFAddLocalSourceSpec   `json:"spec"`
	Output *CTFAddLocalSourceOutput `json:"output,omitempty"`
}

// CTFAddLocalSourceOutput is the output specification of the
// CTFAddLocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddLocalSourceOutput struct {
	// Source is the updated source descriptor with populated LocalReference
	Source *v2.Source `json:"source"`
}

// CTFAddLocalSourceSpec is the input specification for the
// CTFAddLocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddLocalSourceSpec struct {
	// Repository is the CTF repository specification
	Repository ctf.Repository `json:"repository"`
	// Component is the component name to add the source to.
	Component string `json:"component"`
	// Version is the component version to add the source to.
	Version string `json:"version"`
	// Source is the source descriptor to add.
	// If the Source contains an access specification, it may be used
	// by the underlying implementation to derive metadata to avoid additional compute
	// (such as digest information) or to steer implementation (such as a reference name)
	Source *v2.Source `json:"source"`
	// File is the access specification to the data that should be added
	File v1alpha1.File `json:"file"`
}
