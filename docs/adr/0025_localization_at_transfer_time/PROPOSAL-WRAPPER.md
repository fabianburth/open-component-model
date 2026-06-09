# Transfer-Time Localization — Wrapper-Chart Model

> Use Helm's own composition model. Don't invent one.

Status: draft / for discussion
Author: Fabian, with Claude
Date: 2026-06-09

Compare against:
- [PROPOSAL.md](PROPOSAL.md) — chain-rewrite model.
- [PROPOSAL-SIDECAR.md](PROPOSAL-SIDECAR.md) — full-chart sidecar model.

This proposal keeps the same goals and non-negotiables as the sidecar
model. It changes the localized artifact from "rewritten copy of the
chart" to "tiny wrapper chart that depends on the original."

## Goal (unchanged)

After `ocm transfer cv ... target`, a consumer running native deploy
tooling (Flux, Argo, plain `helm install`) against artifacts in
`target` can install successfully — image pulls go to the local
mirror, not the upstream the author wrote. No deploy-time
OCM-aware render step on the consumer's side.

## Non-negotiables (unchanged)

- Transfer-time only.
- Multi-hop composes.
- Original author's signature stays verifiable.
- Seamless author UX.

## Core idea

The signed resources in the descriptor — the original Helm chart, the
image references, anything content-addressed — **never change** across
hops. Their digests are stable. The descriptor's signature stays
valid forever, transparently.

What does change: each hop publishes a **wrapper chart** alongside
the original at the same registry. The wrapper is a minimal Helm
chart whose `Chart.yaml` declares the original as a dependency, and
whose `values.yaml` overrides image references for the local
environment.

The wrapper's manifest carries `subject: <original-chart-manifest-digest>`,
anchoring it as an OCI 1.1 referrer of the original. The descriptor's
access for the chart resource continues to point at the **original**,
not the wrapper. OCM-aware tooling discovers the wrapper via referrer
lookup; non-OCM-aware tooling pulling the access gets the byte-
identical original (which is the honest state — the original isn't
localized).

## Why this is better than full-sidecar

| Concern | Full-chart sidecar (PROPOSAL-SIDECAR) | Wrapper chart |
|---|---|---|
| Storage per hop | Original + full localized chart | Original + ~1 KB wrapper |
| Canonical chart repack required | Yes (hard — tar member ordering, gzip metadata, mtimes) | No (wrapper is two YAML files) |
| Determinism scope | Whole-chart bytes | Two YAML files |
| Scanner story | Annotation-based ("derived from X") | Structural — Helm dependency declared in `Chart.yaml` |
| Helm-version compatibility | Any | Subchart values precedence stable since Helm 3.0 |
| Consumer experience | Discover referrer, prefer-localized | Discover referrer, point tooling at wrapper |
| Argo support | Awkward (multi-source for values) | Native (subchart dependencies are first-class) |

## How it works

### Authoring (unchanged)

Author writes a normal Helm chart with `values.yaml` using standard
Helm key conventions (`image.repository`, `image.tag`, `image.digest`,
etc.). Pins images **by digest** (`@sha256:...`). Digest pinning is
a hard requirement for security under this model — same as
PROPOSAL-SIDECAR. See *Trust analysis* below.

Author writes a normal `component-constructor.yaml`, listing the
chart and the images as separate resources. Signs the descriptor.
Pushes to their registry. Done. No new flags at authoring time.

### Transfer

`ocm transfer cv --localize <source> <target>`:

1. **Pull** the descriptor and its resources from `<source>`. Verify
   the descriptor signature.
2. **Push** the original resources to `<target>` byte-identical. The
   descriptor's access fields are updated to point at `<target>`.
   The signed resource digests are unchanged.
3. **Build the wrapper(s).** For each chart resource that needs
   localization, run the canonical
   `GenerateWrapper(original_chart_metadata, descriptor) → wrapper_files`
   function (see *Verification* below). Package as a Helm chart,
   push to `<target>` as a separate OCI artifact whose manifest
   carries `subject: <original-chart-manifest-digest>` and an
   artifact type `application/vnd.ocm.localized.v1+helm.wrapper`.

