package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const FileInputType = "FileInput"

// FileInput is a transformer specification to read a file from the local
// filesystem and buffer it to an output file for downstream consumption.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type FileInput struct {
	// +ocm:jsonschema-gen:enum=FileInput/v1alpha1
	Type   runtime.Type     `json:"type"`
	ID     string           `json:"id"`
	Spec   *FileInputSpec   `json:"spec"`
	Output *FileInputOutput `json:"output,omitempty"`
}

// FileInputOutput is the output specification of the FileInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type FileInputOutput struct {
	// File is the file access specification for the produced blob.
	File v1alpha1.File `json:"file"`
	// Resource is the v2-compatible resource descriptor produced by the transformation.
	Resource *v2.Resource `json:"resource,omitempty"`
}

// FileInputSpec is the input specification for the FileInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type FileInputSpec struct {
	// Resource is the resource descriptor this input belongs to.
	// The input-specific attributes (path, mediaType, compress) are carried in Resource.Input.
	Resource *constructorv1.Resource `json:"resource,omitempty"`
	// WorkingDirectory is the base directory for resolving relative paths.
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	// OutputPath is the optional directory path to buffer the blob file.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
