# Transfer-Time Localization

> Make a transferred component deployment-ready with native tooling
> (Flux, Argo, plain `helm install`) by performing content-level
> rewrites *during transfer*, not at deploy time.

Status: draft / for discussion
Author: Fabian, with Claude
Date: 2026-06-01

## Goal

After `ocm transfer cv ... target`, the resource bytes in `target` —
including `values.yaml` shipped inside or alongside a Helm chart, and
any other content-bearing resource — reference the **target registry**,
not the source. A consumer who does not run any OCM tooling at deploy
time can `flux create source oci ... && flux create helmrelease ...`
and have it work, or `helm pull && helm install`, against the bytes in
the target registry as-is.

This is "localization at transfer time". Localization at deploy time
already works via the controllers + kro pattern in
`website/content/docs/tutorials/deploy-helm-chart-bootstrap.md`; that
pattern is not the answer here. The answer here ends with bytes in
the target registry that need no further OCM-aware processing.

## Non-negotiables

- **Transfer-time only.** No deploy-time render step. No `ocm render`
  command in the consumer's pipeline.
- **Multi-hop composes.** Components are routinely transferred through
  multiple registries (vendor → corp → air-gap). Each hop must be a
  complete, self-contained input to the next hop.
- **Original author's signature stays verifiable.** A consumer who
  trusts only the original author must be able to mechanically verify
  that author signed *something*, and that the chain of hops between
  that something and the bytes they're about to deploy is acceptable
  to their policy.
- **Seamless author UX.** A normal Helm chart that follows ecosystem
  convention should require zero author action.

## Design overview

Two halves: **what gets rewritten** and **how rewrites are
provenanced**.

### Part 1: rewriting

Borrowed wholesale from how Renovate handles the same shape of
problem. Two mechanisms, layered.