The descriptor's access for the chart resource continues to point at
the **original chart**. The wrapper is reachable only as a referrer.

That's it. The descriptor signature was verified on pull (step 1)
and the same signed descriptor flows to `<target>` unchanged. There
is no resign step. There is no attestation chain.

### What the wrapper looks like

Original chart at `target.registry/charts/podinfo:6.11.1` (byte-
identical to source). Author's `values.yaml` includes:

```yaml
image:
  repository: ghcr.io/stefanprodan/podinfo
  digest: sha256:abc123...
sidecar:
  image:
    repository: ghcr.io/stefanprodan/podinfo-sidecar
    digest: sha256:def456...
```

The transfer agent generates a wrapper chart with the following
layout — exactly the structure Helm expects for a parent chart with
a subchart dependency
([Helm subcharts and globals](https://helm.sh/docs/chart_template_guide/subcharts_and_globals/)):

```
podinfo-localized/
├── Chart.yaml
├── Chart.lock          # generated by `helm dependency update`
└── values.yaml
```

`Chart.yaml` — declares the original as a pinned OCI dependency:

```yaml
apiVersion: v2
name: podinfo-localized
version: 6.11.1+localized.1
type: application
description: Localized wrapper for podinfo, generated by OCM transfer.
dependencies:
  - name: podinfo
    version: 6.11.1
    repository: oci://target.registry/charts
    alias: podinfo
    # Helm 3.14+: pin the dependency by digest for full integrity.
    # repository: oci://target.registry/charts@sha256:<original-chart-digest>
```

`values.yaml` — overrides image references under the dependency
alias. Helm's subchart values-passing (stable since Helm 3.0) merges
these into the subchart's own `values.yaml` at render time, with
parent values taking precedence
([Helm docs](https://helm.sh/docs/chart_template_guide/subcharts_and_globals/#overriding-values-from-a-parent-chart)):

```yaml
podinfo:
  image:
    repository: target.registry/library/podinfo
    digest: sha256:abc123...
  sidecar:
    image:
      repository: target.registry/library/podinfo-sidecar
      digest: sha256:def456...
```

Packaged as `.tgz` and pushed at
`target.registry/charts/podinfo-localized:6.11.1+localized.1`, with
manifest `subject` pointing at the original chart's manifest digest.

A consumer's `HelmRelease` or `Application` then references the
wrapper:

```yaml
# Flux HelmRelease
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
spec:
  chart:
    spec:
      chart: podinfo-localized
      version: 6.11.1+localized.1
      sourceRef:
        kind: HelmRepository
        name: target-registry
```

At `helm install`, Helm pulls the wrapper, resolves the `podinfo`
dependency from the same registry, and renders templates with the
merged values — which means image refs in the rendered Deployments
point at `target.registry`, not the upstream the author wrote.
Original chart bytes on disk are unchanged; the override happens
purely at template rendering.

### Discovery

A consumer at `<target>` who wants the localized variant of the chart
resource does:

1. Resolve the descriptor's access for the chart resource → OCI ref
   of the original.
2. `oras discover` referrers, filter by artifact type
   `application/vnd.ocm.localized.v1+helm.wrapper`.
3. If exactly one match, that's the localized wrapper. If zero, the
   original works as-is (or doesn't, if it needs localization the
   author didn't provide for). If multiple, apply tiebreaker (see
   *Open questions*).

Wrapped in a CLI helper like `ocm get reference --prefer-localized`,
this is a single-call lookup. Output is a plain OCI URL the
operator's deploy tooling consumes — pointing at the wrapper.

The descriptor's access **does not** change to point at the wrapper.
That property matters: anyone resolving the descriptor naively (no
OCM tooling, no referrer discovery) gets the original — the chart
the author actually signed. The localized variant is a discoverable
overlay, not a replacement.

### Multi-hop

vendor → corp → airgap. Three observations:

**Original chart travels byte-identical.** Each hop transfers
descriptor + resources to the next registry; access fields update,
content doesn't. The original signature stays valid the whole way.

**Wrappers are environment-local.** corp's wrapper has
`subject: <vendor-original-chart-digest>` and overrides image refs
for corp's namespace. airgap's wrapper has
`subject: <vendor-original-chart-digest>` (same root) and overrides
for airgap's namespace. Both derive *independently* from the same
authoritative source.

**No chain to walk.** Trust at airgap = "I verify the original
descriptor signature on the bytes that arrived." If I want to
inspect what corp did, I can pull corp's wrapper and read its
`values.yaml` — but I don't *need* to in order to deploy.

The "chain" of localizations is structurally a fan, not a chain —
every hop's wrapper points at the same root.

### Re-author

Same as PROPOSAL-SIDECAR. `ocm transfer cv --reauthor` re-signs the
descriptor with the operator's identity. Wrappers still work the
same way — they `subject` whatever's in the registry. No special
chain-break machinery needed because there is no chain.

## Trust analysis

### What's signed, what isn't

- **Signed by the author**: the descriptor's normalised content,
  including each resource's digest (the **original** chart's digest).
- **Not signed (mutable by transfer)**: each resource's access
  field, which points at the registry where the original now lives.
- **Not signed**: the wrapper bytes themselves. The wrapper is a
  generated artifact at each hop; the operator could sign it
  (cosign keyed or sigstore keyless), but the trust path doesn't
  require it.

### How the wrapper is verified

Two approaches, same as PROPOSAL-SIDECAR. The wrapper model makes
both dramatically simpler.

#### Approach A: deterministic generation + admission webhook

The wrapper-generation rewrite is a **canonical, OCM-protocol-
defined pure function**:

```
GenerateWrapper(original_metadata, descriptor) → wrapper_files
```

Inputs:

- `original_metadata` — name, version, OCI ref of the original chart
  at the current registry (resolvable from the descriptor + access).
- `descriptor` — the full descriptor object. The function reads
  sibling resources' access fields to determine override targets
  (every value override points the chart at where the descriptor
  itself says the sibling now lives).

There are **no other parameters**. Two implementations of the
function fed the same inputs produce byte-identical output, and
therefore byte-identical wrapper digests.

Output: a pair (or small set) of canonical YAML files:

- `Chart.yaml` — wrapper metadata, declaring the original as a
  pinned dependency.
- `values.yaml` — overrides under the dependency alias key.
- (Optional) `Chart.lock` if the registry/Helm version requires it.

Packaged as a chart `.tgz`. The packaging step requires deterministic
tar/gzip — but since the inputs are tiny YAML files, the tar contains
2–3 fixed-name members and canonicalization is trivial (sorted member
order, fixed mtimes = 0, gzip metadata stripped). This is materially
easier than canonical full-chart repack: the input space is bounded
to a known small set of files with known content.

A consumer-side **admission webhook** (deployed once per cluster)
verifies any wrapper by recomputing:

1. Pull the wrapper at `<wrapper-digest>`. Read its `subject` →
   `<original-chart-digest>`.
2. Pull the descriptor whose resource list includes
   `<original-chart-digest>`. Verify the descriptor signature
   against an allowlisted author identity.
3. Read the wrapper's `Chart.yaml`. Confirm the dependency
   `repository + version` resolves to `<original-chart-digest>`.
4. Run `GenerateWrapper(original_metadata, descriptor)` locally.
5. Hash the result. Compare to `<wrapper-digest>`. Match → admit;
   mismatch → reject.

The transfer agent is **untrusted for content correctness**. Anyone
can push a wrapper at any digest; only wrappers whose digest equals
the canonical recompute admit.

This is reproducible-builds-as-a-trust-model, scoped to the wrapper-
generation step. The trust root is the original author's signature
on the descriptor; everything else is mechanical.

**What this requires:**

- The convention scanner is *normative* (same as PROPOSAL-SIDECAR
  and PROPOSAL).
- `GenerateWrapper` is fully deterministic. Canonical YAML
  serializer (sorted maps, deterministic line endings) and a small,
  fixed-shape tar/gzip packaging for the wrapper chart.
- The function is *versioned*. Wrapper manifest declares
  `ocm.software/localize-version: v1`; webhook supports v1+v2+...
- Operator-customized rewrites are **out of scope**. Author-side
  annotation overrides (`# ocm: from=...`) are fine because they're
  in source bytes, signed by the author.

#### Approach B: sign the wrapper

Same shape as PROPOSAL-SIDECAR Approach B. Transfer agent signs the
wrapper with cosign (keyed or keyless). Verifier checks signature
against an operator-allowlisted agent identity, and confirms the
wrapper's dependency resolves to a base resource in a signed
descriptor.

No determinism requirement; the wrapper is whatever the agent
generated, and the signature attests the agent endorsed it.

Same tradeoffs as PROPOSAL-SIDECAR: per-transfer signing
infrastructure, operator allowlist, malicious-agent risk.

#### Comparison

| Property | Approach A (deterministic + webhook) | Approach B (signing) |
|---|---|---|
| Trust the transfer agent for content | No | Yes |
| New keys / PKI / sigstore | None | Per-agent signing key |
| Cluster-side setup | Admission webhook (one-time) | Operator policy listing allowed agent identities |
| Operator-customized rewrites | Not supported | Supported |
| Behaviour on a malicious agent | Wrapper rejected (digest mismatch) | Wrapper admitted if signed by allowlisted-but-compromised agent |
| Spec & implementation discipline | Canonical wrapper-generation function (small) | Standard signing tooling |
| Per-hop operational overhead | None | One signature per transfer |

Approach A is the **default**. Approach B exists for environments
that want operator-customizable rewrites or are not ready to
guarantee determinism. Both can be supported in the same
implementation.

The Approach A spec under this model is **substantially smaller**
than under PROPOSAL-SIDECAR, because the deterministic surface is
two YAML files and a 2–3-member tar, not an arbitrary chart
structure.

### Defense in depth: digest pinning

Identical to PROPOSAL-SIDECAR. The descriptor's access fields can be
mutated by transfer. A compromised transfer agent could rewrite an
image resource's access to point at `evil.io/library/nginx`, and
then `GenerateWrapper(...)` would produce a wrapper whose
`values.yaml` references `evil.io/library/nginx@sha256:...`.

**Digest pinning makes this attack inert.** The image reference in
the wrapper's values.yaml is `evil.io/library/nginx@sha256:abc...`.
The kubelet pulls from `evil.io` but verifies against `sha256:abc...`.
Either:

- evil.io serves the legitimate bytes (CDN of the same digest) —
  harmless.
- evil.io serves different bytes — kubelet rejects on digest
  mismatch.

The access field is a *hint about where to look*; the digest is the
actual integrity check.

### Threat catalogue

**Threat: registry tampering after transfer replaces the wrapper
bytes.** Operator's deploy tooling pins the wrapper by digest. Same
mitigation as PROPOSAL-SIDECAR.

**Threat: malicious wrapper at transfer time (Approach A).** Wrapper
digest doesn't match canonical recompute; webhook rejects.

**Threat: malicious wrapper at transfer time (Approach B).** Same
profile as PROPOSAL-SIDECAR Approach B.

**Threat: descriptor's access field rewritten to malicious
registry.** Digest pinning collapses this to denial of service.

**Threat: tampered original chart at the registry.** Wrapper's
`Chart.yaml` declares the dependency by `version`. If the registry
serves different bytes for that version, Helm dependency resolution
pulls them and the rendered manifests use those bytes' templates.

Mitigation: pin the dependency by digest where supported (Helm 3.14+
supports OCI digest pinning in dependency repository URLs). Where
not supported, the descriptor's signed digest of the original chart
is the ground truth — the admission webhook (Approach A) can verify
the dependency resolves to `<D_orig>` and reject otherwise.

**Threat: author published a non-digest-pinned image reference.**
Same as PROPOSAL-SIDECAR. Outside the security envelope. Convention
scanner flags or treats as ineligible.

**Threat: tampering with the deploy manifest in the operator's
GitOps repo.** Outside OCM's scope.

## Vulnerability scanner story

Strictly better than PROPOSAL-SIDECAR's annotation-based story.

The wrapper's `Chart.yaml` declares the dependency on the original
chart **structurally**. Trivy/Grype/Snyk scanning the wrapper see:

- A Helm chart with one dependency (`podinfo:6.11.1`).
- The dependency resolves to the original chart at the registry.
- Scanning recurses into the dependency, finds image references in
  the original's templates and values, matches against the CVE
  database keyed on the original's digests.

This is exactly how scanners already handle umbrella charts in the
Helm ecosystem. No annotation convention to bolt on, no scanner-
vendor cooperation required.

The original chart's digest is also unchanged in the descriptor and
present at the registry. Scanners pointed at the component find it
directly, with or without going through the wrapper.

## Author UX, end to end

1. Author writes Helm chart + `component-constructor.yaml` exactly
   as today. **Pins all image references by digest.** Signs the
   component. Done.
2. `ocm transfer cv --localize $TARGET`:
   - Resources travel byte-identical.
   - Descriptor signature carries through.
   - For each chart resource, the canonical `GenerateWrapper`
     function produces a wrapper chart; wrapper is published to
     `$TARGET` as a referrer of the original.
   - Descriptor's access for the chart still points at the original.
3. Consumer uses their existing deploy tooling. Operator-side
   discovery (e.g., `ocm get reference --prefer-localized`) resolves
   the wrapper URL. Flux `HelmRelease` or ArgoCD `Application`
   points at the wrapper. Cluster's admission webhook (Approach A)
   or signature verifier (Approach B) gates it on admit.

When convention doesn't suffice, author adds `# ocm: from=...`
annotations as in PROPOSAL.md. Same syntax, same author surface.

## Where the code lives

- `bindings/go/localization/` — the **canonical wrapper-generation
  function**. Pure, deterministic, versioned. Convention scanner +
  annotation parser + canonical YAML serializer + minimal-tar
  packager. No I/O. Same module imported by transfer agent (to
  produce wrappers) and admission webhook (to verify them).
- `bindings/go/transfer/internal/` — new transformation node
  `GenerateHelmWrapper` that calls `GenerateWrapper`, pushes the
  result as a referrer artifact.
- `bindings/go/oci/` — small extension to push artifacts with
  `subject` set; existing referrer-listing code is reused for
  discovery.
- `kubernetes/admission/localization-webhook/` (new) — admission
  webhook consuming `bindings/go/localization` for verification.
  Approach A.
- `bindings/go/signing/` — extension to support wrapper signing if
  the operator chooses Approach B. Existing OCM signing handlers
  reused.
- CLI: `--localize` flag on `ocm transfer cv`; `--prefer-localized`
  flag on `ocm get reference`.

**No** `bindings/go/attestation/` module. **No** in-toto / SLSA
chain machinery. **No** policy DSL. **No** canonical full-chart
repack.

## Phasing

1. **Phase 1 — wrapper mechanism.** Convention scanner + wrapper
   generator + referrer-aware OCI push + `ocm get reference
   --prefer-localized`. Trust by registry-controlled push (Approach
   B without explicit signing — works for self-hosted single-tenant
   registries today).

2. **Phase 2a — Approach A (deterministic + webhook).** Lock down
   the canonical `GenerateWrapper` function spec. Implement
   canonical YAML serializer and minimal tar/gzip packaging. Ship
   the admission webhook.

   **Phase 2b — Approach B (signing).** Optional wrapper signing
   via existing OCM signing handlers. Ships in parallel with 2a;
   neither is required for the other.

3. **Phase 3 — annotation override.** `# ocm: from=...` syntax
   support, identical to PROPOSAL.md.

4. **Phase 4 — additional resource types.** The pattern
   generalizes: use the resource type's own composition mechanism.
   - **Kustomize**: generate a kustomize overlay referencing the
     base via OCI. Overlay applies `images:` patches with the
     localized refs. Native kustomize semantics.
   - **Plain k8s manifests**: generate a kustomize overlay over
     the manifests (kustomize handles plain manifests too).
   - **Other formats**: case-by-case, but the principle is the
     same — composition via the format's native mechanism, not
     via OCM-layer rewriting.

Compared to PROPOSAL.md, all the in-toto / SLSA / chain-walking /
policy-DSL phases are absent. Compared to PROPOSAL-SIDECAR, the
canonical full-chart repack work is absent.

## Open questions

- **Wrapper version naming.** `6.11.1+localized.1`? `6.11.1+corp.local`?
  Semver build metadata (`+`) signals derivative without affecting
  ordering. Lock down a convention before phase 1 ships.

- **Subchart digest pinning.** Helm 3.14+ supports OCI digest pinning
  in dependency `repository`. Pre-3.14 environments: rely on the
  admission webhook (Approach A) to verify the dependency resolves
  to the descriptor's signed digest. Document the version floor.

- **Wrapper artifact type URI.** Suggested:
  `application/vnd.ocm.localized.v1+helm.wrapper`. Distinct from
  PROPOSAL-SIDECAR's full-chart sidecar type. Lock down before
  phase 1 ships.

- **Versioning the wrapper-generation function.** When we bump from
  v1 to v2, wrappers produced under v1 must keep verifying. Webhook
  supports multiple versions; transfer agents produce the latest.
  Need a formal deprecation/sunset policy for old versions.

- **Multiple wrappers per original.** Two operators publish wrappers
  of the same original chart at the same registry: `oras discover`
  returns both. Under Approach A, duplicates produced from the same
  descriptor are byte-identical (deterministic), so deduplication
  by digest is automatic. Different descriptors (different
  environments) produce different wrappers; operator policy
  resolves which to deploy.

- **Wrapper lifecycle on re-transfer.** If `ocm transfer cv
  --localize` runs twice with different target registries from the
  same source, do earlier-target wrappers get garbage-collected?
  Suggest: no, they live with the registry, not the component.

- **Discovery in non-OCI targets.** OCI image layouts support
  referrers natively. Filesystem and other non-OCI targets are out
  of scope for `--localize`, same as PROPOSAL-SIDECAR.

- **Author opt-out.** Some authors may *want* their resource bytes
  unchanged — no localization, no wrappers, period. Suggest a
  resource-level annotation `ocm.software/localize: false` that
  suppresses wrapper generation for that resource.

- **Webhook scope.** Same as PROPOSAL-SIDECAR. Suggest opt-in via a
  label like `ocm.software/source-component: <component-id>`;
  absence means "not under OCM verification, deploy as-is."

- **Wrapper for non-Helm resources.** Phase 4 plan is "use the
  format's own composition mechanism." Worth one ADR per format
  (kustomize first) before phase 4 to validate the pattern holds.

- **Templates that hardcode image refs.** If the author wrote
  `image: docker.io/library/nginx:1.27` directly in a template
  (not via `{{ .Values.image }}`), the wrapper can't override it.
  This is the same constraint as PROPOSAL-SIDECAR's values-only
  variant — author must use values-driven image refs. Convention
  scanner flags violations; author fixes via `# ocm: from=...`
  annotation or by refactoring the template. Standard Helm
  practice.

## Comparison with prior proposals

| Aspect | PROPOSAL.md | PROPOSAL-SIDECAR | This proposal |
|---|---|---|---|
| Resource bytes mutate at each hop | Yes | No | No |
| Descriptor digests change per hop | Yes | No | No |
| Per-hop signing | Required | Optional (Approach B) | Optional (Approach B) |
| in-toto / SLSA attestations | Required | Not used | Not used |
| Storage per hop (chart) | One full chart | Original + full localized | Original + ~1 KB wrapper |
| Canonical chart repack required | Yes (implicit) | Yes (load-bearing for Approach A) | No |
| Determinism surface (Approach A) | Whole chart | Whole chart | Two YAML files + tiny tar |
| Verifying original-author signature | Walk attestation chain | Verify base signature directly | Verify base signature directly |
| Vulnerability scanners find original CVEs | No | Yes (annotation hint) | Yes (structural via Chart.yaml dependency) |
| Sigstore / Fulcio / Rekor dependency | Yes | Optional | Optional |
| Cluster-side trust component | Trust transferred descriptor + chain | Admission webhook (full-chart recompute) | Admission webhook (wrapper recompute) |
| Phase 2 complexity | Large (chain, walker, policy DSL) | Medium (canonical chart repack) | Small (canonical YAML + minimal tar) |
| Argo support | n/a | Awkward (multi-source values) | Native (subchart deps) |
| Generalizes to non-Helm | Format-agnostic | Format-agnostic (in theory) | Per-format composition (kustomize, etc.) |

## Why I think this wins over PROPOSAL-SIDECAR

PROPOSAL-SIDECAR's headline argument is "decline the choice to mutate
resource bytes; trust collapses to verifying the original signature."
That argument applies fully here too — resource bytes don't mutate.

The wrapper variant goes one step further: **don't reinvent
composition.** Helm already has a composition model (subchart
dependencies). Use it. The benefits:

1. **Storage drops by ~3 orders of magnitude** for the localized
   variant. A 5 MB chart's wrapper is 1–2 KB.

2. **The hardest open question in PROPOSAL-SIDECAR — canonical
   full-chart repack — disappears.** You only need canonical YAML
   (well-understood) and a minimal-tar packager (a few-dozen-line
   library, deterministic by construction over fixed-name members).

3. **Scanners get a structural signal, not an annotation hint.**
   Helm's existing dependency mechanism is what scanners already
   understand. CVE matches against the original chart fire by
   default.

4. **Argo support becomes native.** PROPOSAL-SIDECAR's values-only
   sub-variant suffers from Argo's awkward cross-source values
   handling. A wrapper chart is just *a chart* — Argo's
   `Application.spec.source.helm` consumes it directly.

5. **Per-hop CPU is trivial.** Generating two YAML files and
   packaging a 2-member tar is microseconds. No diff/delta
   computation. No full-chart unpack/repack.

The cost relative to PROPOSAL-SIDECAR is one constraint: **this
proposal is Helm-specific by design**. Phase 4 generalizes via per-
format composition (kustomize overlays, etc.) rather than via a
single OCM-layer mechanism. That's a deliberate tradeoff:
better-fit per-format paths over one mediocre format-agnostic path.
Helm has the dominant share of templated-deploy workloads; the
remaining formats (kustomize, raw manifests) have their own
composition mechanisms that the same pattern fits.

## Open Problems

- Having an additional access for the wrapper in a label does not feel like
  the best solution yet.

## References

- [PROPOSAL.md](PROPOSAL.md) — chain-rewrite model.
- [PROPOSAL-SIDECAR.md](PROPOSAL-SIDECAR.md) — full-chart sidecar
  model.
- Helm subchart values precedence —
  [docs](https://helm.sh/docs/chart_template_guide/subcharts_and_globals/).
- Helm OCI digest pinning (3.14+) —
  [release notes](https://github.com/helm/helm/releases/tag/v3.14.0).
- OCI 1.1 referrers API —
  [spec](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers).
- Kustomize image transformer —
  [docs](https://kubectl.docs.kubernetes.io/references/kustomize/builtins/#_imagetagtransformer_).
