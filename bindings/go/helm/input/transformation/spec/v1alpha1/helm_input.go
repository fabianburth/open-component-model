package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
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
	// Resource is the resource descriptor populated when the chart is fetched from a remote repository.
	Resource *v2.Resource `json:"resource,omitempty"`
}

// HelmInputSpec is the input specification for the HelmInput transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HelmInputSpec struct {
	// Path is the path to the directory or tgz file containing the chart on the local filesystem.
	Path string `json:"path,omitempty"`
	// Repository is an OCI reference specifying the upload location of the fetched chart.
	// The reference MUST contain a version tag, and it needs to equal the version of the chart.
	Repository string `json:"repository,omitempty"`
	// HelmRepository specifies the download location of the helm chart. It can either be a URL or an OCI reference.
	HelmRepository string `json:"helmRepository,omitempty"`
	// Version is the version of the chart to download from the remote repository.
	Version string `json:"version,omitempty"`
	// CACert is used in combination with HelmRepository to specify a TLS root certificate.
	CACert string `json:"caCert,omitempty"`
	// CACertFile is used in combination with HelmRepository to specify a relative filename for TLS root certificate.
	CACertFile string `json:"caCertFile,omitempty"`
	// OutputPath is the optional directory path to buffer the blob file.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
