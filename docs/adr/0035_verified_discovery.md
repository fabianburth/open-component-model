# Verified Discovery

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-11-13

**Technical Story**: Users combine `Discovery` with KRO to consume many resources without one `Resource` CR each.
`Discovery` only queries descriptors and does not verify them, so this path bypasses the integrity guarantees a
`Resource` gives.

## Context and Problem Statement

A `Resource` CR establishes a chain of trust before it reports `Ready`:

1. The root component version is verified by signature.
2. Every component version on the reference path is verified against the digest its parent reference records.
3. The resource content is verified against the resource digest in the descriptor, and the access is pinned to
   that digest (e.g. `tag@sha256:…`).

`Discovery` ([ADR 0027](0027_ocm_k8s_toolkit_discover.md)) skips all three:

* It fetches the root again without checking it against the digest the `Component` verified.
* It resolves children by name and version only. The digests of their parent references are not checked, and a
  resolver may route them to a different repository.
* It does not verify resource content, and it publishes accesses as written in the descriptor, i.e. usually as
  mutable tags.

`Ready` on a `Discovery` therefore means "the query ran", not "the content is what was signed". Consumers such as
KRO deploy from `status.extracted` and pull by tag.

## Decision Drivers

* Same integrity guarantees, however a resource is consumed: what is deployed must be what was signed.
* Keep the single-CR ergonomics that made users choose `Discovery`.
* One verification model in the controller, not one per CRD.
* Bounded cost on large component graphs and on etcd.

## Considered Options

### Option 1: Discovery verifies what it outputs

#### High-Level Design

`Discovery` becomes a trust gate for its own output:

* **Root**: anchored to the digest of the ready `Component`, which the `Component` controller verified by
  signature. The root signature is not verified again.
* **Descriptors**: every descriptor on a path from the root to a surviving component is checked against its parent
  reference digest. All other descriptors are only traversed.
* **Resources**: only resources that survive the selectors are verified, and their accesses are pinned to the
  verified digest.
* **Output**: extraction and the raw descriptor output only see verified, pinned data. Nothing unverified reaches
  the status.

The trust model is the one `Resource` uses. Both controllers share one implementation of it, with a reference path
being a walk with a single branch.

#### Structural / API Changes

* `Discovery.spec.verificationPolicy` (`Always` default, `Never`), with the same meaning as on `Resource`.
* A status condition stating what was verified.
* A shared verified-walk component used by `Resource` and `Discovery`.

#### Pros and Cons

* **Pros**: one CR, the same guarantees as `Resource`, pinned output, single trust model. Thanks to the root-digest
  short-circuit, verification only runs when the root digest changes, and pinned output makes skipping safe.
* **Cons**: cost grows with the *selected* set, not the set actually used downstream; a broad selector verifies
  more than needed. Access types that can only be verified by download (git, GitHub, S3, Helm or wget without a
  checksum) are expensive. One failing resource fails the whole `Discovery`.

### Option 2: Discovery plans, generated Resource CRs verify

#### High-Level Design

`Discovery` stays a query that only emits pointers (reference path and resource identity). KRO (`forEach`) or
`Discovery` itself generates one `Resource` CR per pointer, and consumers read from those.

#### Structural / API Changes

* A pointer-shaped extraction result, or a `Discovery` that owns generated `Resource` CRs.
* No new verification code.

#### Pros and Cons

* **Pros**: a single verification path that already exists. It verifies exactly what is used. Existing consumers of
  `Resource` (Deployer, Flux, Argo CD) work unchanged.
* **Cons**: N objects in etcd and N reconciles. Each `Resource` repeats the root signature check and the walk down
  its reference path. Deleting many CRs is slow. Users get back the object count they wanted to avoid, now
  generated rather than authored.

### Option 3: Resource with a selector (ResourceSet)

#### High-Level Design

A bulk resource CRD (`ResourceID.BySelector`,
[ocm-project#296](https://github.com/open-component-model/ocm-project/issues/296)) verifies and pins N resources
in one object. `Discovery` stays an explicitly untrusted metadata query.

#### Structural / API Changes

* A new CRD or a selector mode on `Resource` with a list status.

#### Pros and Cons

* **Pros**: the guarantees are clear from the name. `Discovery` keeps its scope.
* **Cons**: a second CRD that largely duplicates `Discovery`'s selectors, extraction and graph walk. Users must
  migrate.

### Option 4: Status quo, documented as untrusted

#### High-Level Design

`Discovery` stays as is. The documentation states that its output must not be deployed from without verifying it
separately.

#### Pros and Cons

* **Pros**: no cost, no change.
* **Cons**: users already deploy from `Discovery`. Documentation does not close the gap.

## Decision Outcome

Chosen option: **[Option 1](#option-1-discovery-verifies-what-it-outputs)**.

### Justification

`Discovery` is already the API users deploy from, so the guarantee has to live there. Option 1 keeps one CR and
reuses the trust model of `Resource` instead of adding a second one.

Compared with N `Resource` CRs it does the shared work once (root, common reference paths), adds no objects, and
needs no generated-object lifecycle. Option 3 would rebuild `Discovery` under another name. Option 4 leaves the
gap open.

### Consequences / Trade-offs

* **Selector is now a security and cost boundary.** Verification covers exactly what the selectors keep, because
  the extraction cannot be analysed for what it uses. Users should select what they deploy. Without a
  `resourceSelector`, every resource of every surviving component is verified.
* **Cost compared with N Resource CRs**: about equal when the selector matches what is used. Higher when it is
  broad. Download-only access types dominate; caching verified (access, digest) pairs is the main mitigation.
* **Behavioural change**: existing `Discovery` objects over unsigned graphs, references without a digest, or
  unverifiable access types fail under `Always`. `Never` is the opt-out.
* **Trust assumption**: anchoring the root to the `Component` status relies on that status being writable only by
  the controller.
* Supersedes the "per-descriptor digest verification" follow-up of [ADR 0027](0027_ocm_k8s_toolkit_discover.md).
* Follow-up: a `Discovery` workload in the controller performance benchmark to confirm the cost before enabling
  `Always` by default.
