# Plan: Extract constructor/v2 into its own module, use in input transformation specs

## Context

Input transformation specs (FileInput, DirInput, UTF8Input, HelmInput) use `*v2.Resource` for their `Resource` field, but `v2.Resource` only has `Access *runtime.Raw` — not `Input`. The constructor's `spec/v1.Resource` has `AccessOrInput` supporting both. We can't import `constructor/v2` from input specs because the constructor **module** depends on the input modules — creating a module-level cycle.

The fix: extract `constructor/v2` into its own Go module. The spec types only depend on `runtime`, `yaml`, and `jsonschema/v6` — making it a leaf module with no cycles.

Additionally, the current spec structure is wrong: input-specific attributes (path, mediaType, etc.) sit as top-level fields alongside a `Resource` field. Instead, the spec should contain a `*constructorv1.Resource` whose `Input` field holds the input-specific attributes. The transformer deserializes `Resource.Input` to get the input config. Only transformer-specific concerns (workingDirectory, outputPath) remain as top-level spec fields.

## Steps

### 1. Create `bindings/go/constructor/v2/go.mod`

New module: `ocm.software/open-component-model/bindings/go/constructor/v2`

Dependencies (matching what constructor.go + validate.go need):
- `ocm.software/open-component-model/bindings/go/runtime`
- `sigs.k8s.io/yaml`
- `github.com/santhosh-tekuri/jsonschema/v6`

### 2. Remove `convert.go` and `convert_test.go` from `spec/v1`

These functions are only called from spec/v1's own tests — no external callers. The same conversions already exist in `constructor/runtime/convert_v1.go`. Removing them keeps the new module's deps minimal (avoids pulling in `descriptor/runtime`).

### 3. Create `bindings/go/constructor/v2/Taskfile.yml`

Standard Taskfile matching the pattern used by other modules (e.g., `descriptor/v2/Taskfile.yml`).

### 4. Register in root `Taskfile.yml`

Add the new module's Taskfile include to the root Taskfile.

### 5. Update `bindings/go/constructor/go.mod`

Add `ocm.software/open-component-model/bindings/go/constructor/v2` as a dependency.

### 6. Restructure input transformation specs

Replace the current pattern where input-specific fields sit alongside a `Resource` field with the new model where `Resource.Input` contains the input-specific attributes.

**Before (FileInputSpec as example):**
```go
type FileInputSpec struct {
    Resource         *v2.Resource `json:"resource,omitempty"`
    Path             string       `json:"path"`
    MediaType        string       `json:"mediaType,omitempty"`
    Compress         bool         `json:"compress,omitempty"`
    WorkingDirectory string       `json:"workingDirectory,omitempty"`
    OutputPath       string       `json:"outputPath,omitempty"`
}
```

**After:**
```go
type FileInputSpec struct {
    Resource         *constructorv1.Resource `json:"resource,omitempty"`
    WorkingDirectory string                  `json:"workingDirectory,omitempty"`
    OutputPath       string                  `json:"outputPath,omitempty"`
}
// Resource.Input contains {"type":"file/v1","path":"./foo.txt","mediaType":"text/plain","compress":false}
```

Apply to all four specs:
- `bindings/go/input/file/transformation/spec/v1alpha1/file_input.go` — FileInputSpec + FileInputOutput
- `bindings/go/input/dir/transformation/spec/v1alpha1/dir_input.go` — DirInputSpec + DirInputOutput
- `bindings/go/input/utf8/transformation/spec/v1alpha1/utf8_input.go` — UTF8InputSpec + UTF8InputOutput
- `bindings/go/helm/input/transformation/spec/v1alpha1/helm_input.go` — HelmInputSpec + HelmInputOutput

Update each module's `go.mod` to add the new `constructor/v2` dependency (and remove `descriptor/v2` if no longer needed).

### 7. Update all four transformers

