# Transfer-Time Localization — Sidecar Model

> Make a transferred component deployment-ready without rewriting any
> signed bytes, without resigning at every hop, and without a chain of
> per-hop attestations.

Status: draft / for discussion
Author: Fabian, with Claude
Date: 2026-06-09

Compare against [PROPOSAL.md](PROPOSAL.md) (the chain-rewrite model).
This proposal keeps the same goal and non-negotiables; it changes the
mechanism.

## Goal (unchanged)

After `ocm transfer cv ... target`, a consumer running native deploy
tooling (Flux, Argo, plain `helm install`) against artifacts in
`target` can install successfully — image pulls go to the local mirror,
not the upstream the author wrote. No deploy-time OCM-aware render
step on the consumer's side.

## Non-negotiables (unchanged)

- Transfer-time only.
- Multi-hop composes.
- Original author's signature stays verifiable.
- Seamless author UX.

## Core idea

The signed resources in the descriptor — the helm chart, the image
references, anything content-addressed — **never change** across
hops. Their digests are stable. The descriptor's signature stays
valid forever, transparently.

What does change: each hop publishes a **localized sidecar artifact**
alongside the base resource at the same registry. The sidecar is a
new OCI artifact (a rewritten chart, a rewritten config blob) whose
manifest carries `subject: <base-resource-manifest-digest>`,
anchoring it as an OCI 1.1 referrer of the base.

Conceptually it's "base image + overlay" applied to OCM resources.
The overlay lives next to the base in the same registry, content-
addressed. OCM does **not** generate deploy manifests; that's the
operator's tooling. OCM's job ends when the sidecar is published.

## Why this is a much smaller proposal

| Concern | Chain-rewrite model | Sidecar model |
|---|---|---|
| Per-hop signing | Yes (RSA or sigstore keyless, every hop) | Optional / not load-bearing |
| In-toto / SLSA chain | Required for multi-hop trust | Not used |
| Cross-registry referrer carry | Required (`oras cp --recursive`) | Not required |
| Descriptor mutation per hop | Yes (resource bytes change) | None (resources byte-identical across hops) |
| Original-author signature preservation | Reconstructed from chain attestations | Direct — the signature on the base descriptor is the verification |
| Vulnerability-scanner story | Scanners see new digests, lose CVE matches | Scanners see the original digest in the descriptor; CVE matches fire normally |
| Deploy-manifest generation | Out of scope | Out of scope |

## How it works

### Authoring (unchanged from PROPOSAL.md)

Author writes a normal Helm chart with `values.yaml` using standard
Helm key conventions (`image.repository`, `image.tag`, etc.). Pins
images **by digest** (`@sha256:...`). Digest pinning is a hard
requirement for security under the sidecar model — see *Trust
analysis* below.

Author writes a normal `component-constructor.yaml`, listing the
chart and the images as separate resources. Signs the descriptor
with their identity. Pushes to their registry. Done. No new flags
required at authoring time.

### Transfer

`ocm transfer cv --localize <source> <target>`:

1. **Pull** the descriptor and its resources from `<source>`. Verify
   the descriptor signature.
