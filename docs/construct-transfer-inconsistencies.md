# Constructor vs Transfer: Inconsistency Analysis and Alignment Strategy

## Executive Summary

The `constructor` (`bindings/go/constructor/`) and `transfer` (`bindings/go/transfer/`) packages both generate `TransformationGraphDefinition` DAGs executed by the shared graph engine in `bindings/go/transform/`. Despite identical infrastructure (CEL-based DAG, `transformv1alpha1.TransformationGraphDefinition`, `builder.Builder`), the two implementations diverged significantly during independent development.

**Key findings:**

- **2 bugs** requiring immediate fixes (source processing CEL reference, `runtime.Raw` handling gap).
- **5 critical duplication issues** where identical or near-identical code is copied across packages.
- **1 architectural design issue** where input transformers lack a library abstraction layer, unlike access-type transformers.
- **1 module structure issue** where the constructor module layout does not mirror the descriptor pattern, trapping lightweight types in a heavy module.
- **4 moderate architectural divergences** that complicate future unification.
- **7 intentional or low-priority differences** reflecting legitimate domain differences.

The primary alignment strategy is to **stop using the `Environment` field in both graph generators** and inline static data directly into transformation specs. The `Environment` field remains available in `TransformationGraphDefinition` for external or manually authored specs, but the constructor and transfer graph generators no longer populate it. The environment is a CEL constant — never mutated, never creating DAG edges — so the indirection it introduces in the generators is pure overhead that caused the v1/v2 format split and three separate `buildDescriptorSpec` functions.

---

## Critical Issues

### C-1: Source Processing Bug -- Static `sourceMap` in AddLocalSource Spec

**Severity:** Bug (silent data loss)

**Files:**
- `bindings/go/constructor/internal/graph/source.go` (line 94)
- `bindings/go/constructor/internal/graph/input.go` (line 106, correct pattern)

**Problem:** The AddLocalSource transformation spec uses the literal `sourceMap` variable for the `"source"` field. This is the *pre-input-transformation* version. Any modifications the input transformer makes to the source metadata (e.g., computed media type, updated labels) are silently discarded.

Compare:
```go
// input.go line 106 -- CORRECT: references input transformer output
"resource": fmt.Sprintf("${%s.output.resource}", inputID),

// source.go line 94 -- BUG: uses static pre-transformation map
"source": sourceMap,
```

**Fix:** `"source": fmt.Sprintf("${%s.output.source}", inputID)`

---

### C-2: `runtime.Raw` Handling Gap in Constructor

**Severity:** Latent bug (crash on Raw repository specs)

**Files:**
- `bindings/go/constructor/internal/graph/helpers.go` (lines 55-88)
- `bindings/go/transfer/internal/helpers.go` (lines 29-90)

**Problem:** Transfer's `chooseAddType`, `chooseGetLocalResourceType`, and `chooseAddLocalResourceType` handle `*runtime.Raw` via `convertToConcreteRepo()`. Constructor's equivalents do a direct type switch and return `"unsupported repository type *runtime.Raw"` if the input is not already concrete.

If a repository spec arrives as `*runtime.Raw` (which happens when specs are deserialized from JSON without a pre-registered type), the constructor will error.

---

### C-3: Duplicated `identityToTransformationID` Function

**Severity:** Code duplication (maintenance hazard)

**Files:**
- `bindings/go/constructor/internal/graph/helpers.go` (lines 17-39, prefix `"construct"`)
- `bindings/go/transfer/internal/discovery.go` (lines 231-262, prefix `"transform"`)

**Problem:** Identical algorithm (sort keys, split on `.,/-`, camelCase join), differing only in the initial prefix word. Both also duplicate `var toWordRunes`.

**Fix:** Extract to shared package with parameterized prefix:
```go
func IdentityToTransformationID(prefix string, id runtime.Identity) string
```

---

### C-4: Duplicated `asUnstructured` Function

**Severity:** Code duplication

**Files:**
- `bindings/go/constructor/internal/graph/helpers.go` (lines 42-52)
- `bindings/go/transfer/internal/helpers.go` (lines 16-26)

**Problem:** Byte-for-byte identical. Both convert `runtime.Typed` -> `runtime.Raw` -> `runtime.Unstructured`.

---

