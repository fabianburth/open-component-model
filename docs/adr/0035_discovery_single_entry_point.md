# Discovery as the Single Entry Point

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-11-13

**Technical Story**: Users treat `Discovery` combined with KRO as their one stop shop: it consumes many resources
without one `Resource` CR each. Two gaps stand in the way. `Discovery` does not verify what it outputs, and it
still needs a hand-wired `Repository` and `Component` underneath. This ADR decides how `Discovery` establishes
trust and how it obtains its root component version.

## Context and Problem Statement

The controller is built from layered primitives. Each one reconciles one concern and references the layer below:

```text
Repository ──► Component ──► Resource ──► Deployer
                    └──────► Discovery ──► (KRO, ...)
```

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
* Consistency with the other convenience CRs (the `source` of `OCMHelmRelease` in the proposed ADR 0033,
  [#4101](https://github.com/open-component-model/open-component-model/pull/4101)).
* Bounded cost on large component graphs and on etcd.

## Part 1: Trust

### Option T1: Discovery verifies what it outputs

* **Root**: anchored to the digest of the ready `Component`, which verified it by signature.
* **Descriptors**: those on a path from the root to a surviving component are checked against their parent
  reference digests. All other descriptors are only traversed.
* **Resources**: only those that survive the selectors are verified, and their accesses are pinned to the
  verified digest.
* **Output**: extraction and the raw descriptor output only see verified, pinned data.
* **API**: `spec.verificationPolicy` (`Always` default, `Never`), as on `Resource`. A shared verified-walk
  implementation serves `Resource` (a walk with a single branch) and `Discovery`.
* **Pros**: one CR with the guarantees of `Resource`. Verification only reruns when the root digest changes, and
  pinned output makes skipping it safe.
* **Cons**: cost follows the *selected* set, not the set actually used downstream. Access types that can only be
  verified by download (git, GitHub, S3, Helm or wget without a checksum) are expensive. One failing resource
  fails the whole `Discovery`.

### Option T2: Discovery plans, generated Resource CRs verify

`Discovery` only emits pointers (reference path, resource identity). KRO `forEach`, or `Discovery` itself,
generates one `Resource` per pointer.

* **Pros**: reuses the existing verification path. It verifies exactly what is used. `Deployer`, Flux and Argo CD
  integrations work unchanged.
* **Cons**: N objects and N reconciles. Each `Resource` repeats the root signature check and the walk down its
  reference path. This brings back the object count users wanted to avoid, now generated instead of authored.

### Option T3: Resource with a selector (ResourceSet)

A bulk CRD
([ocm-project#296](https://github.com/open-component-model/ocm-project/issues/296)) verifies and pins N
resources in one object. `Discovery` stays an untrusted metadata query.

* **Pros**: the guarantees are clear from the name.
* **Cons**: duplicates `Discovery`'s selectors, extraction and graph walk. Users must migrate.

### Option T4: Status quo, documented as untrusted

* **Pros**: no cost.
* **Cons**: users already deploy from `Discovery`. Documentation does not close the gap.

## Part 2: Entry Point

### Option E1: References only (status quo)

The user authors `Repository`, `Component` and `Discovery`, and wires them with references.

* **Pros**: explicit. One `Component` can be shared by many `Discovery` and `Resource` objects.
* **Cons**: three objects for one intent. A one-object setup needs an extra orchestration layer such as KRO.

### Option E2: Inline source rendered into child objects (Flux style)

Like Flux's `HelmRelease`, whose `spec.chart` template makes the helm-controller generate a `HelmChart` that the
source-controller reconciles, while `spec.chartRef` references an existing source.

* `Discovery` takes either `componentRef` (as today) or an inline `source` (`repositorySpec`, `component`,
  `semver`, `interval`, …), with the same shape as the `source` of `OCMHelmRelease`.
* From `source`, the controller generates and owns a `Repository` and a `Component`. The existing controllers
  reconcile them, and `Discovery` consumes the generated `Component` exactly as it would a referenced one.
* **Pros**: one authored object. No duplicated logic: semver resolution, polling, downgrade policy and root
  signature verification stay in the `Component` controller. The intermediate objects stay visible and
  debuggable. The pattern is the one convenience CRs already use.
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
* **Cons**: requires KRO. The composition is versioned separately from the CRDs, and that is exactly the
  hand-wiring the [OCMHelmRelease ADR](https://github.com/open-component-model/open-component-model/pull/4101)
  moves away from.

## Decision Outcome

Chosen options: **T1** (Discovery verifies what it outputs) and **E2** (inline source rendered into child objects).

### Justification

Both follow one design rule for the controller:

> **Primitives own one concern each; convenience CRs compose them by generating children, never by
> reimplementing them; trust is established once per layer, and consumers anchor on the verified status of the
> layer below.**

* **T1**: `Discovery` is already the API users deploy from, so the guarantee has to live there. Compared with N
  `Resource` CRs, it does the shared work once and adds no objects. T3 would rebuild `Discovery` under another
  name; T4 leaves the gap open.
* **E2**: gives the one-object experience without a second semver resolver or trust root. It keeps
  `componentRef` for sharing, as Flux keeps `chartRef`, and matches the `source` shape of `OCMHelmRelease`.
  E3 duplicates the `Component` controller; E4 depends on an external orchestrator.
* **Together**: the `Component` verifies the root signature, and `Discovery` anchors on that digest. This holds
  whether the `Component` is referenced or generated.

### Consequences / Trade-offs

* **Selector is a security and cost boundary.** Verification covers exactly what the selectors keep, because what
  the extraction uses cannot be determined. Without a `resourceSelector`, every resource of every surviving
  component is verified.
* **Cost compared with N Resource CRs**: about equal when the selector matches what is used, higher when it is
  broad. Download-only access types dominate; caching verified (access, digest) pairs is the main mitigation.
* **Behavioural change**: existing `Discovery` objects over unsigned graphs, references without a digest, or
  unverifiable access types fail under `Always`. `Never` is the opt-out.
* **Generated children**: they need deterministic names, owner references and a deletion policy, shared with the
  `OCMHelmRelease` design. Edits to generated objects are overwritten.
* **Trust assumption**: anchoring on `Component` status relies on that status being writable only by the
  controller.
* **Overlap with Resource**: a verified `Discovery` with an identity selector covers the single-resource case.
  Whether `Resource` stays a separate primitive or becomes that special case is left open.
* Supersedes the "per-descriptor digest verification" follow-up of [ADR 0027](0027_ocm_k8s_toolkit_discover.md).
* Follow-up: a `Discovery` workload in the controller performance benchmark before making `Always` the default.
