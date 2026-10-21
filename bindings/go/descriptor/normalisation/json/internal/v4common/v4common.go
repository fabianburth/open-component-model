// Package v4common holds the frozen, version-agnostic byte mechanics shared by
// the jsonNormalisation/v4alphaN family (v4alpha1, v4alpha2, ...). It is
// internal and deliberately immutable: its output is part of every importing
// version's signature contract, so a change here moves the normalised bytes —
// and therefore breaks existing signatures — for ALL of them at once.
//
// Only put helpers here that every v4 version must compute identically
// (provider mapping, none-access digest stripping, component defaulting).
// Anything a version interprets differently (e.g. label signing/version rules)
// stays in that version's own package so versions can diverge without touching
// shared code.
package v4common

import (
	"encoding/json"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Constants for "none" access types.
const (
	NoneType       = "none"
	NoneLegacyType = "None"
)

// DefaultComponent sets default values for various fields in the v2 descriptor
// if they are not already set. This ensures consistent normalization output
// regardless of whether optional fields are present in the input.
//
// Parameters:
//   - d: The v2 descriptor to set defaults for
func DefaultComponent(d *v2.Descriptor) {
	if d.Component.RepositoryContexts == nil {
		d.Component.RepositoryContexts = make([]*runtime.Raw, 0)
	}
	if d.Component.References == nil {
		d.Component.References = make([]v2.Reference, 0)
	}
	if d.Component.Sources == nil {
		d.Component.Sources = make([]v2.Source, 0)
	}
	if d.Component.Resources == nil {
		d.Component.Resources = make([]v2.Resource, 0)
	}
}

// ProviderAsMap converts a provider string into a map structure if possible.
// This function is used during normalization to handle provider information
// in a standardized way.
// ProviderAsMap tries to parse a JSON-encoded provider string into a map.
func ProviderAsMap(v any) any {
	var provider map[string]any

	switch v := v.(type) {
	case []byte:
		if err := json.Unmarshal(v, &provider); err == nil {
			return provider
		}
	case string:
		if err := json.Unmarshal([]byte(v), &provider); err == nil {
			return provider
		}
	}

	return map[string]any{
		"name": v,
	}
}

// MapResourcesWithNoneAccess maps resources with "none" access, removing the digest.
// This is used to handle special cases where resources with no access type
// should have their digest removed during normalization.
func MapResourcesWithNoneAccess(v interface{}) interface{} {
	return MapResourcesWithAccessType(
		IsNoneAccessKind,
		func(v interface{}) interface{} {
			m := v.(map[string]interface{})
			delete(m, "digest")
			return m
		},
		v,
	)
}

// MapResourcesWithAccessType applies a mapper function if the access type matches.
// This is used to transform resources based on their access type.
func MapResourcesWithAccessType(test func(string) bool, mapper func(interface{}) interface{}, v interface{}) interface{} {
	access, ok := v.(map[string]interface{})["access"]
	if !ok || access == nil {
		return v
	}
	typ, ok := access.(map[string]interface{})["type"]
	if !ok || typ == nil {
		return v
	}
	if s, ok := typ.(string); ok && test(s) {
		return mapper(v)
	}
	return v
}

// IsNoneAccessKind checks if the given access type is "none".
// Supports both current and legacy access type values.
func IsNoneAccessKind(kind string) bool {
	return kind == NoneType || kind == NoneLegacyType
}