### C-5: Duplicated `addDescriptorToEnvironment` Function

**Severity:** Code duplication (superseded by Strategy 1)

**Files:**
- `bindings/go/constructor/internal/graph/graph.go` (lines 370-381)
- `bindings/go/transfer/internal/graph.go` (lines 295-306)

**Problem:** Identical implementation. Marshals `*descriptorv2.Descriptor` to JSON, unmarshals to `map[string]any`, stores in `tgd.Environment.Data`.

**Note:** Strategy 1 (inline static data) deletes both copies entirely rather than extracting to a shared package.

---

### C-6: `AddLocalSource` Type Constants Defined as Workaround

**Severity:** Technical debt

**File:** `bindings/go/constructor/internal/graph/helpers.go` (lines 90-96)

**Problem:** `ociAddLocalSourceV1alpha1` and `ctfAddLocalSourceV1alpha1` are manually defined with a comment: *"the published OCI module version may not yet include them."* These duplicate what should be canonical constants in `ociv1alpha1`.

**Fix:** Publish these constants in the `ociv1alpha1` package.

---

## Moderate Issues

### M-1: Environment Data Format Divergence (v1 vs v2)

**Severity:** Superseded by Strategy 1

**Files:**
- `bindings/go/constructor/internal/graph/graph.go` (lines 383-401, 610-738)
- `bindings/go/transfer/internal/graph.go` (lines 295-306, 344-379)

**Problem:**
- Constructor stores constructor components in **v1 spec format** (`ConvertToV1Component`) and external components in **v2 descriptor format**.
- Transfer always stores **v2 descriptor format**.

This forces two completely different descriptor assembly functions:
- `buildConstructorDescriptorSpec` (~130 lines) maps v1 field paths: top-level `name`, `version`, `provider.name`, `componentReferences[i]`.
- `buildDescriptorSpec` maps v2 paths: `component.name`, `component.version`, `component.provider`.

The constructor maintains *both* functions. This is the largest source of code divergence.

**Impact:** Any future change to descriptor assembly logic must be made in 3 places (constructor v1 variant, constructor v2 variant, transfer v2 variant).

**Resolution:** Strategy 1 eliminates this — graph generators no longer write to the environment, so there is no format to diverge on.

---

### M-2: Descriptor Assembly Style Inconsistency

**Severity:** Superseded by Strategy 1

**Files:**
- `bindings/go/transfer/internal/graph.go` (lines 383-389, `setOptionalField`)
- `bindings/go/constructor/internal/graph/graph.go` (lines 576-600)

**Problem:**
- Transfer uses a clean `setOptionalField` helper for nil/present branching.
- Constructor manually checks each optional field with explicit `if` blocks and literal `nil` assignments.

**Resolution:** Strategy 1 deletes all three `buildDescriptorSpec` variants and `setOptionalField`. Descriptor data is inlined as literal maps — no CEL path construction, no nil/present branching.

---

### M-3: Digest Handling Asymmetry

**Files:**
- `bindings/go/constructor/internal/graph/graph.go` (lines 407-436, `addComputeDigestTransformation`)
- `bindings/go/constructor/internal/graph/input.go` (lines 121-162, `ProcessOCIResourceDigest`)
- Transfer: no equivalent transformations

**Problem:**
- Constructor computes component-level digests (`ComputeComponentDigest`) and resource-level digests (`ProcessOCIResourceDigest`) via dedicated transformers.
- Transfer has **neither**. It only verifies reference digests during discovery (`multiResolver.Resolve`, discovery.go lines 100-109).
- Transfer does not update reference digests when transferring.

**Impact:** A transferred component version may carry stale reference digests if the referenced component was also transferred and its content changed.

---

### M-4: External Component Resolution Interface Mismatch

**Files:**
- `bindings/go/constructor/internal/graph/types.go` (`ExternalComponentRepositoryProvider` -- 1 method)
- `bindings/go/repository/component/resolvers/` (`ComponentVersionRepositoryResolver` -- 3 methods)

**Problem:**
- Constructor uses `ExternalComponentRepositoryProvider` with `GetExternalRepository(ctx, name, version)`.
- Transfer uses `ComponentVersionRepositoryResolver` with `GetRepositorySpecificationForComponent`, `GetComponentVersionRepositoryForSpecification`, `GetComponentVersionRepositoryForComponent`.
- Transfer tracks `SourceRepository` in discovery values for Get transformations; constructor does not.

