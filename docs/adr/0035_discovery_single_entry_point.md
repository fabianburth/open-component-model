# Discovery as the Single Entry Point

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-11-13

**Technical Story**: Users treat `Discovery` combined with KRO as their one stop shop: it consumes many resources
without one `Resource` CR each. Two gaps stand in the way. `Discovery` does not verify what it outputs, and it
still needs a hand-wired `Repository` and `Component` underneath. This ADR decides where `Discovery` sits in the
controller's layering, how it establishes trust, how it obtains its root component version, and how deployment
and bootstrap build on it.

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

**Bootstrap.** The `Deployer` applies exactly one `Resource`, typically an RGD shipped in the component. Getting
that first RGD into a cluster takes four hand-written objects (`Repository`, `Component`, `Resource`,
`Deployer`), and deploying several artifacts of a component needs one `Resource` and one `Deployer` each.

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
* **Descriptors**: every edge of the traversed graph is checked: each descriptor against the digest each of its
  incoming references records. A descriptor counts as anchored only through an unbroken chain of digests from the
  root; a digest recorded by an unanchored parent proves nothing. Checking only the paths to surviving components
  is a follow-up for cost.
* **Resources**: only those that survive the selectors are verified, and their accesses are pinned to the
  verified digest.
* **Output**: extraction and the raw descriptor output only see verified, pinned data.
* **API**: `spec.verificationPolicy` (`Always` default, `Never`), as on `Resource`.
  * `Always`: the digest chain must be complete. The root digest is present on the `Component` and every reference
    of the traversed graph carries a complete digest (value, hash and normalisation algorithm), otherwise
    `Discovery` is not `Ready` (retried, not terminal; the message lists the gaps). Surviving resources are verified
    and pinned.
  * `Never`: resources are published as declared. Missing digests are only reported as a warning, but every
    descriptor digest that is present is still checked, and a mismatch fails.

  `Resource` and `Discovery`
  share one verified-walk implementation; a reference path is a walk with a single branch.
* **Pros**: one CR with the guarantees of `Resource`. Verification only reruns when the root digest changes, and
  pinned output makes skipping it safe.
* **Cons**: cost follows the *selected* set, not the set actually used downstream. Access types that can only be
  verified by download (git, GitHub, S3, Helm or wget without a checksum) are expensive. One failing resource
  fails the whole `Discovery`. Two CRDs overlap at the consumption layer (resolved in Part 3).

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

## Part 3: Deployment and Bootstrap

Ordering is out of scope for all options: like today's `Deployer`, an applied set has no ordering. Compositions
that need it use KRO.

### Option D1: Deployer keeps applying one Resource (status quo)

* **Pros**: no change.
* **Cons**: bootstrap needs four objects. N artifacts need N `Resource` and N `Deployer` objects. Keeps two
  consumption primitives.

### Option D2: Deployer with an inline source

The `Deployer` takes an inline `source` plus a resource identity and generates `Repository`, `Component` and
`Resource`.

* **Pros**: one object for bootstrap.
* **Cons**: bootstrap stays a special path, limited to one artifact. The `Deployer` generates the whole stack, and
  both consumption primitives remain.

### Option D3: Deployer applies a Discovery

The three questions get one primitive each:

| Question | Primitive |
| --- | --- |
| **Where** is the trusted version? | `Repository`, `Component` |
| **What** is consumed from it? | `Discovery` |
| **How** does it get into the cluster? | `Deployer` |

* `Discovery` always publishes a controller-owned `status.resources[]`. Each entry has the shape of today's
  `Resource` status (`component`: repository, name, version, digest; `resource`: identity, pinned access, digest)
  plus a `referencePath`: the references from the root to the entry's component version, the last of which recorded
  `component.digest`, empty for the root. `component.repositorySpec` is the root's repository, from which the
  `Discovery` resolves the whole graph, routing the root to it ahead of configured resolvers. A `Resource` *is* one
  entry of a `Discovery`. `extracted` stays an optional, user-projected view next
  to it.
