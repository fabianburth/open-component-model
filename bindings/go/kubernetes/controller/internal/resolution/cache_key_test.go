package resolution

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Component patterns change which repository a component resolves from, so a
// resolver built with them must not be shared with one built without.
func TestBuildRepoCacheKeySeparatesComponentPatterns(t *testing.T) {
	r := require.New(t)
	spec := &runtime.Raw{Type: runtime.NewVersionedType("ctf", "v1"), Data: []byte(`{"type":"ctf/v1","filePath":"/x"}`)}

	key := func(patterns ...string) string {
		k, err := buildRepoCacheKey([]byte("config"), spec, patterns)
		r.NoError(err)
		return k
	}

	none, root := key(), key("ocm.software/root")
	r.Equal(none, key(), "the key is stable")
	r.Equal(root, key("ocm.software/root"), "the key is stable")
	r.NotEqual(none, root)
	r.NotEqual(key("ab"), key("a", "b"))
}