**Impact:** Constructor cannot participate in resolver-based workflows. When by-value resource copying is implemented (currently TODO), constructor will need source repo tracking.

---

## Minor / Intentional Differences

| ID | Difference | Assessment |
|----|-----------|------------|
| L-1 | ID prefix `"construct"` vs `"transform"` | Beneficial for debugging. Non-issue once C-3 is resolved. |
| L-2 | Transfer has no source handling | Intentional. Transfer moves existing components; it doesn't process source inputs. |
| L-3 | Conflict policy only in constructor | Intentional. Constructor creates new versions that might already exist. |
| L-4 | Multi-target only in transfer | Intentional. Constructor builds one output; transfer fans out. |
| L-5 | Different resource access type breadth | Complementary. Constructor handles inputs; transfer handles Get/Add chains for LocalBlob, OCI, Helm. |
| L-6 | Upload type modes only in transfer | Constructor doesn't need this flexibility today. |
| L-7 | Different builder transformer registration sets | Correct -- each builder is tailored to its domain. |

---

## Architectural Issues

### A-1: Input Transformers Lack a Library Abstraction Layer

**Severity:** Design issue (limits library usability)

**Problem:** Access-type transformers and input-type transformers follow fundamentally different layering:

**Access-type transformers** (OCI, Helm, LocalBlob) follow a clean two-layer architecture:
1. **Library layer** — `repository.ComponentVersionRepository.GetLocalResource()`, `repository.ResourceRepository.DownloadResource()`. Reusable by any Go consumer without the transformation graph.
2. **Transformer layer** — thin wrapper that calls the library, buffers to a file, populates the transformation output.

```
GetLocalResource transformer
  └─ calls repo.GetLocalResource()        ← library abstraction, usable standalone
  └─ buffers to file
  └─ populates output

GetOCIArtifact transformer
  └─ calls ResourceRepository.DownloadResource()  ← library abstraction, usable standalone
  └─ buffers to file
  └─ populates output
```

**Input-type transformers** (File, Dir, UTF8, Helm input) collapse both layers into the transformer:
1. **Library layer** — partial: `file.GetV1FileBlob()`, `dir.GetV1DirBlob()` return a `blob.ReadOnlyBlob`, but do *not* produce the resource descriptor.
2. **Transformer layer** — calls the blob function, constructs the resource descriptor (media type, relation, version defaults), buffers to file, populates output. The resource descriptor construction is embedded in the transformer.

```
FileInput transformer
  └─ calls file.GetV1FileBlob()            ← only returns blob, not resource descriptor
  └─ constructs resource descriptor         ← embedded in transformer, not reusable
  └─ buffers to file
  └─ populates output
```

A library user who wants to process a file input spec without the transformation graph has no standalone function to call. The resource descriptor construction logic (setting media type from blob, defaulting version, setting relation to `local`) is locked inside the transformer.

**Files:**
- `bindings/go/input/file/transformation/file_input.go` — resource construction at line 70 (`ConvertResourceToV2`)
- `bindings/go/input/dir/transformation/dir_input.go` — same pattern
- `bindings/go/input/utf8/transformation/utf8_input.go` — same pattern
- `bindings/go/helm/input/transformation/helm_input.go` — same pattern
- `bindings/go/input/file/blob.go` — `GetV1FileBlob()` returns blob only
- `bindings/go/oci/transformer/get_local_resource.go` — correct pattern: library call + thin wrapper

**Historical context:** The old `ResourceInputMethod` / `SourceInputMethod` interfaces in `constructor/interface.go` were designed as this missing library layer. They defined a `ProcessResource(ctx, resource, credentials) → (ProcessedResource | ProcessedBlobData)` contract — exactly "given an input spec, produce a resource descriptor + blob." When the transformation architecture replaced sequential orchestration with graph-based execution, the orchestration was correctly replaced, but the library abstraction was accidentally consumed into the transformers. Each input package had an `InputMethod` type implementing these interfaces, deleted in commit `4f2e8f910` ("initial ai refactoring of constructor"):

