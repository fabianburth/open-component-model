package v1alpha1

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const UTF8InputType = "UTF8Input"

// UTF8Input is a transformer specification to produce a blob from inline UTF-8
// content (text, JSON, formatted JSON or YAML) and buffer it to an output file
// for downstream consumption.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type UTF8Input struct {
	// +ocm:jsonschema-gen:enum=UTF8Input/v1alpha1
	Type   runtime.Type     `json:"type"`
	ID     string           `json:"id"`
	Spec   *UTF8InputSpec   `json:"spec"`
	Output *UTF8InputOutput `json:"output,omitempty"`
}

// UTF8InputOutput is the output specification of the UTF8Input transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type UTF8InputOutput struct {
	// File is the file access specification for the produced blob.
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor this input belongs to.
	Resource *v2.Resource `json:"resource,omitempty"`
}

// UTF8InputSpec is the input specification for the UTF8Input transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type UTF8InputSpec struct {
	// Resource is the resource descriptor this input belongs to.
	Resource *v2.Resource `json:"resource,omitempty"`
	// Text is a UTF-8 string, raw encoded.
	Text string `json:"text,omitempty"`
	// JSON is a JSON object, raw encoded via UTF-8.
	JSON json.RawMessage `json:"json,omitempty"`
	// FormattedJSON is a JSON object, raw encoded via UTF-8, with default indentation applied.
	FormattedJSON json.RawMessage `json:"formattedJson,omitempty"`
	// YAML is a YAML object, raw encoded via UTF-8.
	YAML json.RawMessage `json:"yaml,omitempty"`
	// Compress indicates whether the content should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`
	// OutputPath is the optional directory path to buffer the blob file.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
