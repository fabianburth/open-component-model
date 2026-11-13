package discovery

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// testRegistry is a TLS OCI registry serving one image, test/app:1.0.0.
type testRegistry struct {
	// host is the registry host (host:port) without scheme.
	host string
	// manifest is the digest of the image manifest.
	manifest digest.Digest
	// ocmConfig trusts the registry's certificate.
	ocmConfig string
}

// newTestRegistry serves one image manifest of test/app under tag 1.0.0, with
// its config and layer blob, over TLS.
func newTestRegistry(t *testing.T) *testRegistry {
	t.Helper()

	const repo, tag = "test/app", "1.0.0"
	layer := []byte("verified content")
	config := []byte(`{}`)
	manifest, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeEmptyJSON, Digest: digest.FromBytes(config), Size: int64(len(config))},
		Layers:    []ociImageSpecV1.Descriptor{{MediaType: "application/octet-stream", Digest: digest.FromBytes(layer), Size: int64(len(layer))}},
	})
	require.NoError(t, err)

	objects := map[string][]byte{
		"manifests/" + tag: manifest,
		"manifests/" + digest.FromBytes(manifest).String(): manifest,
		"blobs/" + digest.FromBytes(config).String():       config,
		"blobs/" + digest.FromBytes(layer).String():        layer,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v2/" {
			return
		}
		rest, ok := strings.CutPrefix(req.URL.Path, "/v2/"+repo+"/")
		data := objects[rest]
		if !ok || data == nil {
			http.NotFound(w, req)
			return
		}
		mediaType := "application/octet-stream"
		if strings.HasPrefix(rest, "manifests/") {
			mediaType = ociImageSpecV1.MediaTypeImageManifest
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Docker-Content-Digest", digest.FromBytes(data).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if req.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	return &testRegistry{
		host:     strings.TrimPrefix(srv.URL, "https://"),
		manifest: digest.FromBytes(manifest),
		ocmConfig: marshalOCMConfig(t, map[string]any{
			"type":       "http.config.ocm.software/v1alpha1",
			"rootCAsPEM": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})),
		}),
	}
}

// imageReference is the tagged reference of the served image.
func (r *testRegistry) imageReference() string {
	return r.host + "/test/app:1.0.0"
}