| Package | Deleted file | `InputMethod` implemented |
|---------|-------------|--------------------------|
| `input/file` | `method.go` | `ResourceInputMethod`, `SourceInputMethod` |
| `input/dir` | `method.go` | `ResourceInputMethod`, `SourceInputMethod` |
| `input/utf8` | `method.go` | `ResourceInputMethod`, `SourceInputMethod` |
| `helm/input` | `method.go` | `ResourceInputMethod` only |

These were straightforward: convert input spec via scheme → call the existing blob function (`GetV1FileBlob`, `GetV1DirBlob`, `GetV1UTF8Blob`, `GetV1HelmBlob`) → return `ProcessedBlobData` (and optionally `ProcessedResource` for remote Helm charts).

The interfaces and their current status:

| Interface | Status | Consumers |
|-----------|--------|-----------|
| `ResourceInputMethod` | Dead | Only plugin registry (`GetResourceInputPlugin` — never called outside tests) |
| `SourceInputMethod` | Dead | Only plugin registry (`GetSourceInputPlugin` — never called outside tests) |
| `ResourceInputMethodResult` | Dead | Only test assertions |
| `SourceInputMethodResult` | Dead | Only plugin converter (never called) |
| `ResourceConsumerIdentityProvider` | Dead (constructor version) | Embedded in dead `ResourceInputMethod`; helm has its own separate version |
| `SourceConsumerIdentityProvider` | Dead | Embedded in dead `SourceInputMethod` |

Note: `ResourceDigestProcessor` and `ExternalComponentRepositoryProvider` remain live — they are used in the actual graph building and execution flow.

---

### A-2: Constructor Module Structure Does Not Mirror Descriptor Module Structure

**Severity:** Design issue (coupling, dependency weight)

**Problem:** The `descriptor/` directory cleanly separates concerns into independent modules:

```
descriptor/
├── v2/             (module)  ← serialization format, lightweight (runtime, yaml)
├── runtime/        (module)  ← runtime types + conversions (runtime, descriptor/v2)
├── normalisation/  (module)  ← digest normalization
```

The `constructor/` directory does not follow this pattern:

```
constructor/                  (current)
├── spec/v1/        (module)  ← serialization format (already separate, lightweight)
├── runtime/        (subpkg)  ← runtime types — NOT a module, locked inside constructor
├── spec/transformation/      (subpkg)
├── transformer/              (subpkg)
├── internal/graph/           (subpkg)
├── interface.go              ← library interfaces (ResourceInputMethod, etc.)
├── constructor.go            ← BuildGraphDefinition (graph orchestration)
├── builder.go                ← NewDefaultBuilder (transformer registration)
└── go.mod                    ← one monolithic module
```

**Consequences:**

1. **`constructor/runtime` is not independently importable.** Any package that needs the constructor runtime types (`Resource`, `Source`, `AccessOrInput`) must depend on the full `constructor` module, pulling in the graph-building machinery, all input/transformer packages, CEL, DAG, etc. This affects `input/file`, `input/dir`, `input/utf8`, `helm/input`, and `plugin`.

2. **Library interfaces are trapped in the heavy module.** `ResourceInputMethod`, `SourceInputMethod`, and their result types live in `constructor/interface.go`. Input packages that want to implement these interfaces must import the full `constructor` module.

3. **`constructor/spec/v1` is misnamed.** The constructor format is based on the v2 descriptor schema. The `v1` name is the spec version, not the descriptor version, but this creates confusion when both `constructor/spec/v1` and `descriptor/v2` coexist.

4. **`ResourceDigestProcessor` is duplicated.** It's defined in both `constructor/interface.go` and `repository/interface.go` with the same signature. The constructor version should be dropped in favor of the `repository` one (which is already used by the actual transformers).

**Current dependency flow:**
```
input/file ──→ constructor (full module) ──→ dag, cel, transform, oci, helm, ...
               just to get constructor.ResourceInputMethod
```

**Target dependency flow:**
```
input/file ──→ constructor/runtime (lightweight module)
               gets ResourceInputMethod, Resource, Source types only
```

---

## Alignment Strategies

### Strategy 1: Stop Using Environment in Graph Generators — Inline Static Data

