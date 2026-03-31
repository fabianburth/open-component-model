package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
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
}

// FileInputSpec is the input specification for the FileInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type FileInputSpec struct {
	// Path is the path to the file on the local filesystem.
	Path string `json:"path"`
	// MediaType is the optional media type of the file.
	// If not set, it is auto-detected.
	MediaType string `json:"mediaType,omitempty"`
	// Compress indicates whether the file should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`
	// WorkingDirectory is the base directory for resolving relative paths.
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	// OutputPath is the optional directory path to buffer the blob file.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