* The `Deployer` references a `Discovery` and applies every entry as one ApplySet once the `Discovery` is `Ready`
  and has observed its current generation. An entry that drops out of the selection is pruned, so an empty
  `status.resources` prunes everything the `Deployer` applied. Two entries rendering the same object fail the
  apply. An entry the `Deployer` cannot apply is an error, not skipped: a deploy artifact such as an
  RGD is often an untyped `blob`, so "deployable" could only be guessed from the content. Images and charts are
  selected by a separate `Discovery` inside the deployed composition, not by the bootstrap `Discovery`.
* The `Deployer` does not trust the entries as a location or an anchor, exactly as on the `resourceRef` path: it
  follows the `Discovery` to its root `Component`, re-verifies the root signature with the verifications of its
  effective configuration (inherited from the `Discovery`), and resolves each entry's component version from that
  root through `referencePath`, each step anchored by its reference digest. `component.repositorySpec` and
  `component.digest` stay informational; the resolved component version must match them, or the entry fails.
* Bootstrap is the same two generic objects for every product, as Flux bootstraps with a source plus a
  `Kustomization`; the `ocm` CLI can write the pair. A `NamespacedDeployer` additionally needs its
  `ServiceAccount` and RBAC, and a private or signed source a `Secret` with its `.ocmconfig`:

  ```text
  Discovery (inline source, resourceSelector: deploy artifacts) ──► Deployer (discoveryRef)
  ```

* KRO `forEach` over `status.resources` stays available when every entry needs its own templated object, e.g. a
  `HelmRelease` per chart.
* **Pros**: bootstrap is no special case. One consumption primitive and one output contract for the `Deployer`,
  KRO and humans. Removal is handled by ApplySet pruning. The threat model is unchanged: like for a `Resource`, the
  `Deployer` takes only the selection from the status and re-establishes the chain from the signed root itself.
* **Cons**: breaking `Deployer` API change, softened by keeping `resourceRef` during migration. One failing entry
  fails the whole apply.

## Decision Outcome

Chosen options: **T1** (Discovery is the consumption primitive and verifies what it outputs), **E2** (inline
source rendered into child objects) and **D3** (Deployer applies a Discovery).

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
* **D3**: the `Deployer` is the layer above consumption and consumes it through one contract. Bootstrap, nested
  components and "apply every deploy artifact" are one path. D2 makes the `Deployer` generate the whole stack and
  keeps bootstrap special; D1 keeps the object count.
* **Together**: the `Component` verifies the root signature, `Discovery` anchors on that digest, and the
  `Deployer` applies only what `Discovery` selected, re-resolved from the signed root along the same digest chain.
  This holds whether the `Component` is referenced or generated.

### Consequences / Trade-offs

* **Resource is deprecated.** `Discovery` with an identity selector replaces it, and the shared entry shape keeps
  migration mechanical. `Deployer.spec.resourceRef` is kept until `Resource` is removed. Flux and Argo CD
  integrations that read `Resource` status read a `status.resources[]` entry instead.
* **Output contract.** `status.resources[]` is the API other tools build on and needs the same stability as a
  CRD field. Whether the raw `components` output and `extract` are still needed next to it is a follow-up.
* **Selector is a security and cost boundary.** Verification covers exactly what the selectors keep, because what
  the extraction uses cannot be determined. Without a `resourceSelector`, every resource of every surviving
  component is verified.
* **Cost compared with N Resource CRs**: about equal when the selector matches what is used, higher when it is
  broad. Download-only access types dominate; caching verified (access, digest) pairs is the main mitigation.
* **Behavioural change**: existing `Discovery` objects over unsigned graphs, references without a digest, or
  unverifiable access types fail under `Always`. `Never` is the opt-out.
* **Generated children**: they get deterministic names (`<name>-repository`, `<name>-component`), owner
  references and are deleted when the `Discovery` switches to a `componentRef`, unless it references the generated
  `Component`. Edits to generated objects are overwritten. An existing object with a generated name that the
  `Discovery` does not control is never adopted; the `Discovery` reports a conflict instead.
* **Trust assumption**: anchoring on `Component` status relies on that status being writable only by the
  controller. `Discovery` status is not trusted for where or what a component version is: whoever can write it can
  only change the selection, within the graph of the signed root.
* Supersedes the "per-descriptor digest verification" follow-up of [ADR 0027](0027_ocm_k8s_toolkit_discover.md).
* `Always` is the default. Follow-up: a `Discovery` workload in the controller performance benchmark, to measure
  its cost.