**Goal:** Have constructor and transfer graph generators stop populating `TransformationGraphDefinition.Environment`, inlining static data directly into transformation specs. This eliminates M-1, M-2, C-5, and the v1/v2 format split.

**Scope:** The `Environment` field remains in `TransformationGraphDefinition` as a general-purpose capability — external tooling or manually authored specs can still use it. The graph engine (`builder.BuildAndCheck`, `env.NewEnvBuilder`, CEL constant registration) continues to support it unchanged. Only the constructor and transfer graph generators change: they stop writing to `tgd.Environment.Data` and instead embed values directly in transformation specs.

**Rationale:** The graph generators store static data (component descriptors, resource maps, file access specs) in the environment and reference it via `${environment.X.Y}` CEL expressions. This indirection is unnecessary at the generator level:

1. **Environment is never mutated** — set once before graph execution.
2. **`environment` is a CEL constant** — registered via `cel.Constant("environment", ...)` in `env/builder.go:25`.
3. **No DAG edges** are created for `${environment.X}` — the `Inspector` only creates `ResourceDependency` edges for transformation IDs; `environment` resolves directly from the constant.
4. **All `${environment.X}` references resolve to literal values** known at graph-build time.

The indirection caused the v1/v2 format divergence (M-1), forced three `buildDescriptorSpec` variants (M-2), and duplicated `addDescriptorToEnvironment` (C-5). Removing it from the generators collapses all of these.

**What gets deleted from the generators:**

| Code | Location | Reason |
|------|----------|--------|
| `addDescriptorToEnvironment()` | `constructor/.../graph.go:370-381`, `transfer/.../graph.go:295-306` | Generators no longer populate environment |
| `addConstructorToEnvironment()` | `constructor/.../graph.go:383-401` | Generators no longer populate environment |
| `buildConstructorDescriptorSpec()` | `constructor/.../graph.go:610-738` | Descriptor inlined in upload spec |
| `buildDescriptorSpec()` (constructor) | `constructor/.../graph.go:509-601` | Descriptor inlined in upload spec |
| `buildDescriptorSpec()` (transfer) | `transfer/.../graph.go:344-379` | Descriptor inlined in upload spec |
| `setOptionalField()` | `transfer/.../graph.go:383-389` | No CEL path construction needed |
| Per-resource/source env entries | `constructor/.../graph.go:647,679` | Inline in upload spec |
| File access spec env entries | `constructor/.../graph.go:323-327` | Inline in AddLocalResource spec |
| `ConvertToV1Component()` call chain | `constructor/.../graph.go:388` | No v1 format needed |

**What stays unchanged:**

| Code | Location | Reason |
|------|----------|--------|
| `TransformationGraphDefinition.Environment` field | `transform/spec/v1alpha1/config.go` | General-purpose capability, still available |
| `GetEnvironmentData()` | `transform/spec/v1alpha1/config.go:17` | Used by `builder.BuildAndCheck` |
| `env.NewEnvBuilder()` | `transform/graph/env/builder.go` | Registers environment as CEL constant (handles empty/nil) |
| `builder.BuildAndCheck()` environment handling | `transform/graph/builder/builder.go:43-51` | Processes whatever environment is present |

**What changes in the generators:**

- **Upload transformation specs** (`AddComponentVersion`) carry the full descriptor as an inline literal `map[string]any` instead of a tree of `${environment.X.Y}` CEL expressions. Modified resources/sources still use `${addID.output.resource}` CEL refs (these are transformation-output references, not environment).
- **`ComputeComponentDigest`** specs that currently reference `${environment.baseID}` for external components without copy — inline the v2 descriptor map directly.
- **External blob file access** (`{type, uri}` maps) inlined directly in AddLocalResource specs.
- **Constructor always converts to v2 at graph-build time** — no v1 format anywhere in the generators.

**After this change, generator-produced CEL expressions only reference transformation outputs** (`${X.output.Y}`). The `${environment.X}` pattern is no longer emitted by the generators.

**Impact on `--dry-run` / `--transfer-spec`:**
- Dry-run output has an empty `environment` section; transformation specs are larger but self-contained and easier to read without cross-referencing.
- `--transfer-spec` round-trip is unaffected — `BuildAndCheck` handles empty environments. Manually authored specs that use the environment still work.

---

### Strategy 2: Shared Graph Utilities Package

