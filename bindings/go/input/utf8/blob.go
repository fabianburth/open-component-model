package utf8

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	v1 "ocm.software/open-component-model/bindings/go/input/utf8/spec/v1"
)

// GetV1UTF8Blob creates a ReadOnlyBlob from a v1.UTF8 specification.
// It supports text, JSON, formatted JSON, and YAML content types.
func GetV1UTF8Blob(utf8 v1.UTF8) (blob.ReadOnlyBlob, error) {
	if err := utf8.Validate(); err != nil {
		return nil, fmt.Errorf("error validating utf8 input spec: %w", err)
	}

	var (
		reader    io.Reader
		mediaType string
		size      int64
	)

	switch {
	case utf8.Text != "":
		reader = strings.NewReader(utf8.Text)
		mediaType = "text/plain"
		size = int64(len(utf8.Text))
	case len(utf8.JSON) > 0, len(utf8.FormattedJSON) > 0:
		var data []byte
		var err error
		switch {
		case len(utf8.JSON) > 0:
			data, err = json.Marshal(utf8.JSON)
		case len(utf8.FormattedJSON) > 0:
			data, err = json.MarshalIndent(utf8.FormattedJSON, "", "  ")
		}
		if err != nil {
			return nil, fmt.Errorf("error marshalling utf8 JSON input: %w", err)
		}
		reader = bytes.NewReader(data)
		mediaType = "application/json"
		size = int64(len(data))
	case len(utf8.YAML) > 0:
		data, err := yaml.Marshal(utf8.YAML)
		if err != nil {
			return nil, fmt.Errorf("error marshalling utf8 YAML input: %w", err)
		}
		reader = bytes.NewReader(data)
		mediaType = "application/x-yaml"
		size = int64(len(data))
	default:
		return nil, fmt.Errorf("utf8 input must contain a valid content description")
	}

	var utf8Blob blob.ReadOnlyBlob = direct.New(reader,
		direct.WithMediaType(mediaType),
		direct.WithSize(size),
	)

	if utf8.Compress {
		utf8Blob = compression.Compress(utf8Blob)
	}

	return utf8Blob, nil
}
