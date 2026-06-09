# Transfer-time localization — mock walkthrough

Demonstrates the [proposal](../PROPOSAL.md) end-to-end without any new code. We
fake the transfer agent by hand: rewrite the resource, rebuild and resign the
component with `ocm sign cv`, then attach an in-toto SLSA Provenance v1
attestation as an OCI referrer using `oras attach`. Each hop links to the
previous hop's component manifest digest via `predicate.buildDefinition.resolvedDependencies`,
forming a walkable chain.

## What this verifies before any code

- Per-hop OCM descriptor resigning is straightforward — `ocm sign cv` already
  produces what we need.
- DSSE-wrapped SLSA Provenance v1 carries enough state to:
  - identify the rewrite (buildType + externalParameters),
  - anchor to the previous hop (resolvedDependencies),
  - identify the operator (builder.id, signing key).
- OCI 1.1 referrers API (`oras attach` / `oras discover`) is the right
  storage mechanism: no descriptor schema changes, content-addressed,
  works across registries that follow the spec.
- The chain remains walkable from any downstream registry as long as
  the previous-hop manifest is reachable (the referrers tree is local
  to each registry, so registries cache the bits they care about).

## What this does NOT verify

- Convention-based scanning of `values.yaml` (we rewrite by hand).
- Annotation override (`# ocm: from=resource-name`).
- Multi-line / non-string image references.
- Any of this happening atomically inside `ocm transfer cv`.
- Sigstore-keyless signing — we use offline RSA + cosign-key mode.
  Production swaps `--signer-spec rsa.yaml` for `--signer-spec sigstore.yaml`
  and `cosign attest-blob --key` for `cosign attest-blob` (keyless). Same
  envelope format, same chain semantics.

## Prerequisites

```bash
brew install oras jq yq cosign
# docker desktop for local registries
# ocm: from this repo (`task build` at the root) or from a release
```

## Run the demo

```bash
cd .scratch/transfer-time-localization/mock
task                 # Show the index
task prereqs         # Tool check
task setup           # Start 3 registries, generate keys (RSA + cosign)
task hop0-publish    # Vendor publishes signed component
task hop0-verify     # Verify vendor signature
task hop1-transfer   # Plain transfer vendor -> corp
task hop1-localize   # Rewrite, resign, attest at corp
task hop1-verify     # Verify corp + walk chain
task hop2-transfer   # Plain transfer corp -> airgap
task hop2-localize   # Rewrite, resign, attest at airgap
task hop2-verify     # Verify airgap + walk full chain
task chain-walk      # Full chain dump from airgap
task chain-diff HOP=1   # Byte diff hop0 -> hop1 chart-values
task chain-diff HOP=2   # Byte diff hop1 -> hop2 chart-values
task teardown        # Stop registries (keeps files)
task clean           # Stop registries and wipe state
```

## Topology

```
hop 0: vendor.example.com   →  localhost:5001  (vendor-rsa, vendor-cosign)
hop 1: corp.example.com     →  localhost:5002  (corp-rsa,   corp-cosign)
hop 2: airgap.example.com   →  localhost:5003  (airgap-rsa, airgap-cosign)
```

Each hop:
1. Pulls the component from the previous registry.
2. Downloads the `chart-values` resource, rewrites the image reference for
   the local mirror namespace.
3. Rebuilds the component into a fresh CTF, transfers to its registry.
4. Resigns the descriptor with its own RSA key (`ocm sign cv`).
5. Constructs a SLSA Provenance v1 statement, signs as DSSE with cosign-key,
   attaches as an OCI referrer of the component manifest.

## Reading the chain

The `chain-walk` task starts at the current-hop tag, then for each manifest:
fetches its in-toto referrer, decodes the DSSE payload, prints the SLSA
predicate, and follows `resolvedDependencies[0].digest` to the prior hop —
all on the same registry.

```bash
task chain-walk REG=localhost:5003
```

Output (abbreviated):

```json
manifest: sha256:9f0a... (airgap)
  attestation: sha256:b8c1...
{
  "buildType": "https://ocm.software/transfer/localize/v1",
  "hop": 2,
  "rewrite": "image.repository: localhost:5002/library/nginx -> localhost:5003/library/nginx",
  "builder": "airgap.example.com/transfer-agent",
  "subject": "ocm.software/mock/transfer-localize:1.0.0@localhost:5003",
  "resolvedDeps": [
    {
      "uri": "oci://localhost:5002/component-descriptors/ocm.software/mock/transfer-localize:1.0.0",
      "digest": { "sha256": "5e2d..." }
    }
  ]
}

manifest: sha256:5e2d... (corp)
  attestation: sha256:7a44...
{
  "hop": 1,
  "rewrite": "image.repository: docker.io/library/nginx -> localhost:5002/library/nginx",
  "builder": "corp.example.com/transfer-agent",
  "resolvedDeps": [{"uri": "oci://localhost:5001/...", "digest": {"sha256": "1c9f..."}}]
}

manifest: sha256:1c9f... (vendor)
  (no in-toto attestation — reached hop 0)
```