**Goal:** Eliminate remaining code duplication (C-2, C-3, C-4).

**Proposed location:** `bindings/go/transform/internal/graphutil`

**Contents:**
1. `IdentityToTransformationID(prefix string, id runtime.Identity) string`
2. `AsUnstructured(typed runtime.Typed) (*runtime.Unstructured, error)`
3. `ConvertToConcreteRepo(repo runtime.Typed, scheme *runtime.Scheme) (runtime.Typed, error)`
4. `ChooseAddType`, `ChooseGetLocalResourceType`, `ChooseAddLocalResourceType` using `ConvertToConcreteRepo`

**Note:** `AddDescriptorToEnvironment` is no longer needed (deleted by Strategy 1).

**Impact:** Resolves C-2, C-3, C-4 in one PR.

---

### Strategy 3: Publish AddLocalSource Type Constants

**Goal:** Remove C-6 workaround.

**Approach:** Add `OCIAddLocalSourceV1alpha1` and `CTFAddLocalSourceV1alpha1` to the `ociv1alpha1` package.

---

### Strategy 4: Restructure Constructor Modules to Mirror Descriptor

**Goal:** Address A-2 — make the constructor directory structurally equivalent to the descriptor directory, enabling lightweight imports and clean dependency separation.

**Target layout:**

```
constructor/
├── v2/             (module)  ← serialization format (renamed from spec/v1)
├── runtime/        (module)  ← runtime types + library interfaces
├── spec/
│   └── transformation/       ← transformation spec types (ComputeComponentDigest)
├── transformer/              ← transformation implementations
├── internal/graph/           ← graph generation logic
└── go.mod                    ← orchestration module (BuildGraphDefinition, NewDefaultBuilder)
```

Mirrors:
```
descriptor/
├── v2/             (module)  ← serialization format
├── runtime/        (module)  ← runtime types + conversions
├── normalisation/  (module)  ← digest normalization
```

**Changes:**

1. **Rename `constructor/spec/v1` → `constructor/v2`** (new module path: `ocm.software/.../constructor/v2`). The constructor format is based on the v2 descriptor schema; calling it `v2` aligns naming with `descriptor/v2`. The `v2` is the schema version, not the spec version. The existing `constructor/spec/v1` module continues to exist as an alias/redirect during transition.

2. **Extract `constructor/runtime` into its own module** (new module path: `ocm.software/.../constructor/runtime`). Contains:
   - Runtime types: `Component`, `Resource`, `Source`, `AccessOrInput`, `Reference`, `Digest`, `Label`, `CopyPolicy`, etc. (currently in `constructor/runtime/constructor.go`)
   - Conversion functions: `ConvertToV1Component`, `ConvertFromV2`, `ConvertToV2Resource`, etc. (currently in `constructor/runtime/convert_*.go`)
   - Library interfaces: `ResourceInputMethod`, `SourceInputMethod`, result types, `ResourceConsumerIdentityProvider`, `SourceConsumerIdentityProvider`, `ExternalComponentRepositoryProvider` (currently in `constructor/interface.go`)
   - Dependencies: `runtime`, `blob`, `descriptor/runtime`, `constructor/v2`, `repository` — all lightweight

3. **Drop `ResourceDigestProcessor` from `constructor/interface.go`.** Use `repository.ResourceDigestProcessor` everywhere (same interface, already used by the actual transformers).

4. **Constructor root module slims down** to orchestration only: `BuildGraphDefinition`, `NewDefaultBuilder`, options, and the `internal/graph/` package. It imports `constructor/runtime` and `constructor/v2` like any other consumer.

**Dependency impact:**

| Consumer | Before | After |
|----------|--------|-------|
| `input/file`, `input/dir`, `input/utf8` | → full `constructor` module | → `constructor/runtime` (lightweight) |
| `helm/input` | → full `constructor` module | → `constructor/runtime` (lightweight) |
| `plugin/manager` | → full `constructor` module | → `constructor/runtime` (lightweight) |
| `cli` | → full `constructor` module | → `constructor` (orchestration, unchanged) |

---

### Strategy 5: Restore Input Method Implementations

**Goal:** Address A-1 — restore two-layer architecture for input types so library consumers can process input specs without the transformation graph.

