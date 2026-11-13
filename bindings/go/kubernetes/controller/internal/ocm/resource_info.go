package ocm

import (
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

// BuildResourceInfo constructs the status representation of a descriptor resource.
func BuildResourceInfo(res *descriptor.Resource) (*v1alpha1.ResourceInfo, error) {
	raw, err := json.Marshal(res.Access)
	if err != nil {
		return nil, fmt.Errorf("marshaling access spec: %w", err)
	}

	labels, err := convertLabels(res.Labels)
	if err != nil {
		return nil, fmt.Errorf("converting labels: %w", err)
	}

	return &v1alpha1.ResourceInfo{
		Name:          res.Name,
		Type:          res.Type,
		Version:       res.Version,
		ExtraIdentity: res.ExtraIdentity,
		Access:        apiextensionsv1.JSON{Raw: raw},
		Digest:        descriptor.ConvertToV2Digest(res.Digest),
		Labels:        labels,
	}, nil
}

// convertLabels maps descriptor labels to API Label objects.
func convertLabels(in []descriptor.Label) ([]v1alpha1.Label, error) {
	out := make([]v1alpha1.Label, len(in))
	for i, l := range in {
		valueBytes, err := json.Marshal(l.Value)
		if err != nil {
			return nil, fmt.Errorf("marshaling label %q value: %w", l.Name, err)
		}
		out[i] = v1alpha1.Label{
			Name:    l.Name,
			Value:   apiextensionsv1.JSON{Raw: valueBytes},
			Version: l.Version,
			Signing: l.Signing,
		}
	}

	return out, nil
}
