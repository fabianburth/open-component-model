# Discovery as the Single Entry Point

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-11-13

**Technical Story**: Users treat `Discovery` combined with KRO as their one stop shop: it consumes many resources
without one `Resource` CR each. Two gaps stand in the way. `Discovery` does not verify what it outputs, and it
still needs a hand-wired `Repository` and `Component` underneath. This ADR decides where `Discovery` sits in the
controller's layering, how it establishes trust, and how it obtains its root component version.

## Context and Problem Statement

The controller is built from layered primitives. Each one reconciles one concern and references the layer below:

| Layer | Concern | Primitive |
| --- | --- | --- |
| Source | where component versions live | `Repository` |
| Version | which version, verified by signature | `Component` |
| Consumption | which resources, verified by digest | `Resource`, `Discovery` |

`Resource` and `Discovery` both live in the consumption layer, but only `Resource` does the consumption layer's
job.

**Trust.** A `Resource` CR establishes a chain of trust before it reports `Ready`:

1. The root component version is verified by signature.
2. Every component version on the reference path is verified against the digest its parent reference records.
3. The resource content is verified against its digest, and the access is pinned to that digest.

`Discovery` ([ADR 0027](0027_ocm_k8s_toolkit_discover.md)) skips all three:

* It fetches the root again without checking it against the digest the `Component` verified.
* It resolves children by name and version only.
* It publishes accesses as written in the descriptor, usually as mutable tags.

Its `Ready` means "the query ran", not "the content is what was signed". Consumers such as KRO deploy from
`status.extracted` anyway.

**Entry point.** A working `Discovery` needs three authored objects: `Repository`, `Component` and `Discovery`.
Users who want one object end up writing a KRO definition just to wire them together.

## Decision Drivers

* Same integrity guarantees, however a resource is consumed: what is deployed must be what was signed.
* One authored object for the common case.
* One implementation of each concern: no second semver resolver, no second trust model.
* Bounded cost on large component graphs and on etcd.

## Part 1: Consumption Layer and Trust

### Option T1: Discovery is the consumption primitive and verifies what it outputs

`Discovery` is not a query layered on top of `Resource`. It is the general form of `Resource`: select N resources
from a component graph, verify them, publish them. A `Resource` is the case of one resource selected by identity.

* **Root**: anchored to the digest of the ready `Component`, which verified it by signature.
* **Descriptors**: those on a path from the root to a surviving component are checked against their parent
  reference digests. All other descriptors are only traversed.
* **Resources**: only those that survive the selectors are verified, and their accesses are pinned to the
  verified digest.
* **Output**: extraction and the raw descriptor output only see verified, pinned data.
* **API**: `spec.verificationPolicy` (`Always` default, `Never`), as on `Resource`. `Resource` and `Discovery`
  share one verified-walk implementation; a reference path is a walk with a single branch.
* **Pros**: one CR with the guarantees of `Resource`. Verification only reruns when the root digest changes, and
  pinned output makes skipping it safe.
* **Cons**: cost follows the *selected* set, not the set actually used downstream. Access types that can only be
  verified by download (git, GitHub, S3, Helm or wget without a checksum) are expensive. One failing resource
  fails the whole `Discovery`. Two CRDs overlap at the consumption layer until `Resource`'s future is decided.

### Option T2: Discovery plans, generated Resource CRs verify

`Discovery` sits above the consumption layer and only emits pointers (reference path, resource identity). KRO
`forEach`, or `Discovery` itself, generates one `Resource` per pointer.

* **Pros**: reuses the existing verification path. It verifies exactly what is used. `Deployer`, Flux and Argo CD
  integrations work unchanged.
* **Cons**: N objects and N reconciles. Each `Resource` repeats the root signature check and the walk down its
  reference path. This brings back the object count users wanted to avoid, now generated instead of authored.

### Option T3: Resource with a selector (ResourceSet)