2. **Push** the original resources to `<target>` byte-identical. The
   descriptor's access fields are updated to point at `<target>`
   (this is what `ocm transfer cv` does today — *access* changes,
   *content* doesn't, descriptor digests stay stable because access
   isn't part of the signed normalisation).
3. **Build the sidecar(s).** For each content-bearing resource that
   needs localization, run the canonical `Localize(base, descriptor)`
   function (see *Verification* below). Push the resulting bytes to
   `<target>` as a separate OCI artifact whose manifest carries
   `subject: <base-resource-manifest-digest>` and an artifact type
   like `application/vnd.ocm.localized.v1+helm.chart`.

That's it. The descriptor signature was verified on pull (step 1)
and the same signed descriptor flows to `<target>` unchanged (apart
from the unsigned access fields). There is no resign step. There is
no attestation chain.

### Discovery

A consumer at `<target>` who wants the localized variant of resource
`<res>` does:

1. Resolve the descriptor's access for `<res>` to its OCI ref.
2. `oras discover` referrers, filter by artifact type
   `application/vnd.ocm.localized.v1+...`.
3. If exactly one match, that's the localized variant. If zero, the
   base works as-is. If multiple, apply tiebreaker.

Wrapped in a CLI helper like `ocm get reference --prefer-localized`,
this is a single-call lookup. The output is a plain OCI URL the
operator's deploy tooling consumes.

### Multi-hop

vendor → corp → airgap. Three observations:

**Original resources travel byte-identical.** Each hop transfers the
descriptor and its resources to the next registry; access fields
update, content doesn't. The original signature stays valid the
whole way.

**Sidecars are environment-local.** corp's localized chart has
`subject: <vendor-base-chart-digest>` and rewrites image refs for
corp's namespace. airgap's localized chart also has
`subject: <vendor-base-chart-digest>` (same root) and rewrites for
airgap's namespace. Both derive *independently* from the same
authoritative source.

**No chain to walk.** Trust at airgap = "I verify the original
descriptor signature on the bytes that arrived." If I want to inspect
what corp did, I can pull corp's sidecar separately and diff it
against the base — but I don't *need* to in order to deploy.

The "chain" of localizations is structurally a fan, not a chain —
every hop's sidecar points at the same root.

### Re-author

If an operator wants to break trust to the original author
(legitimate use case, e.g., enterprise re-vendoring),
`ocm transfer cv --reauthor` re-signs the descriptor with the
operator's identity, updating `signatures[]`. Sidecars still work
the same way — they `subject` whatever's in the registry. No
special chain-break machinery is needed because there is no chain.

## Trust analysis

### What's signed, what isn't

- **Signed by the author**: the descriptor's normalised content,
  including each resource's digest.
- **Not signed (mutable by transfer)**: each resource's access
  field, which is allowed to change as the descriptor moves
  registries.
- **Not signed**: the sidecar bytes themselves. The sidecar is a
  new artifact at each hop; the operator could sign it (cosign
  keyed or sigstore keyless), but the trust path doesn't require
  it.

### How the sidecar is verified

Two approaches. They can be combined; choose one as default.

#### Approach A: deterministic localization + admission webhook

The localization rewrite is a **canonical, OCM-protocol-defined
pure function**:

```
Localize(base_bytes, descriptor) → localized_bytes
```

The function is fully closed over its inputs:

- `base_bytes` — the original resource content (chart, config, etc.).
- `descriptor` — the full descriptor object. The function reads
  sibling resources' access fields to determine rewrite *targets*
  (every rewrite points the chart at where the descriptor itself
  says the sibling now lives).

There are **no other parameters**. Two implementations of the
function fed the same inputs produce byte-identical output, and
therefore byte-identical sidecar digests.

A consumer-side **admission webhook** (deployed once per cluster)
verifies any sidecar by recomputing:

1. Pull the sidecar at `<sidecar-digest>`. Read its `subject` →
   `<base-digest>`.
2. Pull the descriptor whose resource list includes `<base-digest>`.
   Verify the descriptor signature against an allowlisted author
   identity.
3. Pull the base resource at `<base-digest>`.
4. Run `Localize(base_bytes, descriptor)` locally.
5. Hash the result. Compare to `<sidecar-digest>`. Match → admit;
   mismatch → reject.

The transfer agent is **untrusted for content correctness**. Anyone
can push a sidecar at any digest; only sidecars whose digest equals
the canonical recompute admit. An attacker who wants to inject a
malicious sidecar would have to produce bytes that the canonical
function would also produce — which means they're not actually
attacking, they're producing the legitimate localization.

This is reproducible-builds-as-a-trust-model, applied to the
localization step. The trust root is the original author's
signature on the descriptor; everything else is mechanical.

**What this requires:**

- The convention scanner is *normative*. Two implementations must
  agree on what's an image reference, how matches against sibling
  accesses are made, and how rewrites are applied.
- The localization function is fully deterministic. For helm chart
  `.tgz` resources: canonical extract, rewrite, canonical repack
  (sorted tar member order, canonical gzip metadata, fixed mtimes).
  For YAML resources: deterministic serializer (sorted maps,
  preserved comment positions, normative line endings).
- The function is *versioned*. When OCM bumps the canonical
  function, sidecars produced under the old version must keep
  verifying. Sidecar manifest declares
  `ocm.software/localize-version: v1`; webhook supports v1+v2+...
- Operator-customized rewrites are **out of scope**. The function
  is OCM-protocol-defined; operators do not get to plug in custom
  logic. Author-side annotation overrides (`# ocm: from=...` from
  PROPOSAL.md Part 1) are fine because they're in source bytes,
  signed by the author.

#### Approach B: sign the sidecar

The transfer agent signs the sidecar with cosign (keyed or
sigstore keyless). Verifier:

1. Pull the sidecar.
2. Verify signature against an operator-allowlisted agent
   identity.
3. Confirm `subject` digest matches a base resource in a signed
   descriptor.

No determinism requirement; no canonical function spec; the rewrite
is whatever the agent did, and the signature attests the agent
endorsed it.

**What this requires:**

- Per-transfer signing infrastructure: keys (RSA/cosign keypairs),
  rotation policy, what-if-compromised playbook. Or sigstore
  keyless (Fulcio + Rekor), with all that entails.
- Per-cluster operator policy: which agent identities are allowed
  to produce sidecars. Allowlist coarse-grained ("trust the corp
  transfer agent") rather than content-aware.
- A `--no-sign-sidecar` opt-out for environments that prefer
  Approach A or trust the registry's push controls.

#### Comparison

| Property | Approach A (deterministic + webhook) | Approach B (signing) |
|---|---|---|
| Trust the transfer agent for content | No | Yes (signature is the trust) |
| New keys / PKI / sigstore | None | Per-agent signing key |
| Cluster-side setup | Admission webhook (one-time) | Operator policy listing allowed agent identities |
| Operator-customized rewrites | Not supported | Supported (agent does whatever, signs) |
| Behaviour on a malicious agent | Sidecar rejected (digest mismatch) | Sidecar admitted if signed by an allowlisted-but-compromised agent |
| Spec & implementation discipline | Canonical localize function, versioned | Standard signing tooling |
| Per-hop operational overhead | None (no signing) | One signature per transfer |

Approach A is the **default**. Approach B exists for environments
that want operator-customizable rewrites or are not ready to
guarantee determinism. Both can be supported in the same
implementation; they're not mutually exclusive — a sidecar can be
both deterministically reproducible *and* signed.

### Defense in depth: digest pinning

The descriptor's access fields can be mutated by transfer (this is
how transfer redirects registry references). A compromised transfer
agent could in principle rewrite an image resource's access to
point at `evil.io/library/nginx`, and then `Localize(...)` would
produce a sidecar referencing `evil.io/library/nginx@sha256:...`.

**Digest pinning makes this attack inert.** The image reference in
the chart is the form `evil.io/library/nginx@sha256:abc...`. The
kubelet pulls from `evil.io` but verifies against `sha256:abc...`.
Either:

- evil.io serves the legitimate bytes (it's just a CDN of the
  same digest) — harmless.
- evil.io serves different bytes — kubelet rejects on digest
  mismatch.

The access field is a *hint about where to look*; the digest is the
actual integrity check. Author-side discipline of pinning resource
digests collapses the access-rewriting threat into "denial of
service" (the rewritten access points somewhere unreachable), not
"redirection to malicious content."

This is why **digest pinning is a hard requirement** for any
resource participating in localization. PROPOSAL.md already
required it for convention-scan eligibility (PROPOSAL.md:71-76);
the sidecar model elevates it from a heuristic-safety measure to a
load-bearing security property.

A registry-allowlist policy (the cluster only pulls from
`{corp.io, corp-vault.io, corp-cdn.io}`) is useful for *operational*
hygiene — preventing unintended egress to unknown registries —
but is not load-bearing for security.

### Threat catalogue

**Threat: registry tampering after transfer replaces the sidecar
bytes.** Operator's deploy tooling pins the sidecar by digest
(standard practice for OCI consumers — Flux `OCIRepository`,
ArgoCD, etc., all support digest pinning). Tampered bytes have a
different digest; pull either resolves to original (content-
addressed) or 404s. Deployment can be denied of service but not
redirected.

**Threat: malicious sidecar at transfer time (Approach A).** Sidecar
digest doesn't match canonical recompute; webhook rejects.

**Threat: malicious sidecar at transfer time (Approach B).** Caught
only if the malicious agent is not on the operator's allowlist; if
the agent itself is compromised, sidecar admits. Same threat
profile as today's transfer-agent trust.

**Threat: descriptor's access field rewritten to malicious
registry.** Digest pinning collapses this to denial of service —
the kubelet's digest check rejects mismatched bytes from the wrong
registry.

**Threat: author published a non-digest-pinned image reference.**
Outside the security envelope. Convention scanner treats unpinned
references as ineligible for matching (PROPOSAL.md:71-76); they
pass through unchanged. If an author writes
`image: docker.io/library/nginx:1.27` (no digest), the chart
references that string verbatim regardless of localization, and
the kubelet pulls whatever is currently behind that tag. This is
the author's choice; OCM can warn but cannot fix it.

**Threat: tampering with the deploy manifest in the operator's
GitOps repo.** Outside OCM's scope. Mitigated by the operator's
GitOps controls (signed commits, branch protection).

## Vulnerability scanner story

This is where the sidecar model is meaningfully better than the
chain-rewrite model.

In chain-rewrite, the helm chart's digest changes at every hop.
Trivy/Grype/Snyk scanning the corp registry see a chart digest
they've never seen before; their CVE database keys on the original
digest; they report "no known vulnerabilities" because they can't
match. This is a real false-negative, equivalent to the
base-image-inheritance problem in container scanning.

In sidecar, the original chart's digest is unchanged in the
descriptor and present in the registry. Scanners pointed at the
component find the original chart by digest; CVE matches against
`podinfo:6.11.1@sha256:abc...` fire as expected.

The sidecar can be ignored by scanners (it's a derivative; the
underlying packages are determined by the base) or scanned
separately if the operator chooses (in which case the `subject`
field tells the scanner what it's derived from, providing the
same base-image-inheritance hint scanners use for container layers).

## Author UX, end to end

1. Author writes Helm chart + `component-constructor.yaml` exactly
   as today. **Pins all image references by digest.** Signs the
   component. Done.
2. `ocm transfer cv --localize $TARGET`:
   - Resources travel byte-identical.
   - Descriptor signature carries through.
   - For each rewrite candidate, the canonical `Localize` function
     produces a sidecar; sidecar is published to `$TARGET` as a
     referrer of the base.
3. Consumer uses their existing deploy tooling. Operator-side
   discovery (e.g., a wrapper script around `ocm get reference
   --prefer-localized`, or a CLI plugin to their GitOps generator)
   resolves the sidecar URL and pins it by digest. Cluster's
   admission webhook (Approach A) or signature verifier (Approach
   B) gates it on admit.

When convention doesn't suffice, author adds `# ocm: from=...`
annotations as in PROPOSAL.md. Same syntax, same author surface.

## Where the code lives

- `bindings/go/localization/` — the **canonical localize function**.
  Pure, deterministic, versioned. Convention scanner +
  annotation parser + serializer. No I/O. The same module is
  imported by the transfer agent (to produce sidecars) and the
  admission webhook (to verify them).
- `bindings/go/transfer/internal/` — new transformation node
  `LocalizeAsSidecar` that calls `Localize`, pushes the result as
  a referrer artifact.
- `bindings/go/oci/` — small extension to push artifacts with
  `subject` set; existing referrer-listing code is reused for
  discovery.
- `kubernetes/admission/localization-webhook/` (new) — admission
  webhook consuming `bindings/go/localization` for verification.
  Approach A.
- `bindings/go/signing/` — extension to support sidecar signing if
  the operator chooses Approach B. Existing OCM signing handlers
  reused; no new chain machinery.
- CLI: `--localize` flag on `ocm transfer cv`; `--prefer-localized`
  flag on `ocm get reference`.

**No** `bindings/go/attestation/` module. **No** in-toto / SLSA
chain machinery. **No** policy DSL.

## Phasing

1. **Phase 1 — sidecar mechanism.** Convention scanner + sidecar
   builder + referrer-aware OCI push + `ocm get reference
   --prefer-localized`. Trust by registry-controlled push (Approach
   B without explicit signing — works for self-hosted single-tenant
   registries today).

2. **Phase 2a — Approach A (deterministic + webhook).** Lock down
   the canonical localize function spec. Implement deterministic
   helm chart repack and YAML serializer. Ship the admission
   webhook.

   **Phase 2b — Approach B (signing).** Optional sidecar signing
   via existing OCM signing handlers. Ships in parallel with 2a;
   neither is required for the other.

3. **Phase 3 — annotation override.** `# ocm: from=...` syntax
   support, identical to PROPOSAL.md.

4. **Phase 4 — additional resource types.** Plain k8s manifests,
   kustomize bases, etc. Same convention/annotation contract,
   different localize-function module per type. Determinism
   discipline applies per type.

Compared to PROPOSAL.md, all the in-toto / SLSA / chain-walking /
policy-DSL phases are absent.

## Open questions

- **Canonical helm chart repack.** Tar member ordering, gzip
  metadata, mtime canonicalization. Helm itself uses a partial
  canonicalization for its chart digest; we need to spec exactly
  which form is normative for the localize function. Worth a short
  ADR before phase 2a.

- **Canonical YAML serializer.** yaml.v3 with deterministic
  settings is close. Comment preservation across rewrite is the
  fiddly bit. Spec needed before phase 2a.

- **Versioning the localize function.** When we bump from v1 to v2,
  sidecars produced under v1 must keep verifying. Webhook supports
  multiple versions; transfer agents produce the latest. Need a
  formal deprecation/sunset policy for old versions.

- **Sidecar artifact type URI.** Suggested:
  `application/vnd.ocm.localized.v1+<format>` where `<format>` is
  `helm.chart`, `kustomize`, etc. Lock down before phase 1 ships.

- **Multiple sidecars per base.** If two operators publish
  rewrites of the same base chart at the same registry, `oras
  discover` returns both. Pick a tiebreaker: under Approach A
  duplicates are redundant (deterministic — both sidecars have the
  same digest), so deduplication by digest is automatic. Under
  Approach B distinct agents produce distinct signatures; operator
  policy resolves.

- **Sidecar lifecycle on re-transfer.** If `ocm transfer cv --localize`
  runs twice with different target registries from the same source,
  do earlier-target sidecars get garbage-collected? Suggest: no,
  they live with the registry, not the component.

- **Discovery in non-OCI targets.** Image layouts support referrers
  natively. Filesystem and other non-OCI targets are out of scope
  for `--localize`, same as PROPOSAL.md.

- **Author opt-out.** Some authors may *want* their resource bytes
  unchanged — no localization, no sidecars, period. Suggest a
  resource-level annotation `ocm.software/localize: false` that
  suppresses sidecar generation for that resource.

- **Webhook scope.** Should the admission webhook verify *every*
  HelmRelease in the cluster, or only those marked as OCM-sourced?
  Suggest opt-in via a label like `ocm.software/source-component:
  <component-id>`; absence means "not under OCM verification, deploy
  as-is." Avoids gating non-OCM workloads.

## Comparison with PROPOSAL.md

| Aspect | PROPOSAL.md | This proposal |
|---|---|---|
| Resource bytes mutate at each hop | Yes | No |
| Descriptor digests change per hop | Yes (resources change) | No |
| Per-hop signing | Required | Approach A: none. Approach B: one signature per transfer (no chain) |
| in-toto / SLSA Provenance attestations | Required for multi-hop | Not used |
| Cross-registry referrer carry | Required | Not required |
| Verifying original-author signature | Walk attestation chain to root | Verify base signature directly |
| Vulnerability scanners find original CVEs | No (digests change) | Yes (digests stable) |
| Sigstore / Fulcio / Rekor dependency | Yes (for keyless mode) | Optional (Approach B keyless mode); not required |
| Cluster-side trust component | Trust transferred descriptor + its signature | Approach A: admission webhook running canonical localize. Approach B: signature-verification policy. |
| Phase 2 complexity | Large (chain, walker, policy DSL) | Approach A: spec the canonical function + ship webhook. Approach B: sign sidecars with existing tooling. |
| Failure mode if registry doesn't honor OCI 1.1 referrers | Chain unwalkable; trust fails | Sidecar undiscoverable; deploys fall back to base (functional but not localized) |

The headline tradeoff: this proposal **gives up in-place
descriptor-pointer rewrites** in exchange for **dropping the entire
chain-of-trust apparatus**.

## Why I think this wins

The chain-rewrite model is intellectually clean — every hop is a
signed transformation, the chain composes recursively, in-toto and
SLSA give us standard ecosystem fit. But every property of that
model exists to compensate for one underlying choice: *resources
are mutated in-place at each hop, breaking digests*. Once that
choice is made, you have to track and re-establish trust at every
hop, which is the entire Part 2 of PROPOSAL.md.

The sidecar model **declines that choice**. Resources stay fixed;
the rewritten variant lives next to the original, addressed by
content; trust at the consumer is exactly the trust at the
original publisher. There's no chain because there's nothing to
chain — each hop's rewrite is an independent function of the base,
not of the previous hop's rewrite.

Approach A goes further: the rewrite is an OCM-protocol-defined
*deterministic* function, so trust collapses to "verify the
signature on the descriptor + recompute the canonical localization
+ check the digest." No keys for the transfer agent. No PKI to
manage. The trust root is exactly where it should be: the original
author.

The cost is two-fold:
1. The localize function must be specified rigorously enough that
   two independent implementations produce byte-identical output.
2. OCM does not generate deploy manifests; the operator's tooling
   does. (The original author signature only protects descriptor +
   resource bytes, not deploy manifests. That's the operator's
   GitOps integrity problem.)

Both are bounded engineering — the determinism work is one-time per
resource type, and the deploy-manifest split-of-concerns is
arguably the right architectural boundary anyway. OCM stays in
the component-and-content business; operators stay in the
deploy-tooling business.

## References

- [PROPOSAL.md](PROPOSAL.md) — chain-rewrite model.
- OCI 1.1 referrers API —
  [spec](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers).
- Renovate helm-values manager —
  [docs](https://docs.renovatebot.com/modules/manager/helm-values/).
- SLSA Build L4 (reproducible builds as provenance) —
  [spec](https://slsa.dev/spec/v1.0/levels#build-l4).
- Helm chart digest spec —
  [docs](https://helm.sh/docs/topics/provenance/).
