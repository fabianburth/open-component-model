package v4common_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/internal/v4common"
)

func TestMapResourcesWithAccessType(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "valid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
			},
		},
		{
			name: "invalid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123, // invalid type
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123,
				},
				"digest": "test",
			},
		},
		{
			name: "missing access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
		},
		{
			name: "missing access",
			input: map[string]interface{}{
				"digest": "test",
			},
			expected: map[string]interface{}{
				"digest": "test",
			},
		},
		{
			name: "nil access",
			input: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v4common.MapResourcesWithAccessType(v4common.IsNoneAccessKind, func(v interface{}) interface{} {
				m := v.(map[string]interface{})
				delete(m, "digest")
				return m
			}, tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMapResourcesWithNoneAccess(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "valid none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
			},
		},
		{
			name: "valid legacy none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
			},
		},
		{
			name: "invalid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123, // invalid type
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123,
				},
				"digest": "test",
			},
		},
		{
			name: "missing access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
		},
		{
			name: "missing access",
			input: map[string]interface{}{
				"digest": "test",
			},
			expected: map[string]interface{}{
				"digest": "test",
			},
		},
		{
			name: "nil access",
			input: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v4common.MapResourcesWithNoneAccess(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIsNoneAccessKind(t *testing.T) {
	assert.True(t, v4common.IsNoneAccessKind(v4common.NoneType))
	assert.True(t, v4common.IsNoneAccessKind(v4common.NoneLegacyType))
	assert.False(t, v4common.IsNoneAccessKind("ociArtifact"))
}
