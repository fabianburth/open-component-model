package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
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
	// Resource is the v2-compatible resource descriptor produced by the transformation.
	Resource *v2.Resource `json:"resource,omitempty"`
}

// UTF8InputSpec is the input specification for the UTF8Input transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type UTF8InputSpec struct {
	// Resource is the resource descriptor this input belongs to.
	// The input-specific attributes (text, json, formattedJson, yaml, compress) are carried in Resource.Input.
	Resource *constructorv1.Resource `json:"resource,omitempty"`
	// OutputPath is the optional directory path to buffer the blob file.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
