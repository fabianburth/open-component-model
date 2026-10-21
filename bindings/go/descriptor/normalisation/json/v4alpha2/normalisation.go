package v4alpha2

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
// v4alpha2 is the successor to jsonNormalisation/v4alpha1. It shares the JCS
// baseline and the bulk of the ExclusionRules, but deliberately changes two
// label interpretation rules (see LabelExcludes and IgnoreLabelsWithoutSignature).
// Those changes alter the normalised bytes for the same stored descriptor, so
// they live in a new algorithm version rather than mutating v4alpha1.
const Algorithm = "jsonNormalisation/v4alpha2"

// ExclusionRules defines which fields to exclude from the normalised output.
// This map specifies the exclusion rules for different parts of the component descriptor.
//
// The meta section is completely excluded.
//
// For the component section:
// - repositoryContexts are excluded
// - provider is specially handled with a custom mapper to always be serialized as a map
// - labels use special label exclusion rules and respect if the signing attribute is explicitly false
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
// It excludes only labels whose signing attribute is explicitly false (see
// IgnoreLabelsWithoutSignature for the default-signed interpretation and its
// omitempty caveat).
//
// Unlike v4alpha1, "version" IS included here: v4alpha2 re-introduces
// Label.Version into the normalised label bytes. The runtime<->v2 conversion
// preserves Label.Version, so including it lets a label's specification version
// participate in the signature again. This is one of the two deliberate,
// signature-breaking differences from v4alpha1; it is why v4alpha2 is a new
// version and not an edit to v4alpha1 (see doc.go).
var LabelExcludes = jcs.ExcludeEmpty{
	TransformationRules: jcs.DynamicArrayExcludes{
		ValueChecker: IgnoreLabelsWithoutSignature,
		Continue: jcs.MapIncludes{
			"name":    jcs.NoExcludes{},
			"value":   jcs.NoExcludes{},
			"signing": jcs.NoExcludes{},
			"version": jcs.NoExcludes{},
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

// IgnoreLabelsWithoutSignature reports whether a label should be excluded from
// normalization.
//
// v4alpha2 treats labels as default-signed (Option 2): an ABSENT signing
// attribute means signing:true, so the label is kept. Only an explicit
// signing:false excludes it. This reverses v4alpha1, which excluded any label
// that was not explicitly signing:true.
//
// Caveat (omitempty): v2.Label.Signing is `bool json:"signing,omitempty"`, so a
// false value and an absent attribute both serialise to NO "signing" key. By
// the time the descriptor reaches this checker it has been round-tripped
// through that JSON representation, so the checker cannot observe an explicit
// signing:false coming from a typed descriptor — it only ever sees the key
// present-and-true or absent. The explicit-false branch below therefore only
// fires for raw maps that carry signing:false directly (e.g. inputs that never
// passed through the v2 type, as some unit tests do). In practice, every label
// on a stored descriptor is kept under v4alpha2.
func IgnoreLabelsWithoutSignature(v interface{}) bool {
	if m, ok := v.(map[string]interface{}); ok {
		if sig, ok := m["signing"]; ok && sig != nil {
			return sig == "false" || sig == false
		}
	}
	return false
}