Each transformer must now extract input-specific config from `spec.Resource.Input` instead of reading top-level spec fields:

**File transformer** (`bindings/go/input/file/transformation/file_input.go`):
- Deserialize `spec.Resource.Input` to get path, mediaType, compress

**Dir transformer** (`bindings/go/input/dir/transformation/dir_input.go`):
- Deserialize `spec.Resource.Input` to get path, mediaType, compress, preserveDir, followSymlinks, excludeFiles, includeFiles, reproducible

**UTF8 transformer** (`bindings/go/input/utf8/transformation/utf8_input.go`):
- Deserialize `spec.Resource.Input` to get text, json, formattedJson, yaml, compress

**Helm transformer** (`bindings/go/helm/input/transformation/helm_input.go`):
- Deserialize `spec.Resource.Input` to get path, repository, helmRepository, version, caCert, caCertFile
- Update `createRemoteResource` return type from `*v2.Resource` to `*constructorv1.Resource`
- Construct `constructorv1.Resource` with `AccessOrInput{Access: &rawAccess}` instead of `v2.Resource{Access: &rawAccess}`

### 8. Update `buildInputTransformation` in `input.go`

`bindings/go/constructor/internal/graph/input.go` — change how the transformation spec is built:

**Before:** resourceMap is injected as `inputMap["resource"]` alongside input-specific fields at the top level.

**After:** Build the spec as `{"resource": <full constructorv1.Resource with input populated>}`. The resource already has the input embedded — just serialize it and wrap with workingDirectory/outputPath.

Do NOT put input into `addResourceMap` — AddLocalResource still uses the `access` placeholder.

### 9. Run `task generate` + `go mod tidy`

Regenerate deepcopy, type registration, JSON schemas. Then `go mod tidy` for affected modules.

### 10. Update tests

Update `graph_test.go` assertions for the new resource shape in input transformation specs.

## Key files

**New:**
- `bindings/go/constructor/v2/go.mod`
- `bindings/go/constructor/v2/Taskfile.yml`

**Removed:**
- `bindings/go/constructor/v2/convert.go`
- `bindings/go/constructor/v2/convert_test.go`

**Modified:**
- `Taskfile.yml` (root) — add new module include
- `bindings/go/constructor/go.mod` — add spec/v1 module dep
- `bindings/go/input/file/transformation/spec/v1alpha1/file_input.go` — restructure spec
- `bindings/go/input/file/transformation/file_input.go` — read from Resource.Input
- `bindings/go/input/file/go.mod` — add spec/v1 dep
- `bindings/go/input/dir/transformation/spec/v1alpha1/dir_input.go` — restructure spec
- `bindings/go/input/dir/transformation/dir_input.go` — read from Resource.Input
- `bindings/go/input/dir/go.mod` — add spec/v1 dep
- `bindings/go/input/utf8/transformation/spec/v1alpha1/utf8_input.go` — restructure spec
- `bindings/go/input/utf8/transformation/utf8_input.go` — read from Resource.Input
- `bindings/go/input/utf8/go.mod` — add spec/v1 dep
- `bindings/go/helm/input/transformation/spec/v1alpha1/helm_input.go` — restructure spec
- `bindings/go/helm/input/transformation/helm_input.go` — read from Resource.Input + update createRemoteResource
- `bindings/go/helm/go.mod` — add spec/v1 dep
- `bindings/go/constructor/internal/graph/input.go` — new serialization
- `bindings/go/constructor/internal/graph/graph_test.go` — updated assertions

## Verification

```bash
task init/go.work
task generate
task tidy
task tools:lint
go test ./bindings/go/constructor/v2/... ./bindings/go/constructor/... ./bindings/go/input/file/... ./bindings/go/input/dir/... ./bindings/go/input/utf8/... ./bindings/go/helm/...
go run ./cli/main.go --working-directory /tmp/helloworld add cv --component-version-conflict-policy replace --dry-run
```
