package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const DirInputType = "DirInput"

// DirInput is a transformer specification to read a directory from the local
// filesystem and buffer it to an output file for downstream consumption.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type DirInput struct {
	// +ocm:jsonschema-gen:enum=DirInput/v1alpha1
	Type   runtime.Type    `json:"type"`
	ID     string          `json:"id"`
	Spec   *DirInputSpec   `json:"spec"`
	Output *DirInputOutput `json:"output,omitempty"`
}

// DirInputOutput is the output specification of the DirInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DirInputOutput struct {
	// File is the file access specification for the produced blob.
	File v1alpha1.File `json:"file"`
}

// DirInputSpec is the input specification for the DirInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DirInputSpec struct {
	// Path is the path to the directory on the local filesystem.
	Path string `json:"path"`
	// MediaType is the optional media type of the resulting blob.
	// If not set, it defaults to application/x-tar.
	MediaType string `json:"mediaType,omitempty"`
	// Compress indicates whether the resulting blob should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`
	// PreserveDir defines that the directory specified in the Path field should be included in the resulting blob.
	PreserveDir bool `json:"preserveDir,omitempty"`
	// FollowSymlinks will include the content of the encountered symbolic links to the resulting blob.
	FollowSymlinks bool `json:"followSymlinks,omitempty"`
	// ExcludeFiles is a list of file name patterns to exclude from addition to the resulting blob.
	ExcludeFiles []string `json:"excludeFiles,omitempty"`
	// IncludeFiles is a list of file name patterns to exclusively add to the resulting blob.
	IncludeFiles []string `json:"includeFiles,omitempty"`
	// Reproducible defines that the attributes of the included files have to be normalized.
	Reproducible bool `json:"reproducible,omitempty"`
	// WorkingDirectory is the base directory for resolving relative paths.
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	// OutputPath is the optional directory path to buffer the blob file.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
