package v4alpha2_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// fromYAML loads a stored cd const into the runtime descriptor, mirroring the
// signing flow (v2.Descriptor -> ConvertFromV2).
func fromYAML(t *testing.T, in string) *runtime.Descriptor {
	t.Helper()
	r := require.New(t)
	var desc descriptorv2.Descriptor
	r.NoError(yaml.Unmarshal([]byte(in), &desc))
	rt, err := runtime.ConvertFromV2(&desc)
	r.NoError(err)
	return rt
}

// TestConformance asserts the normalised output of a realistic stored
// descriptor is byte-identical to the pinned v4alpha2 reference bytes. The
// fixture lives in fixtures_test.go.
func TestConformance(t *testing.T) {
	r := require.New(t)
	desc := fromYAML(t, input_cli)

	got, err := normalisation.Normalise(desc, v4alpha2.Algorithm)
	r.NoError(err)
	// Byte-exact, NOT JSONEq: JCS canonical key ordering is part of what we
	// pin, and JSONEq would mask an ordering or spelling regression.
	r.Equal(norm_cli, string(got), "normalised bytes diverge from the pinned v4alpha2 reference") //nolint:testifylint // byte-exact JCS check, JSONEq would mask key-order regressions
}

// TestNormalization verifies that the normalization (using ExclusionRules)
// produces the expected canonical JSON output. Unlike v4alpha1, labels without
// an explicit signing attribute (here: label1) are default-signed and kept.
func TestNormalization(t *testing.T) {
	// YAML input representing a component descriptor.
	inputYAML := `
component:
  componentReferences: null
  name: github.com/vasu1124/introspect
  provider: internal
  repositoryContexts:
  - baseUrl: ghcr.io/vasu1124/ocm
    componentNameMapping: urlPath
    type: ociRegistry
  resources:
  - access:
      localReference: sha256:7f0168496f273c1e2095703a050128114d339c580b0906cd124a93b66ae471e2
      mediaType: application/vnd.docker.distribution.manifest.v2+tar+gzip
      referenceName: vasu1124/introspect:1.0.0
      type: localBlob
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: 6a1c7637a528ab5957ab60edf73b5298a0a03de02a96be0313ee89b22544840c
    labels:
    - name: label1
      value: foo
    - name: label2
      value: bar
      signing: true
      mergeAlgorithm: test
    name: introspect-image
    relation: local
    type: ociImage
    version: 1.0.0
  - access:
      localReference: sha256:d1187ac17793b2f5fa26175c21cabb6ce388871ae989e16ff9a38bd6b32507bf
      mediaType: ""
      type: localBlob
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: d1187ac17793b2f5fa26175c21cabb6ce388871ae989e16ff9a38bd6b32507bf
    name: introspect-blueprint
    relation: local
    type: landscaper.gardener.cloud/blueprint
    version: 1.0.0
  - access:
      localReference: sha256:4186663939459149a21c0bb1cd7b8ff86e0021b29ca45069446d046f808e6bfe
      mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
      referenceName: vasu1124/helm/introspect-helm:0.1.0
      type: localBlob
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: 6229be2be7e328f74ba595d93b814b590b1aa262a1b85e49cc1492795a9e564c
    name: introspect-helm
    relation: external
    type: helm
    version: 0.1.0
  sources:
  - access:
      repository: github.com/vasu1124/introspect
      type: git
    name: introspect
    type: git
    version: 1.0.0
  version: 1.0.0
meta:
  schemaVersion: v2
`

	// Unmarshal YAML into a generic map.
	var descriptor descriptorv2.Descriptor
	if err := yaml.Unmarshal([]byte(inputYAML), &descriptor); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	desc, err := runtime.ConvertFromV2(&descriptor)
	if err != nil {
		t.Fatalf("failed to convert descriptor: %v", err)
	}

	// Normalise the descriptor using our normalization (ExclusionRules applied).
	normalizedBytes, err := normalisation.Normalise(desc, v4alpha2.Algorithm)
	if err != nil {
		t.Fatalf("failed to normalize descriptor: %v", err)
	}
	normalized := string(normalizedBytes)

	// Expected canonical JSON output.
	// Note: Fields that are excluded (e.g. "meta", "repositoryContexts", "access" in resources, etc.)
	// are omitted and maps/arrays are canonically ordered. label1 has no signing
	// attribute and is kept (default-signed); mergeAlgorithm on label2 is dropped.
	expected := `{"component":{"componentReferences":[],"name":"github.com/vasu1124/introspect","provider":{"name":"internal"},"resources":[{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"6a1c7637a528ab5957ab60edf73b5298a0a03de02a96be0313ee89b22544840c"},"labels":[{"name":"label1","value":"foo"},{"name":"label2","signing":true,"value":"bar"}],"name":"introspect-image","relation":"local","type":"ociImage","version":"1.0.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"d1187ac17793b2f5fa26175c21cabb6ce388871ae989e16ff9a38bd6b32507bf"},"name":"introspect-blueprint","relation":"local","type":"landscaper.gardener.cloud/blueprint","version":"1.0.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"6229be2be7e328f74ba595d93b814b590b1aa262a1b85e49cc1492795a9e564c"},"name":"introspect-helm","relation":"external","type":"helm","version":"0.1.0"}],"sources":[{"name":"introspect","type":"git","version":"1.0.0"}],"version":"1.0.0"}}`

	assert.JSONEq(t, expected, normalized)
	if normalized != expected {
		t.Errorf("normalized output does not match expected.\nExpected:\n%s\nGot:\n%s", expected, normalized)
	}
}