A bulk CRD
([ocm-project#296](https://github.com/open-component-model/ocm-project/issues/296)) verifies and pins N
resources in one object. `Discovery` stays an untrusted metadata query.

* **Pros**: the guarantees are clear from the name.
* **Cons**: duplicates `Discovery`'s selectors, extraction and graph walk. Users must migrate. This is T1 under a
  new name.

### Option T4: Status quo, documented as untrusted

* **Pros**: no cost.
* **Cons**: users already deploy from `Discovery`. Documentation does not close the gap.

## Part 2: Entry Point

### Option E1: References only (status quo)

The user authors `Repository`, `Component` and `Discovery`, and wires them with references.

* **Pros**: explicit. One `Component` can be shared by many consumers.
* **Cons**: three objects for one intent. A one-object setup needs an extra orchestration layer such as KRO.

### Option E2: Inline source rendered into child objects (Flux style)

Like Flux's `HelmRelease`, whose `spec.chart` template makes the helm-controller generate a `HelmChart` that the
source-controller reconciles, while `spec.chartRef` references an existing source.

* `Discovery` takes either `componentRef` (as today) or an inline `source` (`repositorySpec`, `component`,
  `semver`, `interval`, …).
* From `source`, the controller generates and owns a `Repository` and a `Component`. The existing controllers
  reconcile them, and `Discovery` consumes the generated `Component` exactly as it would a referenced one.
* **Pros**: one authored object. No duplicated logic: semver resolution, polling, downgrade policy and root
  signature verification stay in the `Component` controller. The intermediate objects stay visible and
  debuggable.
* **Cons**: lifecycle of generated objects (ownership, naming, deletion, drift when a user edits them). Two extra
  objects per `Discovery`, though not N.

### Option E3: Discovery reconciles the source itself

`Discovery` embeds `repositorySpec` and `semver`. It resolves the version, polls and verifies the root signature
itself, with no intermediate objects.

* **Pros**: one object and nothing generated, so the smallest etcd footprint.
* **Cons**: duplicates the `Component` controller (semver, downgrade policy, polling, signature verification).
  `Discovery` turns from watch-driven into polling and becomes a monolith. The resolved version is no longer a
  shareable object, and the trust root moves into a second controller.

### Option E4: Shipped composition

OCM ships a KRO definition (or a Helm chart) that renders `Repository`, `Component` and `Discovery` from one
instance.

* **Pros**: no API change.
* **Cons**: requires KRO. The composition is versioned separately from the CRDs, and the hand-wiring is only
  moved into a definition OCM maintains, not removed.

## Decision Outcome

Chosen options: **T1** (Discovery is the consumption primitive and verifies what it outputs) and **E2** (inline
source rendered into child objects).

### Justification

Both follow one design rule for the controller:

> **Each layer has one primitive concern and one implementation of it. A primitive may generate the primitives
> of the layers *below* it for convenience, but it never reimplements them. Trust is established once per layer,
> and each layer anchors on the verified status of the layer below.**

* **T1**: `Discovery` and `Resource` are the same layer, so `Discovery` must carry the same guarantees, through
  the same implementation. Generating `Resource` children (T2) would treat a peer as a lower layer, at the cost of
  N objects. T3 is T1 under another name; T4 leaves the gap open.
* **E2**: `Repository` and `Component` are lower layers. Generating them gives the one-object experience without
  a second semver resolver or trust root, and keeps `componentRef` for sharing, as Flux keeps `chartRef`. E3
  reimplements the version layer; E4 depends on an external orchestrator.
* **Together**: the `Component` verifies the root signature, and `Discovery` anchors on that digest. This holds
  whether the `Component` is referenced or generated.

### Consequences / Trade-offs

* **Two consumption primitives.** With T1, a `Discovery` with an identity selector covers what a `Resource` does.
  Follow-up decision: keep `Resource` as a thin special case over the shared implementation, or deprecate it in
  favour of `Discovery`. Deprecation needs a way for `Deployer` and other `Resource` consumers to reference a
  single `Discovery` result.
* **Selector is a security and cost boundary.** Verification covers exactly what the selectors keep, because what
  the extraction uses cannot be determined. Without a `resourceSelector`, every resource of every surviving
  component is verified.
* **Cost compared with N Resource CRs**: about equal when the selector matches what is used, higher when it is
  broad. Download-only access types dominate; caching verified (access, digest) pairs is the main mitigation.
* **Behavioural change**: existing `Discovery` objects over unsigned graphs, references without a digest, or
  unverifiable access types fail under `Always`. `Never` is the opt-out.
* **Generated children**: they need deterministic names, owner references and a deletion policy. Edits to
  generated objects are overwritten.
* **Trust assumption**: anchoring on `Component` status relies on that status being writable only by the
  controller.
* Supersedes the "per-descriptor digest verification" follow-up of [ADR 0027](0027_ocm_k8s_toolkit_discover.md).
* Follow-up: a `Discovery` workload in the controller performance benchmark before making `Always` the default.