**Convention-based scan (default).** At transfer, the localizer parses
each content-bearing resource (YAML/JSON for now) and looks for image
references using the standard Helm-chart convention:
([Renovate helm-values manager](https://docs.renovatebot.com/modules/manager/helm-values/))

- `repository:` (image name)
- `tag:` (version)
- `registry:` (optional)
- `image:` (full reference as a single string)
- `version:` (alternative to `tag`)

Reconstructed candidate references are matched against any sibling
resource's current access in the same component descriptor. Match →
rewrite to the sibling's *new* access (already updated for the target
registry by transfer's existing access-rewrite logic). No match → leave
alone.

False-positive risk: a candidate string accidentally equals a sibling
resource's access. Mitigation: require digest pinning on the sibling's
access for it to participate in convention-scan matching. References
of the form `ghcr.io/x/y:1.0@sha256:...` are unique enough that
collisions are not a real-world concern. References without a digest
are not eligible for convention-scan matching; the author can opt them
in via annotation (next).

**Annotation override (escape hatch).** When convention doesn't apply,
the author attaches an inline comment to the value:

```yaml
# ocm: from=image-resource
fullImage: ghcr.io/stefanprodan/podinfo:6.11.1@sha256:262578cde9...
```

For split-field cases:

```yaml
image:
  # ocm: from=image-resource field=registry
  registry: ghcr.io
  # ocm: from=image-resource field=repository
  repository: stefanprodan/podinfo
  # ocm: from=image-resource field=digest
  digest: sha256:262578cde9...
```

Grammar: `from=<resource-name>` (required, names a sibling resource);
`field=registry|repository|tag|digest|reference` (optional, defaults
to `reference`). Suppression: `# ocm: skip` on a line tells the
convention scanner to leave it alone. Mirrors Renovate's
[regex/custom manager](https://docs.renovatebot.com/modules/manager/regex/)
pattern, narrowed because the descriptor already names every resource.

Annotations live in the resource bytes themselves. Nothing about
rewriting state lives in the descriptor.

Out of scope for v1: compressed blobs (chart `.tgz` is recursed into,
but other compressed formats aren't), encoded blobs (base64 manifests,
encrypted blobs), JSON (no comment syntax — author must use convention
or restructure).

### Part 2: provenance

Rewriting bytes invalidates resource digests, which invalidates the
descriptor signature. Each hop has to resign. Multi-hop with a
preserved-original-author requirement means resigning in a way that
**doesn't erase the chain.**

The model: each transfer hop produces a signed in-toto **attestation**
recording materials (input descriptor + resource digests) and
products (output descriptor + resource digests). Attestations are
chained by digest: hop N's `materials` references hop N-1's
`subject`. The original author's authorship is preserved as the
chain's terminal materials reference.

The descriptor itself is unchanged in shape. The standard OCM
`signatures[]` array on each hop's descriptor still attests
"this descriptor as it stands now is signed by hop N's identity". The
**new** machinery is the attestation chain attached to the descriptor
manifest as OCI referrers.

This separates two concerns cleanly:

- **Descriptor signature** = "are the bytes I just pulled authentic
  relative to whoever I'm pulling from?" Local check, same as today.
- **Attestation chain** = "what's the transformation history, and do I
  trust each hop?" Optional walk-back; required only for consumers
  whose policy demands it.

A consumer who doesn't care about the chain pulls the descriptor and
verifies its signature — same as today. A consumer who does care
fetches referrers, walks the chain, applies their policy.

#### Why in-toto, briefly

- It's the only standardized, signed, chainable transformation-record
  format. Reinventing this would put us alongside SLSA, sigstore,
  GitHub provenance, npm provenance, and lose interop with all of
  them.
- The DSSE envelope is what sigstore already emits. Keyless signing
  via Fulcio + Rekor logging gives us short-lived identities for
  transfer agents without long-lived key management.
- Standard verifiers (`in-toto-verify`, `cosign verify-attestation`,
  sigstore policy-controller) consume DSSE-wrapped statements
  directly. We don't have to ship a verifier; we have to ship a
  *policy* describing what to require.

#### Predicate choice: Link vs. SLSA Provenance

Two viable in-toto predicate types
([predicate index](https://github.com/in-toto/attestation/tree/main/spec/predicates)).
We need to pick one and ship it; the proposal documents both honestly
and recommends one.

**[in-toto Link v0.3](https://github.com/in-toto/attestation/blob/main/spec/predicates/link.md)**

Shape:
```json
{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [
    {"name": "descriptor", "digest": {"sha256": "<post-hop digest>"}},
    {"name": "resource:helm-values", "digest": {"sha256": "<post-hop digest>"}}
  ],
  "predicateType": "https://in-toto.io/attestation/link/v0.3",
  "predicate": {
    "name": "ocm.software/transfer-localize",
    "command": ["ocm", "transfer", "cv", "--localize", "..."],
    "materials": [
      {"name": "descriptor", "digest": {"sha256": "<pre-hop digest>"}},
      {"name": "resource:helm-values", "digest": {"sha256": "<pre-hop digest>"}}
    ],
    "byproducts": {
      "transformedResources": ["helm-values"],
      "agentVersion": "ocm-cli v0.x.y"
    },
    "environment": {
      "agentIdentity": "<sigstore subject>"
    }
  }
}
```

Pros:
- Shape *is* "I consumed materials, produced products" — exactly a
  hop. Materials and products are symmetric (both are
  ResourceDescriptor lists with `name` + `digest`). Zero impedance
  mismatch with the localization model.
- Lightweight. No build-platform abstraction. The fields it has are
  the fields we need.
- Chain reconstruction is mechanical: walk back via `materials.digest
  ↔ subject.digest` matches.

Cons:
- Less ecosystem traction than SLSA Provenance. Most policy
  controllers and verifiers are tuned for Provenance shapes.
  Link still verifies as a generic in-toto Statement; it just doesn't
  ride on SLSA-aware tooling.
- Doesn't model "trusted builder identity" as a first-class concept
  the way Provenance does. We'd put the agent identity in
  `environment` or rely on the DSSE signature's certificate. Works,
  less canonical.

**[SLSA Provenance v1](https://slsa.dev/spec/v1.0/provenance)**

Shape (sketched for our use):
```json
{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [
    {"name": "descriptor", "digest": {"sha256": "<post-hop digest>"}}
  ],
  "predicateType": "https://slsa.dev/provenance/v1",
  "predicate": {
    "buildDefinition": {
      "buildType": "https://ocm.software/buildtypes/transfer-localize/v1",
      "externalParameters": {
        "sourceRegistry": "ghcr.io/...",
        "targetRegistry": "registry.corp/...",
        "localize": true
      },
      "internalParameters": {
        "scannerConfig": {...}
      },
      "resolvedDependencies": [
        {"name": "descriptor", "digest": {"sha256": "<pre-hop digest>"}},
        {"name": "resource:helm-values", "digest": {"sha256": "<pre-hop digest>"}}
      ]
    },
    "runDetails": {
      "builder": {
        "id": "https://ocm.software/builders/cli/v1",
        "version": {"ocm-cli": "v0.x.y"}
      },
      "metadata": {
        "invocationId": "...",
        "startedOn": "...",
        "finishedOn": "..."
      },
      "byproducts": [
        {"name": "transformedResources", "content": "[\"helm-values\"]"}
      ]
    }
  }
}
```

Pros:
- Adopted by GitHub Actions provenance, npm provenance, sigstore
  policy-controller, SLSA verifier. Policies written for the SLSA
  ecosystem apply to our hops with minimal adaptation.
- `builder.id` is canonical and verifier-tooling expects it. Trust
  decisions like "I trust components transferred by
  `https://ocm.software/builders/transfer-agent/v1`" are first-class.
- Fits the SLSA narrative: each hop is a "build step" in the
  recursive supply chain ending at the consumer. Auditors familiar
  with SLSA understand the artifact immediately.

Cons:
- The `materials → products` semantics is implicit, not first-class.
  `resolvedDependencies` is the closest mapping for our `materials`,
  but the field's intent is "dependencies fetched at build init",
  which stretches semantically when the "dependency" is the previous
  hop's output. Workable; not pure.
- Heavier shape: `buildDefinition` + `runDetails` introduces a lot of
  nesting and required fields where Link would have one flat
  predicate. Authoring is more verbose; readability suffers.
- The `buildType` URI is opaque to standard SLSA tooling — we'd
  define our own — so part of the ecosystem-fit benefit only kicks
  in if we work to align our `externalParameters` schema with what
  SLSA verifiers can lint.

**Recommendation: SLSA Provenance v1.**

The ecosystem argument wins. The localization story we're telling is
ultimately about supply chain — each hop *is* a build step in the
SLSA sense, even if the build is "rewrite some bytes and resign." The
verbosity of Provenance is a one-time authoring cost; the payoff is
that any organization already running sigstore policy-controller or
SLSA verifier on their inbound supply chain gets meaningful
verification of OCM transfers without bespoke tooling.

The `materials → products` clarity Link offers is real, but it's a
clarity for *us*, not for our consumers. Consumers don't read the
predicate; their policy engine does. The policy engines speak
Provenance.

If a future need arises to express "this was a deterministic byte
transformation of these inputs" with full Link symmetry, we can
publish Link statements alongside Provenance statements without
breaking either. They live in different referrers; verifiers consume
what they understand. We don't have to pick once and forever. But the
default ships Provenance.

#### Storage in the registry

Each attestation is a DSSE envelope, signed by the hop's identity
(sigstore keyless via Fulcio + Rekor for default cases; static keys
allowed via configuration). The envelope is wrapped in an OCI
artifact manifest with `subject` pointing at the hop's output
descriptor manifest — i.e., it's a referrer of the descriptor
([OCI 1.1 referrers](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers)).

The walker:

1. Resolve the descriptor manifest digest in the target registry.
2. List referrers; filter for predicate type SLSA Provenance v1 with
   `buildType` matching `ocm.software/buildtypes/transfer-localize`.
3. For the matching attestation: parse `resolvedDependencies`, find
   the entry naming the input descriptor; that digest is the
   previous hop's output descriptor.
4. Recurse to step 1 with that digest.
5. Terminate when no matching referrer is found — that's the chain
   root, which should match the original author's signed descriptor.

CTF as a transport medium is **out of scope** for `--localize` — CTF
will be replaced by OCI image layouts in the near term, and OCI image
layouts support manifests and referrers natively, so the walker works
without modification across registries and image layouts.

#### Re-author / squash

Operators who legitimately want to break the chain (enterprise
re-vendoring an open-source component) use
`ocm transfer cv --localize --reauthor`. This produces a single
attestation with empty `resolvedDependencies` and a `byproducts` flag
indicating chain severance. Walkers detect this and report the
discontinuity. Any policy that requires walk-to-original-author
refuses to deploy. The operator who chose `--reauthor` is making an
explicit policy statement; the discontinuity is detectable by anyone
downstream.

#### Verification modes

All built on the attestation chain:

- **`ocm verify cv $REF`** — checks the descriptor's own signature
  against current bytes. Default. Same as today. No chain walk.
- **`ocm verify cv --policy <file> $REF`** — walks the attestation
  chain, applies policy. Policy can require: a specific identity at
  the chain root (original author), allowed identities for
  intermediate hops, no `--reauthor` markers, allowed
  `buildType`s, etc.
- **`ocm history cv $REF`** — walks and prints the chain in human
  form. Each hop: signer identity, timestamp, transformed resources,
  Rekor entry link.
- **`ocm diff cv --hop=N $REF`** — fetches resource bytes by digest
  for hop N's `resolvedDependencies` and `subject`, renders a diff.
  Diff is a tooling concern; bytes are already in the registry by
  digest.

## Author UX, end to end

1. Author writes a Helm chart with a normal `values.yaml` using
   standard Helm key names (`image.repository`, `image.tag`,
   `image.registry` or `image:` as a single string). Pins by digest.
2. Author writes a normal `component-constructor.yaml` listing the
   chart and the image as separate resources.
3. `ocm add cv`. No new flags, no new files.
4. `ocm transfer cv --localize $TARGET`. Transfer runs the convention
   scanner over each content-bearing resource, rewrites matches,
   recomputes digests, signs the new descriptor with the agent's
   identity, publishes a SLSA Provenance attestation as a referrer of
   the new descriptor.
5. Consumer pulls from `$TARGET`, runs `helm install`.
   Image pulls succeed against `$TARGET`.

When convention doesn't apply: add `# ocm: from=...` above the value.
One line, in the file the author already maintains.

When the consumer cares about the original author: configure their
verification policy to require the chain to terminate in a known
identity, run `ocm verify cv --policy ...` in their pipeline.

## Security analysis

**Threat: scanner false-positive rewrites a string that happened to
match a resource's access but was meant to stay frozen.** Mitigation:
digest-pinning requirement for convention-scan eligibility makes
collisions arbitrarily improbable. Author can suppress per-line via
`# ocm: skip`.

**Threat: annotation forges a `from=` pointer to a resource the author
doesn't control.** Cannot happen — `from=` resolves within the same
descriptor; resource access was set by the same author and signed by
the same identity.

**Threat: transfer agent compromised, rewrites maliciously and
re-signs.** Same threat shape as today's transfer rewriting access on
descriptors, larger blast radius. Mitigation: consumer policy
requires the chain to walk back through trusted identities only. A
malicious hop's attestation is signed by its identity; if that
identity isn't in the allowed list, verification fails. Sigstore
keyless + Rekor logging makes the malicious hop publicly auditable.

**Threat: annotation injected into resource bytes after signing,
before transfer.** Cannot happen — bytes are part of the resource
digest, which is in the signed normalisation. Tampered bytes fail
descriptor signature verification at the transfer agent's verify
step before the localizer runs.

**Threat: attestation referrer omitted from a hop's output.** A
consumer who requires walk-to-root cannot find the hop's attestation,
fails verification. Operators who care will catch this in their
pipeline. There's no way for an attacker to *forge* a missing-link —
they can only fail to publish, which is detectable.

## Where the code lives

- `bindings/go/localization/` — new module. Pure functions: a
  convention scanner over YAML/JSON, an annotation parser, a
  rewriter. No I/O.
- `bindings/go/transfer/internal/` — new transformation node
  `LocalizeResource` emitted when `--localize` is set, sitting
  between resource Get and Add. Calls into
  `bindings/go/localization`.
- `bindings/go/attestation/` — new module. Builds SLSA Provenance v1
  statements, wraps in DSSE, signs via sigstore keyless or static
  keys. Walks referrer chains.
- `bindings/go/signing/` — extension to surface the agent's identity
  cleanly to the attestation builder.
- `bindings/go/oci/` — already speaks manifests; small extension to
  attach attestations as referrers and list them on retrieval.
- CLI: `--localize`, `--reauthor` flags on `ocm transfer cv`;
  `ocm verify cv --policy`, `ocm history cv`, `ocm diff cv` verbs.

## Phasing

1. **Phase 1 — convention scanner + transfer node, single resource
   type (helmValues / helmChart).** Local-only rewrite path; resigning
   uses existing OCM signature mechanism only (no attestations yet).
   Gets the rewrite story landed.
2. **Phase 2 — SLSA Provenance attestations, sigstore keyless,
   referrer-based chain.** `--localize` becomes the user-visible
   flag. `ocm history cv` lands.
3. **Phase 3 — annotation override.** Parser, comment-attachment
   logic, format-specific handling.
4. **Phase 4 — additional resource types** (plain k8s manifests,
   kustomize bases). Same convention/annotation contract.
5. **Phase 5 — policy verification.** `ocm verify cv --policy`,
   `ocm diff cv`, `--reauthor` chain-break handling.

Phase 1 alone is shippable to users who don't care about the chain
(it just resigns with a fresh signature, no provenance). Phase 2 is
where the multi-hop story becomes real.

## Open questions

- **Annotation suppression syntax.** `# ocm: skip` is the current
  proposal. Confirm before phase 3.
- **Multi-line value annotations.** YAML node anchoring vs. line
  attachment. Probably "applies to the YAML node anchored by the
  next line." Needs spec'ing.
- **Sigstore identity for unattended transfer agents.** OIDC issuer,
  Rekor instance, cert-chain trust roots. Worth a short ADR before
  phase 2.
- **Policy DSL.** Defer until phase 5; start with policy-controller
  CEL and only build our own DSL if it falls short.
- **Behavior when sibling resources have non-OCI access.** Scanner
  has nothing to match against. Probably: warn, transfer normally
  without localization. Decide before phase 1.
- **`buildType` URI scheme.** `https://ocm.software/buildtypes/transfer-localize/v1`
  as the recommendation. Lock it down before phase 2 ships externally.
- **Component resolution by manifest digest.** For airgap consumers to
  diff or verify prior-hop component versions reconstructed from the
  attestation chain, OCM needs to resolve a component reference by its
  OCI manifest digest (not just by `name:version` tag). Today
  `ocm get cv` and `ocm download resource` only accept tag references;
  prior-hop manifests carried into the airgap (by `oras cp@digest`)
  are reachable via `oras` but not via `ocm`. The mock works around
  this by reading from the local workspace; production needs first-class
  digest support, e.g. `ocm get cv <repo>//<comp>@sha256:<digest>`.
- **Carrying the chain inside the component itself.** As an alternative
  (or complement) to the OCI 1.1 referrers + cross-registry `oras cp`
  approach, the component descriptor could carry the prior-hop snapshots
  and DSSE attestations as a dedicated resource (e.g.
  `provenance-chain` of type `application/vnd.in-toto.chain+tar`).
  Tradeoffs: portable across non-referrers-aware registries, walks
  become local file reads, signature verification of prior hops works
  from descriptor bytes alone (no separate digest lookup). Cost:
  loses the OCI-native referrers semantics, descriptor gets larger,
  any rewrite of an earlier hop's bytes invalidates the chain resource
  and forces a re-attest. Worth investigating before committing
  exclusively to the referrers model.

## Related code

- `bindings/go/transfer/internal/graph.go` — graph builder; new
  `LocalizeResource` node slots in here.
- `bindings/go/transfer/internal/oci.go`,
  `bindings/go/transfer/internal/helm.go` — current per-access
  handlers; localization runs after Get and before Add.
- `bindings/go/descriptor/normalisation/json/v4alpha1/normalisation.go`
  — confirms resource bytes (via `digest`) are signed; resigning is
  required.
- `bindings/go/signing/digest.go`,
  `bindings/go/signing/interface.go` — extension point for emitting
  the agent identity to attestations.
- `kubernetes/controller/examples/helm-configuration-localization/`,
  `website/content/docs/tutorials/deploy-helm-chart-bootstrap.md` —
  the deploy-time pattern this proposal aims to make optional.

## References

- Renovate helm-values manager —
  [docs](https://docs.renovatebot.com/modules/manager/helm-values/).
- Renovate regex/custom manager —
  [regex docs](https://docs.renovatebot.com/modules/manager/regex/),
  [config option](https://docs.renovatebot.com/configuration-options/#customManagers).
- in-toto attestation spec v1 —
  [statement](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md).
- in-toto Link predicate v0.3 —
  [spec](https://github.com/in-toto/attestation/blob/main/spec/predicates/link.md).
- SLSA Provenance v1 —
  [spec](https://slsa.dev/spec/v1.0/provenance).
- DSSE envelope —
  [spec](https://github.com/secure-systems-lab/dsse/blob/master/envelope.md).
- OCI distribution spec, referrers API —
  [spec](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers).
