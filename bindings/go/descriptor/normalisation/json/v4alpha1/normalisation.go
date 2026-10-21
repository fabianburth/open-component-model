package v4alpha1

import (
	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/internal/v4common"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Algorithm is the registered name for this normalisation algorithm.
// It is used to identify and retrieve this specific normalisation implementation
// from the normalisation registry.
//
// This algorithm is defined as the "spiritual successor" to jsonNormalisation/v3.
//
// # The baseline of this algorithm is a serialized v2 component descriptor that is normalised using jcs.Normalise
//
// Special Rules are applied to the descriptor which can be observed in ExclusionRules
const Algorithm = "jsonNormalisation/v4alpha1"

// ExclusionRules defines which fields to exclude from the normalised output.
// This map specifies the exclusion rules for different parts of the component descriptor.
//
// The meta section is completely excluded.
//
// For the component section:
// - repositoryContexts are excluded
// - provider is specially handled with a custom mapper to always be serialized as a map
// - labels use special label exclusion rules and respect if the signing attribute is not set
// - resources use dynamic array excludes with special handling for access and source references:
//   - accesses excluded
//   - srcRefs excluded
//
// - signatures are excluded
// - nestedDigests (digests stored in the parent descriptor for referenced components) are excluded
var ExclusionRules = jcs.MapExcludes{
	"meta": nil,
	"component": jcs.MapExcludes{
		"repositoryContexts": nil,
		"provider": jcs.MapValue{
			Mapping: v4common.ProviderAsMap,
			Continue: jcs.MapExcludes{
				"labels": LabelExcludes,
			},
		},
		"labels": LabelExcludes,
		"resources": jcs.DynamicArrayExcludes{
			ValueMapper: v4common.MapResourcesWithNoneAccess,
			Continue: jcs.MapExcludes{
				"access":  nil,
				"srcRefs": nil,
				"labels":  LabelExcludes,
			},
		},
		"sources": jcs.ArrayExcludes{
			Continue: jcs.MapExcludes{
				"access": nil,
				"labels": LabelExcludes,
			},
		},
		"references": jcs.ArrayExcludes{
			Continue: jcs.MapExcludes{
				"labels": LabelExcludes,
			},
		},
	},
	"signatures":    nil,
	"nestedDigests": nil,
}

// LabelExcludes defines exclusion rules for label entries during normalization.
// It excludes labels that don't have a valid signature.
//
// NOTE: version is intentionally NOT included. The runtime<->v2 label
// conversion only began preserving Label.Version to support
// jsonNormalisation/v1 (which signs it); v4alpha1 never emitted version before,
// because the field was always empty across the conversion. Including it here
// would silently change v4alpha1 digests and break existing signatures over
// versioned signing labels. Keep v4alpha1 byte-stable; signing label version is
// a deliberate, versioned change (see jsonNormalisation/v1 for that behaviour).
var LabelExcludes = jcs.ExcludeEmpty{
	TransformationRules: jcs.DynamicArrayExcludes{
		ValueChecker: IgnoreLabelsWithoutSignature,
		Continue: jcs.MapIncludes{
			"name":    jcs.NoExcludes{},
			"value":   jcs.NoExcludes{},
			"signing": jcs.NoExcludes{},
		},
	},
}

// init registers the normalisation algorithm on package initialization.
// This ensures the algorithm is available in the normalisation registry
// when the package is imported.
func init() {
	norms.Normalisations.Register(Algorithm, algo{})
}

// algo implements the normalisation interface for JSON canonicalization.
// It provides methods to normalize component descriptors into a standardized
// JSON format.
type algo struct{}

// Normalise performs normalisation on the given descriptor using the default type and exclusion rules.
// It converts the descriptor to v2 format, applies default values, and then normalizes
// the JSON representation using the JCS (JSON Canonicalization Scheme) algorithm.
//
// Parameters:
//   - cd: The component descriptor to normalize
//
// Returns:
//   - []byte: The normalized JSON representation of the descriptor
//   - error: Any error that occurred during normalization
func (m algo) Normalise(cd *descruntime.Descriptor) ([]byte, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc, err := descruntime.ConvertToV2(scheme, cd)
	if err != nil {
		return nil, err
	}
	v4common.DefaultComponent(desc)
	return jcs.Normalise(desc, ExclusionRules)
}

// IgnoreLabelsWithoutSignature checks if a label lacks a valid signature and should be ignored.
// Returns true if the label should be excluded from normalization.
func IgnoreLabelsWithoutSignature(v interface{}) bool {
	if m, ok := v.(map[string]interface{}); ok {
		if sig, ok := m["signing"]; ok && sig != nil {
			return sig != "true" && sig != true
		}
	}
	return true
}