### Why this works without access to upstream registries

`hop-carry-chain` (run after every localize) does:

```bash
# At hop 1 (corp): copy hop 0's manifest + referrers from vendor into corp by digest
oras cp --recursive --from-plain-http --to-plain-http \
  localhost:5001/component-descriptors/.../@sha256:<vendor-digest> \
  localhost:5002/component-descriptors/.../@sha256:<vendor-digest>

# At hop 2 (airgap): copy hop 0 and hop 1 into airgap by digest
oras cp --recursive --from-plain-http --to-plain-http \
  localhost:5001/...@<vendor-digest> localhost:5003/...@<vendor-digest>
oras cp --recursive --from-plain-http --to-plain-http \
  localhost:5002/...@<corp-digest>   localhost:5003/...@<corp-digest>
```

`oras cp --recursive` follows referrers, so each prior-hop component manifest
arrives at airgap together with its DSSE attestation. The chain walk above
is then fully self-sufficient: every `oras manifest fetch`, `oras discover`,
and `oras pull` hits `localhost:5003` only.

### Manual chain inspection

To poke at it by hand without `task chain-walk`:

```bash
# Current-hop manifest digest
DIGEST=$(oras manifest fetch --plain-http \
  localhost:5003/component-descriptors/ocm.software/mock/transfer-localize:1.0.0 \
  --descriptor | jq -r .digest)

# Discover the in-toto referrer
ATT_DIGEST=$(oras discover --plain-http --format json \
  localhost:5003/component-descriptors/ocm.software/mock/transfer-localize@$DIGEST \
  | jq -r '.manifests[] | select(.artifactType=="application/vnd.in-toto+json") | .digest')

# Pull the DSSE envelope, decode the payload
mkdir -p /tmp/dsse && oras pull --plain-http -o /tmp/dsse \
  localhost:5003/component-descriptors/ocm.software/mock/transfer-localize@$ATT_DIGEST

jq -r .payload /tmp/dsse/*.dsse.json | base64 -d | jq .
```

The decoded payload is the in-toto Statement — its `predicate` is the SLSA
Provenance, including `resolvedDependencies[0].digest.sha256`, which is the
*previous-hop* component manifest digest. Step that into `oras discover` and
repeat.

## Verifying signatures across the chain

Three signatures matter, with three different verification targets:

| Signature | Where | Verifier needs | Purpose |
|---|---|---|---|
| Hop 2 OCM (airgap RSA) | Descriptor at airgap | airgap pubkey | Trust the *current* descriptor bytes |
| Hop 2 DSSE (airgap cosign) | Referrer at airgap | airgap cosign pubkey | Trust the rewrite-attestation chain link |
| Hop 0 OCM (vendor RSA) | Descriptor at vendor | vendor pubkey | Trust the *original* descriptor bytes |

### Verify the current-hop descriptor (airgap)

```bash
task hop2-verify
# = ocm verify cv --signature airgap --config airgap-ocmconfig.yaml \
#     oci::http://localhost:5003//ocm.software/mock/transfer-localize:1.0.0
```

This proves: the descriptor on airgap right now was signed by the airgap
operator's RSA key. Trust at this point = trust in the airgap operator.

### Verify the DSSE attestation

The chain links are only as trustworthy as the DSSE envelopes. Each is signed
with the corresponding hop's cosign key:

```bash
# Hop 2's attestation (signed by airgap cosign key)
cosign verify-blob-attestation \
  --key keys/airgap-cosign.pub \
  --type slsaprovenance1 \
  --new-bundle-format=false \
  --signature attestations/hop2.dsse.json \
  attestations/hop2.statement.json

# Hop 1's attestation (signed by corp cosign key)
cosign verify-blob-attestation \
  --key keys/corp-cosign.pub \
  --type slsaprovenance1 \
  --new-bundle-format=false \
  --signature attestations/hop1.dsse.json \
  attestations/hop1.statement.json
```

A verifier at airgap who only has the DSSE envelopes (pulled from referrers,
not the local `attestations/` directory) does the same thing — extract
`payload` from the envelope, base64-decode to get the statement, then run
`cosign verify-blob-attestation` against the trusted cosign pubkey for that
hop's operator.

