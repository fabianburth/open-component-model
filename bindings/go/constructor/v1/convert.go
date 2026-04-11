package v1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConvertResourceToV2 converts a constructor v1 Resource to a v2 Resource.
// If the resource has no access but has input, a localBlob/v1 placeholder
// access is injected so that downstream transformers (e.g. AddLocalResource)
// can use the resource directly.
func ConvertResourceToV2(resource *Resource) *v2.Resource {
	if resource == nil {
		return nil
	}

	res := &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{
				Name:    resource.Name,
				Version: resource.Version,
				Labels:  convertLabelsToV2(resource.Labels),
			},
			ExtraIdentity: runtime.Identity(resource.ExtraIdentity),
		},
		Type:     resource.Type,
		Relation: v2.ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		res.SourceRefs = make([]v2.SourceRef, len(resource.SourceRefs))
		for i, ref := range resource.SourceRefs {
			res.SourceRefs[i] = v2.SourceRef{
				IdentitySelector: ref.IdentitySelector,
				Labels:           convertLabelsToV2(ref.Labels),
			}
		}
	}

	switch {
	case resource.Access != nil:
		res.Access = resource.Access.DeepCopy()
	case resource.Input != nil:
		res.Access = &runtime.Raw{
			Type: runtime.NewVersionedType("localBlob", "v1"),
			Data: []byte(`{"type":"localBlob/v1"}`),
		}
	}

	return res
}

func convertLabelsToV2(labels []Label) []v2.Label {
	if labels == nil {
		return nil
	}
	out := make([]v2.Label, len(labels))
	for i, l := range labels {
		out[i] = v2.Label{
			Name:    l.Name,
			Value:   l.Value,
			Version: l.Version,
			Signing: l.Signing,
		}
	}
	return out
}