**Prerequisite:** Strategy 4 (constructor module restructuring) — the `InputMethod` implementations need to import `ResourceInputMethod` / `SourceInputMethod` from the lightweight `constructor/runtime` module, not the full `constructor` module.

**Approach:**
1. Restore the `InputMethod` types deleted in commit `4f2e8f910` into each input package (`input/file/method.go`, `input/dir/method.go`, `input/utf8/method.go`, `helm/input/method.go`). These implement `constructor.ResourceInputMethod` / `constructor.SourceInputMethod`.
2. Refactor each transformer (`FileInput`, `DirInput`, `UTF8Input`, `HelmInput`) to instantiate the corresponding `InputMethod` and delegate to `ProcessResource()` / `ProcessSource()`, then buffer the returned blob and populate the transformation output.
3. The `ResourceInputMethod` / `SourceInputMethod` interfaces remain in `constructor/interface.go`. The plugin input registry (`plugin/manager/registries/input/`) becomes live again.

**After this change, transformers mirror the access-type pattern:**

```go
// bindings/go/input/file/transformation/file_input.go — transformer delegates to InputMethod
func (t *FileInput) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
    // ... deserialize spec ...
    method := &file.InputMethod{WorkingDirectory: spec.WorkingDirectory}
    result, err := method.ProcessResource(ctx, resource, nil)
    // ... buffer result.ProcessedBlobData to file, populate output ...
}
```

- Library consumer: creates `file.InputMethod{WorkingDirectory: wd}` and calls `ProcessResource()` directly
- Transformation graph: `FileInput` transformer creates `InputMethod` internally and delegates

---

### Strategy 6: Optional Digest Processing in Transfer

**Goal:** Address M-3 for recursive transfers that modify content.

**Approach:**
1. Register `ComputeComponentDigest` in transfer's builder (behind a flag).
2. Add digest computation nodes for referenced components in `fillGraphDefinitionWithPrefetchedComponents`.
3. Wire reference digest propagation into transfer's upload transformation spec.

---

## Prioritized Action Items

### Phase 1: Bug Fixes (Immediate)

| # | Issue | Action | Effort |
|---|-------|--------|--------|
| 1 | C-1 | Fix `source.go:94` to use `${inputID.output.source}` | Small |
| 2 | C-6 | Publish AddLocalSource constants in `ociv1alpha1` | Small |

### Phase 2: Inline Static Data + Shared Utilities (Short-term)

| # | Issue | Action | Effort |
|---|-------|--------|--------|
| 3 | M-1/M-2/C-5 | Stop using environment in generators per Strategy 1 — inline static data, delete all `buildDescriptorSpec` variants, `addXToEnvironment` functions | Large |
| 4 | C-3/C-4 | Create shared `graphutil` package per Strategy 2 | Medium |
| 5 | C-2 | Include `ConvertToConcreteRepo` in shared package | Small (bundled with #4) |

### Phase 3: Module Restructuring + Input Library Layer (Medium-term)

| # | Issue | Action | Effort |
|---|-------|--------|--------|
| 6 | A-2 | Rename `constructor/spec/v1` → `constructor/v2` per Strategy 4 | Small |
| 7 | A-2 | Extract `constructor/runtime` into its own module with library interfaces per Strategy 4 | Medium |
| 8 | A-2 | Drop duplicate `ResourceDigestProcessor` from `constructor/interface.go` | Small |
| 9 | A-1 | Restore `InputMethod` types in `input/file`, `input/dir`, `input/utf8`, `helm/input` per Strategy 5 | Medium |
| 10 | A-1 | Refactor transformers (`FileInput`, `DirInput`, `UTF8Input`, `HelmInput`) to delegate to restored `InputMethod` | Medium |

### Phase 4: Remaining Alignment (Medium-term)

| # | Issue | Action | Effort |
|---|-------|--------|--------|
| 11 | M-4 | Adapt constructor's resolver to wrap `ComponentVersionRepositoryResolver` | Medium |

### Phase 5: Feature Parity (Long-term)

| # | Issue | Action | Effort |
|---|-------|--------|--------|
| 12 | M-3 | Digest processing in transfer (Strategy 6) | Large |
| 13 | TODO | By-value resource processing in constructor (`input.go:119`) | Large |