### Verify the original-author (vendor) signature

The vendor signature is on hop 0's descriptor. `ocm sign cv` replaces
signatures on each push, so airgap's descriptor does **not** carry the
vendor signature. Two options:

**Option A — verify against the vendor registry directly (online):**

```bash
task hop0-verify
# = ocm verify cv --signature vendor --config vendor-ocmconfig.yaml \
#     oci::http://localhost:5001//ocm.software/mock/transfer-localize:1.0.0
```

Works whenever vendor's registry (or any mirror of it) is reachable and the
verifier has the vendor pubkey. The chain attestation gives the verifier the
hop 0 *manifest digest* to look up; the lookup itself goes through the tag
because OCM doesn't yet support digest-based component resolution.

**Option B — verify against bytes carried into airgap (offline, not yet supported):**

`hop-carry-chain` already copied hop 0's manifest into airgap with signatures
intact (`oras cp@digest` is byte-exact). What's missing is OCM-side digest
resolution: `ocm verify cv` accepts only a tag reference today, so it can't
address the carried-in manifest at airgap. This is tracked as an open
question in the proposal — production needs:

```bash
# NOT YET SUPPORTED — proposal open question
ocm verify cv --signature vendor --config vendor-ocmconfig.yaml \
  oci::http://localhost:5003//ocm.software/mock/transfer-localize@sha256:1c9f...
```

Until that lands, raw `oras` works for inspection (digest of carried-in
manifest matches what `chain-walk` reported, signature blocks unchanged):

```bash
oras manifest fetch --plain-http \
  localhost:5003/component-descriptors/ocm.software/mock/transfer-localize@sha256:<vendor-digest>
```

## Diffing resource bytes between hops

```bash
task chain-diff HOP=1   # docker.io/library/nginx -> localhost:5002/library/nginx
task chain-diff HOP=2   # localhost:5002/library/nginx -> localhost:5003/library/nginx
```

Output for `HOP=1`:

```diff
-  repository: docker.io/library/nginx
+  repository: localhost:5002/library/nginx
```

The current implementation reads the local workspace (`components/hopN/values.yaml`)
because OCM can't yet resolve a component by manifest digest. Once that
limitation is fixed, the same task should pull `chart-values` from each
prior-hop manifest on airgap directly — see [proposal open
questions](../PROPOSAL.md#open-questions).

## Cosign key mode vs. sigstore keyless

The DSSE envelopes here are signed with `cosign attest-blob --key
<file>.cosign.key --tlog-upload=false`. This is the **same envelope format**
that public-Sigstore keyless produces, just wrapping a static-pubkey signature
instead of a Fulcio short-lived cert. To run this against public Sigstore:

```bash
# Drop --key, --tlog-upload=false. Add identity provider config or use ambient credentials.
cosign attest-blob \
  --predicate hop1.statement.json \
  --type slsaprovenance1 \
  hop1.statement.json
# Sigstore opens a browser for OIDC, calls Fulcio for a cert, signs, logs to Rekor.
```

The OCM descriptor signature switches similarly:

```bash
# Replace the RSA signer-spec with:
#   type: SigstoreSigningConfiguration/v1alpha1
ocm sign cv --signer-spec sigstore-sign.yaml ...
```

Identity material changes (key fingerprint → Fulcio cert subject), the rest of
the chain semantics are unchanged.

## Files this generates

```
keys/                    # Per-hop RSA + cosign keypairs
components/hop{0,1,2}/   # Constructor + values.yaml per hop
out/                     # CTF archives, signer/verifier specs, ocmconfig
attestations/            # In-toto statements + DSSE envelopes per hop
```

Inspect anything that catches the eye. `ocm get cv ... -o yaml` is the
fastest way to see what the descriptor looks like at each hop, including
which signatures are attached. `oras discover --plain-http ...` shows the
referrers tree for the component manifest at each hop.

## Known caveats

- We use `zot` (`ghcr.io/project-zot/zot-linux-{arm64,amd64}`) for the local
  registries — it supports the OCI 1.1 referrers API natively. If you swap it
  for an older registry, `oras discover` falls back to the tag-based referrers
  scheme (a separate tag per subject digest), still works.
- We rebuild the component from a fresh CTF on each hop because there's no
  in-place "rewrite the resource of an existing component" command yet —
  this is one of the things real localize support would smooth out.
- `ocm sign cv` always replaces the descriptor's existing signatures section
  on a remote registry push when overwriting; the hop1 vendor signature is
  not preserved on hop1's descriptor, only chained via the attestation.
  This matches the proposal: trust at each hop is the current-hop signature;
  history lives in the referrer chain.
