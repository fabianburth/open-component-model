// Package v4alpha2 provides JSON normalisation for Open Component Model
// descriptors. It is the successor to jsonNormalisation/v4alpha1 and shares the
// JCS (RFC 8785) baseline and the bulk of its field exclusion rules.
//
// # Why a separate version
//
// v4alpha2 differs from v4alpha1 in two deliberate ways that change the
// normalised bytes — and therefore the signature — for the SAME stored
// descriptor. Because existing v4alpha1 signatures must keep verifying against
// v4alpha1, these changes cannot be an in-place edit of v4alpha1; they are a
// new, independently selectable algorithm version:
//
//  1. Default-signed labels (Option 2). v4alpha1 excluded any label that was
//     not explicitly signing:true. v4alpha2 inverts the default: an absent
//     signing attribute means signing:true, so the label is kept, and only an
//     explicit signing:false excludes it.
//
//  2. Label.Version is re-introduced into the normalised label bytes. v4alpha1
//     deliberately excluded Label.Version (the runtime<->v2 conversion only
//     began preserving it for jsonNormalisation/v1); v4alpha2 includes it so a
//     label's specification version participates in the signature.
//
// # omitempty caveat for default-signed labels
//
// v2.Label.Signing is declared `bool json:"signing,omitempty"`, so a false
// value and an absent attribute both serialise to NO "signing" key. The
// normaliser operates on the descriptor AFTER it has been round-tripped through
// that v2 JSON form, so it cannot distinguish "the author set signing:false"
// from "the author omitted signing" — both arrive as an absent key and are
// treated as signing:true (kept). An explicit signing:false is only observable
// for raw label maps that never passed through the v2 type. In practice every
// label on a stored descriptor is included under v4alpha2; opting a label OUT
// of the signature is not expressible through the current v2 wire type. See
// IgnoreLabelsWithoutSignature.
package v4alpha2
