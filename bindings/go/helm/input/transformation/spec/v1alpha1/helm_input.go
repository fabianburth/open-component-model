package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const HelmInputType = "HelmInput"

// HelmInput is a transformer specification to process a Helm chart from a local
// filesystem path or a remote Helm repository and buffer it to an output file
// for downstream consumption.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HelmInput struct {
	// +ocm:jsonschema-gen:enum=HelmInput/v1alpha1
	Type   runtime.Type     `json:"type"`
	ID     string           `json:"id"`
	Spec   *HelmInputSpec   `json:"spec"`
	Output *HelmInputOutput `json:"output,omitempty"`
}

// HelmInputOutput is the output specification of the HelmInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HelmInputOutput struct {
	// File is the file access specification for the produced blob.
	File v1alpha1.File `json:"file"`
	// Resource is the v2-compatible resource descriptor produced by the transformation.
	Resource *v2.Resource `json:"resource,omitempty"`
}

// HelmInputSpec is the input specification for the HelmInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HelmInputSpec struct {
	// Resource is the resource descriptor this input belongs to.
	// The input-specific attributes (path, repository, helmRepository, version, caCert, caCertFile) are carried in Resource.Input.
	Resource *constructorv1.Resource `json:"resource,omitempty"`
	// WorkingDirectory is the base directory for resolving relative paths.
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	// OutputPath is the optional directory path to buffer the blob file.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