// TestVersionedLabelIncluded pins the v4alpha2 change that re-introduces
// Label.Version into the normalised bytes: a signed label's "version" MUST
// appear. This is the deliberate reversal of v4alpha1's version exclusion and
// is signature-breaking relative to v4alpha1 (hence a new algorithm version).
func TestVersionedLabelIncluded(t *testing.T) {
	r := require.New(t)
	d := &runtime.Descriptor{}
	d.Component.Name = "acme.org/x"
	d.Component.Version = "1.0.0"
	d.Component.Provider.Name = "acme"
	d.Component.Labels = []runtime.Label{{
		Name: "k", Value: []byte(`"v"`), Signing: true, Version: "v1",
	}}

	got, err := normalisation.Normalise(d, v4alpha2.Algorithm)
	r.NoError(err)
	r.Contains(string(got), `"labels":[{"name":"k","signing":true,"value":"v","version":"v1"}]`)
}

// TestDefaultSignedLabel pins the v4alpha2 Option-2 default: a label with no
// signing attribute is treated as signing:true and kept. Under v4alpha1 the
// same label would be excluded. Because omitempty collapses signing:false and
// absent on the v2 wire type, this is also what happens to a label the author
// set to signing:false.
func TestDefaultSignedLabel(t *testing.T) {
	r := require.New(t)
	d := &runtime.Descriptor{}
	d.Component.Name = "acme.org/x"
	d.Component.Version = "1.0.0"
	d.Component.Provider.Name = "acme"
	d.Component.Labels = []runtime.Label{{
		Name: "unsigned", Value: []byte(`"v"`),
	}}

	got, err := normalisation.Normalise(d, v4alpha2.Algorithm)
	r.NoError(err)
	r.Contains(string(got), `"labels":[{"name":"unsigned","value":"v"}]`)
}

// TestIgnoreLabelsWithoutSignature pins the v4alpha2 default-signed semantics:
// only an explicit signing:false (observable on a raw map) excludes a label; an
// absent attribute is treated as signing:true and kept.
func TestIgnoreLabelsWithoutSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{
			name: "with signature true",
			input: map[string]interface{}{
				"signing": true,
			},
			expected: false,
		},
		{
			name: "with signature string true",
			input: map[string]interface{}{
				"signing": "true",
			},
			expected: false,
		},
		{
			name: "with signature false",
			input: map[string]interface{}{
				"signing": false,
			},
			expected: true,
		},
		{
			name: "with signature string false",
			input: map[string]interface{}{
				"signing": "false",
			},
			expected: true,
		},
		{
			name: "without signature is default-signed",
			input: map[string]interface{}{
				"other": "value",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v4alpha2.IgnoreLabelsWithoutSignature(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNormalise(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes jcs.TransformationRules
		expected string
		wantErr  bool
	}{
		{
			name: "explicit signing:false excluded, absent kept",
			input: []interface{}{
				map[string]interface{}{
					"name":  "default",
					"value": "value",
				},
				map[string]interface{}{
					"name":    "signed",
					"value":   "value2",
					"signing": true,
				},
				map[string]interface{}{
					"name":    "unsigned",
					"value":   "value3",
					"signing": false,
				},
			},
			excludes: v4alpha2.LabelExcludes,
			expected: `[{"name":"default","value":"value"},{"name":"signed","signing":true,"value":"value2"}]`,
		},
		{
			name: "signed label with version is kept",
			input: []interface{}{
				map[string]interface{}{
					"name":    "k",
					"value":   "v",
					"signing": true,
					"version": "v1",
				},
			},
			excludes: v4alpha2.LabelExcludes,
			expected: `[{"name":"k","signing":true,"value":"v","version":"v1"}]`,
		},
		{
			// Regression test for #3431: label values may be arbitrary JSON,
			// including arrays (e.g. cloud.gardener.cnudie/responsibles). The
			// normalisation used during signing must preserve such values.
			name: "signed label with array value",
			input: []interface{}{
				map[string]interface{}{
					"name": "cloud.gardener.cnudie/responsibles",
					"value": []interface{}{
						map[string]interface{}{
							"github_hostname": "github.com",
							"teamname":        "open-component-model/odg-maintainers",
							"type":            "githubTeam",
						},
					},
					"signing": true,
				},
			},
			excludes: v4alpha2.LabelExcludes,
			expected: `[{"name":"cloud.gardener.cnudie/responsibles","signing":true,"value":[{"github_hostname":"github.com","teamname":"open-component-model/odg-maintainers","type":"githubTeam"}]}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jcs.Normalise(tt.input, tt.excludes)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(got))
		})
	}
}
