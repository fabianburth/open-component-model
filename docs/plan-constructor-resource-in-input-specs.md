# Plan: Add constructor spec types to descriptor/v2 module, use in input transformation specs

## Context

Input transformation specs (FileInput, DirInput, UTF8Input, HelmInput) use `*v2.Resource` for their `Resource` field, but `v2.Resource` only has `Access *runtime.Raw` — not `Input`. The constructor's `spec/v1.Resource` has `AccessOrInput` supporting both. We can't import `constructor/spec/v1` from input specs (circular dep: constructor → input/* → can't import constructor). So we create `descriptor/v2/constructor/` that copies the constructor spec types. The `descriptor/v2` module already has the exact same deps (`runtime`, `yaml`, `jsonschema/v6`).

The plugin contracts at `bindings/go/plugin/manager/contracts/input/v1/types.go` already model this pattern: request uses `*constructorv1.Resource` (with input), response uses `*descriptorv2.Resource` (with access). Our input transformation specs should follow the same model.

## Steps

### 1. Create `bindings/go/descriptor/v2/constructor/constructor.go`

Copy types from `bindings/go/constructor/spec/v1/constructor.go` (425 lines). Include everything:
- `ComponentConstructor`, `Component`, `Provider`, `Resource`, `Source`, `Reference`
- `AccessOrInput` (the key type with both `Access` and `Input *runtime.Raw`)
- `ConstructorAttributes`, `CopyPolicy` constants
- `ElementMeta`, `ObjectMeta`, `ComponentMeta`, `Meta`
- `Label`, `SourceRef`, `Digest`, `Signature`, `SignatureInfo`
- `ResourceRelation` constants (`LocalRelation`, `ExternalRelation`)
- All methods (`ToIdentity`, `HasInput`, `HasAccess`, `Validate`, `String`, etc.)
- `AsRawMessage`, `MustAsRawMessage`

Change package declaration from `package v1` to `package constructor`. Import path stays the same (`runtime`, `yaml`).

### 2. Create `bindings/go/descriptor/v2/constructor/validate.go`

Copy from `bindings/go/constructor/spec/v1/validate.go` (93 lines). Includes:
- Embedded schema: `//go:embed resources/schema-2020-12.json`
- `GetJSONSchema` singleton, `Compile()`, `Validate()`, `ValidateRawJSON()`, `ValidateRawYAML()`

### 3. Copy `bindings/go/constructor/spec/v1/resources/schema-2020-12.json`

Copy the 567-line JSON Schema file to `bindings/go/descriptor/v2/constructor/resources/schema-2020-12.json`.

### 4. Update `constructor/spec/v1` to re-export from new package

Change `constructor/spec/v1/constructor.go` to type-alias everything from `descriptor/v2/constructor`:
```go
import constructor "ocm.software/open-component-model/bindings/go/descriptor/v2/constructor"
type Resource = constructor.Resource
type AccessOrInput = constructor.AccessOrInput
// ... etc for all types
```

Keep `convert.go` as-is — it only uses the aliased types, so it'll work.
Keep `validate.go` — delegate to the new package or keep both (the schema is the same).

This preserves backward compatibility for all external consumers:
- `cli/cmd/add/component-version/cmd.go`
- `bindings/go/plugin/manager/contracts/input/v1/types.go`
- `bindings/go/helm/cmd/main_test.go`

### 5. Change input transformation spec `Resource` fields

In all four input spec types, replace `*v2.Resource` with `*constructor.Resource`:

- `bindings/go/input/file/transformation/spec/v1alpha1/file_input.go` — `FileInputSpec.Resource` and `FileInputOutput.Resource`
- `bindings/go/input/dir/transformation/spec/v1alpha1/dir_input.go` — `DirInputSpec.Resource` and `DirInputOutput.Resource`
- `bindings/go/input/utf8/transformation/spec/v1alpha1/utf8_input.go` — `UTF8InputSpec.Resource` and `UTF8InputOutput.Resource`
- `bindings/go/helm/input/transformation/spec/v1alpha1/helm_input.go` — `HelmInputSpec.Resource` and `HelmInputOutput.Resource`

Import: `constructor "ocm.software/open-component-model/bindings/go/descriptor/v2/constructor"`

### 6. Update HelmInput transformer

`bindings/go/helm/input/transformation/helm_input.go:111` — change `createRemoteResource` return type from `*v2.Resource` to `*constructor.Resource`. Update the struct literal to use constructor types (`constructor.ElementMeta`, `constructor.Resource`, etc.) and set `Access` via `AccessOrInput`.

### 7. Update `buildInputTransformation` in `input.go`

`bindings/go/constructor/internal/graph/input.go` — inject the serialized input spec into `resourceMap["input"]`. The `resourceMap` already has name/version/type/relation; now also gets `"input": <serialized input spec>`. This means input transformation specs carry the full resource including its input definition.

Do NOT put input into `addResourceMap` — AddLocalResource still uses the `access` placeholder.

### 8. Run `task generate` + `go mod tidy`

Regenerate deepcopy, type registration, JSON schemas. Then `go mod tidy` for affected modules (descriptor/v2, input/file, input/dir, input/utf8, helm, constructor).

### 9. Update tests

Update `graph_test.go` assertions if needed for the new resource shape in input transformation specs.

## Key files

**New:**
- `bindings/go/descriptor/v2/constructor/constructor.go`
- `bindings/go/descriptor/v2/constructor/validate.go`
- `bindings/go/descriptor/v2/constructor/resources/schema-2020-12.json`

**Modified:**
- `bindings/go/constructor/spec/v1/constructor.go` → re-export via type aliases
- `bindings/go/constructor/spec/v1/validate.go` → delegate to new package
- `bindings/go/input/file/transformation/spec/v1alpha1/file_input.go`
- `bindings/go/input/dir/transformation/spec/v1alpha1/dir_input.go`
- `bindings/go/input/utf8/transformation/spec/v1alpha1/utf8_input.go`
- `bindings/go/helm/input/transformation/spec/v1alpha1/helm_input.go`
- `bindings/go/helm/input/transformation/helm_input.go`
- `bindings/go/constructor/internal/graph/input.go`
- `bindings/go/constructor/internal/graph/graph_test.go`

## Verification

```bash
task generate
# go mod tidy for each affected module
go vet ./bindings/go/descriptor/v2/... ./bindings/go/constructor/... ./bindings/go/input/file/... ./bindings/go/input/dir/... ./bindings/go/input/utf8/... ./bindings/go/helm/...
go test ./bindings/go/descriptor/v2/... ./bindings/go/constructor/... ./bindings/go/input/file/... ./bindings/go/input/dir/... ./bindings/go/input/utf8/... ./bindings/go/helm/...
go run ./cli/main.go --working-directory /tmp/helloworld add cv --component-version-conflict-policy replace --dry-run
```
