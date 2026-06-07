### BEGIN KARPATHY ##
# CLAUDE.md

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

### END KARPATHY ###

# Build Commands
**IMPORTANT**: Use mage for building tptctl, NOT go build
```bash
# Build tptctl binary
mage build:tptctl

# NOT: go build ./...
```

When building container images with `tptdev build`, use `--parallel=2` to limit concurrent builds. This can be increased if your machine has more resources.
```bash
tptdev build --names rest-api,kubernetes-workload-controller --push --parallel=2
```

## Local Verification
Default to these local checks before committing — in order:

1. `go vet ./...` — static check
2. `mage build:tptctl` — CLI compiles
3. `tptdev build --names <changed components> --parallel=2` — verify the container images that actually get deployed compile (no `--push` needed locally)
4. `threeport-sdk gen -c sdk-config.yaml` followed by `git diff --name-only | grep '_gen.go'` — should produce no diff (idempotence). A diff means either you manually edited a `_gen.go` file, or you changed source types in `pkg/api/v0/*.go` / `sdk-config.yaml` without regenerating.

**Do not default to `go test ./...`.** Every test in this repo is integration or e2e and assumes a live control plane, so local runs fail for infrastructure reasons rather than code correctness. Run them only against a real environment (`mage test:integration`, `mage test:e2eLocal`, etc.).

**Do not use `go build ./cmd/<controller>/`.** It leaves untracked binaries in the repo root (e.g. `database-migrator`), and `main_gen.go` is not the actual deployment artifact anyway. `tptdev build` produces the image that gets deployed and is fast on subsequent runs thanks to the Go build cache. If you run `go build` for any reason, delete the binary immediately — they are not gitignored.

## Using threeport-sdk
- **Don't stat or which-check for the `threeport-sdk` binary.** Just run `mage install:sdk` when you need it — the build is a no-op when up to date and cheap when not, and it guarantees the binary matches the current source. Running `ls ~/go/bin/threeport-sdk` or `which threeport-sdk` first is wasted motion (and often triggers a permission prompt for no benefit).

## Prefer mage, with one exception
- **Always use mage** as the entrypoint for tasks that have a mage target — `mage install:sdk`, `mage build:tptctl`, `mage dev:generate`, `mage test:integration`, etc. It's the canonical interface and knows how to wire up dependencies.
- **Exception — source types in flux.** `mage` itself must compile every magefile before running any target, and the magefiles import project packages. If you've changed a source type in `pkg/api/v0/*.go` or removed an object from `sdk-config.yaml` without regenerating yet, `mage` will fail with `undefined: …` errors before it can even start. In that narrow window, call the already-installed underlying binary directly: `threeport-sdk gen -c sdk-config.yaml`. Once regeneration is complete and the tree compiles again, go back to using `mage`.

# Generated Files - CRITICAL

**NEVER manually edit files ending in `_gen.go`**. These are regenerated by `threeport-sdk gen` and any manual edits will be silently overwritten.

If you need to change behavior in generated code:
1. Find the corresponding generator in `pkg/sdk/v0/gen/` (e.g., handler generation is in `pkg/sdk/v0/gen/pkg/api-server/handlers.go`)
2. Modify the generator code (jennifer/jen Go code generation)
3. Rebuild SDK: `mage install:sdk`
4. Regenerate: `threeport-sdk gen -c sdk-config.yaml`
5. Verify the generated output reflects your changes

Generated file locations (DO NOT manually edit):
- `pkg/api/v0/*_gen.go`, `pkg/api/v0/table_name_gen.go`
- `pkg/api-server/v0/handlers/*_gen.go`, `routes/*_gen.go`, `versions/*_gen.go`
- `pkg/api-server/v0/module_gen.go`, `tagged_fields_gen.go`
- `pkg/client/v0/*_gen.go`, `delete_object_gen.go`
- `magefiles/magefile_gen.go`, `internal/*/..._gen.go`
- `cmd/*/main_gen.go`

# Command Readability

Before running a `kubectl exec` command (especially piped one-liners), always describe in plain text what the command does. These commands are often long and don't fit on the user's terminal, so the description ensures the user understands what is being executed.

# Infrastructure Cost Awareness

Always be mindful of cloud resource state being managed. Threeport is, at its core, infrastructure management — excess or forgotten resources cost real money in both development and production. Clean up resources promptly after testing. Don't leave clusters, VPCs, or other billable resources running unnecessarily.

# API-First Understanding

Before digging into code to understand the API, refer to the Swagger API spec first to get a full picture of available endpoints, request/response shapes, and object relationships. The spec is the authoritative reference for API behavior.

# PR Description Style

Flat prose, no headers, no checkboxes. For comment-only or docs-only PRs, one or two sentences is enough. Lead with what changed and why; skip the test-plan boilerplate unless the PR genuinely needs verification steps. Match the style the user already writes in: terse, no marketing tone, no AI flourishes.

Do not hard-wrap lines inside paragraphs. Write each paragraph as one logical line and let the browser reflow it on render. Hard-wrapped 70-char lines render as a narrow column in GitHub's editor and preview panes even though markdown collapses the breaks in the final view. Use newlines only between paragraphs and inside fenced code blocks.

# Branch Names

Branch names use conventional-commits prefixes mirroring the commit-subject types: `feat-`, `fix-`, `refactor-`, `docs-`, `chore-`. Prefix is followed by a hyphen, then a kebab-case description. The description follows the same rules as commit subjects (lowercase, plain English, no CamelCase type names, no stage markers).

Format: `<type>-<kebab-description>`.

Examples: `feat-aor-delete-guards`, `fix-control-plane-only-cluster-name`, `refactor-extract-encrypt-hooks-to-api-lib`, `docs-add-boilerplate-scaffolding-markers`.

Avoid: non-conventional prefixes like `add-`, `improve-`, or descriptive-only names with no prefix at all.

# Threeport Code Conventions

## Reading Before Writing

Before writing comments, docstrings, or new code in an existing package, grep that package for analogous code and match the local pattern. The rules below state defaults; the package is the authoritative source for the *shape* of code. If the package convention conflicts with a rule below, surface the conflict before changing direction.

## Moving Logic Means Moving It

When asked to "move" or "extract" logic, relocate it verbatim. Preserve inline comments, local variable names, loop structure, and multi-line forms — even if the result feels awkward in its new home or you'd write it differently from scratch. Refactoring during a move conflates two changes and makes review much harder. If the moved code obviously needs cleanup, surface it as a follow-up; don't bundle it with the move.

## Return Validation Errors Early

In validation functions, reject and return as soon as a precondition fails. Each check should be a flat top-level guard: validate, return on failure, fall through to the next. Code below the check can then assume the error case has been handled, which makes the function easy to extend — append a new check at the bottom and trust everything above ran cleanly.

The inverted shape — wrapping the happy path inside nested `if`/`else` branches that depend on prior conditions — makes the function fragile. Logic added later can land in a branch that's never reached, with no compile-time signal that anything is wrong.

## Function Naming
- Use PascalCase for exported functions: `ThreeportWorkloadName`, `GetConnection`, `CreateOCIUserAndCredentials`
- Use camelCase for private/unexported functions: `createOCICompartment`, `validateThreeportState`, `getAvailabilityDomainName`
- Function names should be descriptive and use complete words rather than abbreviations
- **CRITICAL**: Use "verb+action" naming pattern for all functions:
  - `GetClusterOCID`, `CreateOCIUser`, `ValidateOCIUserPropagation`, `DeployComplete`
  - NOT: `ClusterOCID`, `OCIUser`, `UserPropagation`
  - Start with action verbs: `Get`, `Create`, `Validate`, `Deploy`, `Delete`, `Generate`, `Write`, etc.

## Spell Out Identifiers
- **Default to spelling words out fully** in identifiers (types, fields, vars, params, function names, constants). Short ad-hoc abbreviations like `fk`, `fks`, `cfg`, `req` are hard to read at a glance and inconsistent across files.
  - Use `foreignKey` not `fk`; `foreignKeys` not `fks`; `RelationshipForeignKey` not `RelationshipFK`.
  - Use `config` not `cfg`; `request` not `req`; `response` not `res`/`resp`.
- **Same rule for filenames.** Prefer `relationship_foreign_keys.go` over `relationship_fks.go`.
- **In comments and prose**, write "foreign key" rather than "FK".
- Established domain abbreviations are fine (`ID`, `URL`, `OCI`, `AWS`, `GCP`, `OCID`, `JSON`, `YAML`, `HTTP`, `TLS`, `DNS`, `K8s`/`Kubernetes`). When in doubt, spell it out.
- Loop and very-short-scope variables (`i`, `j`, `err`, `ok`) are fine as-is — the rule is about *abbreviations of domain words*, not standard Go idioms.

## Import Aliases
- **Alias by the package's conceptual name, not its version.** Versioned threeport packages all declare `package v0`, so importing them without an alias would name them all `v0` and clash. Pick the meaningful suffix:
  - `pkg/api/v0` → alias `api`
  - `pkg/client/v0` → alias `client`
  - `pkg/encryption/v0` → alias `encryption`
  - `pkg/sdk/v0` → alias `sdk`
  - `pkg/util/v0` → alias `util`
- **Avoid `v0`, `v01`, `api_v0`, `client_v0`** as aliases. `v0` says nothing about which package; `api_v0` is redundantly versioned in a file that already implies v0.
- **Use the longer form (e.g. `api_v0`) only when the compiler requires it** — i.e. two distinct versioned packages with the same conceptual name needing to coexist in the same file. A file in `package api` importing `pkg/api/v0` as `api` is fine; Go allows the alias to shadow the local package name without symbol collision.
- **Function parameter wins over import alias when names collide.** If a function parameter naturally takes the package's conceptual name (e.g. `func F(gen *gen.Generator)`), keep the parameter on its natural name and give the import a longer alias (`sdkgen "github.com/.../sdk/v0/gen"`) so package-level calls inside the body remain reachable. This matters when the body calls a package function (e.g. `sdkgen.ParseRelationshipTagValue(rel)`) — otherwise the parameter shadows the package inside the function body and the package call fails to compile.

## Function Documentation (Docstrings)
- ALL functions (both exported and unexported) require documentation comments
- Comments must begin with the function name followed by a verb describing what it does
- Use present tense verbs: "creates", "returns", "validates", "performs"
- Format: `// FunctionName verbs what the function does.`

### Examples of correct docstring format:
```go
// ThreeportWorkloadName returns a standardized name for a ThreeportWorkload
// Kubernetes custom resource based on the kubernetes workload instance ID.
func ThreeportWorkloadName(...)

// MergeHelmValuesGo merges two helm values documents and
// returns the result as a map[string]interface{}.
func MergeHelmValuesGo(...)

// createOCICompartment creates a new compartment for the threeport instance.
func (b *OCIBootstrapSDK) createOCICompartment(...)

// getAvailabilityDomainName returns the full name of the first availability domain in the region.
func (i *KubernetesRuntimeInfraOKE) getAvailabilityDomainName() (string, error)

// CreateOCIUserAndCredentials creates user, groups, policies, and API key using OCI SDK directly.
func (b *OCIBootstrapSDK) CreateOCIUserAndCredentials() error

// ValidateOCIUserPropagation validates that the service user credentials are propagated across all OCI services.
func (b *OCIBootstrapSDK) ValidateOCIUserPropagation() error
```

## Key Patterns:
- Exported functions get detailed multi-line descriptions when complex
- Unexported functions get concise single-line descriptions
- Always start with function name + verb
- End with period
- For functions that span multiple lines in description, maintain consistent formatting

## Docstring Scope (What NOT to Include)
- Docstrings describe what the function does, not what it *used* to do, what it *replaces*, or what other system now handles its former job. Refactor narrative belongs in the commit message.
- Never name other functions, tables, fields, files, or systems by identifier in a docstring. Identifiers rename and move; the docstring rots silently.
- For stub/no-op functions (`return nil` placeholders, hooks reserved for later), the one-line docstring per the conventions above is sufficient. Do not add a paragraph explaining the absence.

## Comment Line Balance
- When a `//` comment wraps across multiple lines, avoid leaving the last line significantly shorter than the others. A four-line comment ending in just `row.` or `tags via reflection.` reads as an awkward straggler. Pack content so the last line is at least roughly comparable in width to the others, or use fewer lines.
- This applies to docstrings and inline comment blocks alike. Tighten the prose to fit, rather than letting a trailing fragment dangle.

## Logging Conventions

### Structured Logging (Controllers/Reconcilers)
- Use `log.Info()`, `log.Error()`, `log.V(1).Info()` for internal operations
- Include contextual fields: `log.Error(err, "failed to create resource", "resourceID", id)`
- Use verbosity levels: `log.V(1).Info()` for debug-level information

### Console Output (User-facing Operations)
- **NO EMOJIS** - Use plain text only
- Use `fmt.Printf()` for standard output operations
- Use `fmt.Fprintf(os.Stderr, ...)` for errors
- **Message formats:**
  - Success: `fmt.Printf("Successfully created %s\n", name)`
  - Info/Status: `fmt.Printf("Using existing %s: %s\n", type, name)`
  - Progress: `fmt.Printf("Creating %s...\n", resource)`
  - Errors: `fmt.Fprintf(os.Stderr, "Error: %s\n", err)`
  - Usage: `fmt.Fprintf(os.Stderr, "Usage: %s <args>\n", program)`

### CLI Output Functions (pkg/cli/v0/output.go)
Use the standardized functions when available:
- `cli.Info("message")` → "Info: message"
- `cli.Error("message", err)` → "Error: message" (in red)
- `cli.Warning("message")` → "Warning: message" (in yellow)
- `cli.Complete("message")` → "Complete: message" (in green)

### Formatting Standards:
- **Always end with newline**: `\n`
- **Simple descriptive text**: "Creating compartment", "Successfully authenticated"
- **Include relevant details**: resource names, IDs, accounts when helpful
- **Consistent capitalization**: Start messages with capital letters

## Error Message Conventions

### Error Wrapping and Formatting
- **Use `%w` for error wrapping**: `fmt.Errorf("failed to create resource: %w", err)`
- **NOT `%v`**: Avoid `fmt.Errorf("failed to create resource: %v", err)`
- **Error messages start lowercase**: "failed to create", not "Failed to create"
- **Use consistent patterns**: "failed to [action]" for most error messages

### Error Types
- **Wrapped errors**: `fmt.Errorf("failed to get user: %w", err)`
- **Simple errors**: `errors.New("user must be attached to kubernetes workload instance")`
- **Context-specific errors**: Include relevant details when helpful

### Examples of correct error patterns:
```go
// Error wrapping (preferred)
return fmt.Errorf("failed to create compartment: %w", err)
return fmt.Errorf("failed to get tenancy OCID: %w", err)

// Simple errors
return errors.New("secret instance must be attached to a kubernetes workload instance")
return errors.New("deletion notification received but not scheduled")

// With context
return fmt.Errorf("no active cluster found with name %s", clusterName)
```

### Common Error Message Patterns:
- `"failed to create X"`
- `"failed to get X"`
- `"failed to update X"`
- `"failed to delete X"`
- `"X not found"`
- `"X must be Y"`

## Code Formatting Conventions

### Inline Error Handling
- **Use `if ...; err != nil` whenever possible.** Inline the call in the `if`, check the error in the block, and keep the happy path after the `if` — not nested in an `else` branch. When the call returns a value that needs to survive past the `if`, pre-declare the variable with `var` and use `=` in the `if` statement.

```go
// error-only check
if err := b.createOCICompartment(client); err != nil {
    return fmt.Errorf("failed to create compartment: %w", err)
}

// value + error, value needed after the if: pre-declare, then inline with =
var refs *[]v0.AttachedObjectReference
if refs, err = client.GetAttachedObjectReferencesByAttachedObjectID(
    r.APIClient,
    r.APIServer,
    id,
); err != nil {
    log.Error(err, "failed to get attached object references")
    continue
}
for _, ref := range *refs {
    // ...
}
```

Avoid the `val, err := f(); if err != nil { ... }` two-line form and avoid wrapping the happy path in an `else` branch — both are less consistent with the rest of the codebase.

### Function Parameters and Multi-line Formatting
- **Break parameters into multiple lines** when function calls have >3 parameters or are very long
- **Align parameters vertically** for readability
- **Apply same rules to function definitions**

```go
// Multi-line function call (>3 parameters or long line)
if err := client.EnsureAttachedObjectReferenceExists(
    c.r.APIClient,
    c.r.APIServer,
    c.workloadInstanceType,
    c.workloadInstanceId,
    util.TypeName(*c.secretInstance),
    c.secretInstance.ID,
); err != nil {
    return fmt.Errorf("failed to ensure reference exists: %w", err)
}

// Multi-line function definition
func NewOCIInfrastructureRefactored(
    runtimeInstanceName,
    version,
    tenancyOCID,
    targetRegion,
    workerNodeShape string,
    workerNodeInitialCount int32,
    bootstrap *OCIBootstrapSDK,
) *OCIInfrastructureRefactored {
    // implementation
}
```

### Formatting Rules:
- **Indent parameters**: Use tabs for indentation
- **Closing parenthesis**: Place on same line as last parameter followed by function call continuation
- **Comma placement**: After each parameter except the last
- **Consistent alignment**: Parameters should align vertically

### Struct Literal Field Alignment
- **Do not hand-align struct literal fields** — always rely on `gofmt` (or the editor's format-on-save, which runs gofmt)
- `gofmt` aligns struct literal fields per-literal based on the longest field name in that specific literal, using tabs
- Different struct literals in the same file will have different alignment depending on their longest field — this is correct behavior, don't try to make them all match
- After editing struct literals, run `gofmt -w <file>` if you can't rely on the editor
- Manual alignment gets overwritten on save anyway, so spending effort on it is wasted time

```go
// CORRECT (gofmt-aligned): MachineRuntimeDefinition is the longest field in this literal
machineRuntimeInstance := MachineRuntimeInstanceValues{
    Name:                     instance.Name,
    Hostname:                 instance.Hostname,
    MachineRuntimeDefinition: def,
}

// CORRECT (gofmt-aligned): SSHPassword is the longest field in this literal
sshConfig := MachineRuntimeInstanceValues{
    Name:        instance.Name,
    Hostname:    instance.Hostname,
    SSHPassword: instance.SSHPassword,
}
```

### API Struct Tag Convention

Enforced at codegen time by `pkg/sdk/v0/gen/generator.go::ValidateTags`. Every field on an api/v0 type carrying a `validate:` tag must follow:

- **json**: `json:",omitempty"` on every `validate:"required"`, `validate:"optional"`, and `validate:"optional,association"` field. The field-name part is dropped (Go's `encoding/json` defaults to the Go field name). The `omitempty` is non-negotiable: without it, nil-pointer required fields serialize as JSON null on partial PATCH bodies, and the `PayloadCheck()` null-on-required guard rejects the request.
- **yaml**: drop. threeport uses `sigs.k8s.io/yaml` for all YAML handling (tptctl config files, CLI output, SDK config), which routes YAML through JSON via `encoding/json` with case-insensitive field matching. Yaml struct tags are functionally vestigial — the JSON tag determines the wire format. The historical concern about `yaml`'s library lowercasing field names by default applies only to `gopkg.in/yaml.v2`/`v3` used directly, which the codebase doesn't. Don't add yaml tags to new types; drop them when touching existing types.
- **query**: forbidden. The `QueryBinder` in `pkg/api-server/lib/v0/binder.go` derives keys from `strings.ToLower(field.Name)`. An explicit query tag is noise at best and a silent rename hazard at worst.

Tag order convention (hand-written types): `json` -> `validate` -> `gorm` -> `encrypt` -> `relationship` -> `persist`. Example:

```go
KubernetesWorkloadDefinitionID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`
```

Rationale: `json:",omitempty"` and `validate:"required|optional"` are the strongest semantic pair (they together drive the `PayloadCheck()` null-on-required guard), so keeping them adjacent makes the contract scannable at a glance.

jen's `.Tag(map)` sorts alphabetically (gorm before json), so generated code does not naturally land in convention order. `pkg/sdk/v0/create/api.go` uses a custom ordered-tag helper (`util.Tag`) to emit the convention order at scaffold time.

Why per-field rather than a global toggle: Go's `encoding/json` doesn't support "omitempty by default" as a marshaler option. The experimental `encoding/json/v2` proposal ([#71497](https://github.com/golang/go/issues/71497), in Go 1.25) redefines what "empty" means but still requires per-field tagging.

### Nil Checks on API Type Pointer Fields
- **Do not add defensive nil checks for fields that are guaranteed non-nil by GORM constraints** — if a field has `gorm:"not null"` and `validate:"required"`, it cannot be nil when read from the database
- Reconcilers and other code that receives API objects from the database or notification payloads can dereference these fields directly without a nil guard
- Only check for nil on fields that are actually nullable — i.e., those with `validate:"optional"` and no `gorm:"not null"` constraint
- The distinction is load-bearing: defensive nil checks on non-nullable fields add noise, suggest to readers that the field might legitimately be missing (it can't), and hide real bugs if the field does somehow end up nil (the reconciler silently skips instead of failing loudly)

```go
// MachineRuntimeInstance field definitions:
//   Hostname    *string `gorm:"not null" validate:"required"`  // never nil
//   SSHKey      *string `validate:"optional" encrypt:"true"`    // nullable
//   SSHPassword *string `validate:"optional" encrypt:"true"`    // nullable

// CORRECT: direct dereference on non-nullable field, nil check only on nullable ones
addr := fmt.Sprintf("%s:22", *mri.Hostname)
if mri.SSHKey != nil {
    decryptedKey, err := encryption.Decrypt(key, *mri.SSHKey)
    // ...
}

// WRONG: defensive nil check on non-nullable field
if mri.Hostname == nil {
    return fmt.Errorf("hostname is nil") // can never happen - wastes a check and misleads readers
}
```

### Use util.Ptr for Inline Pointer Values
- **When constructing a struct with pointer fields**, prefer `util.Ptr(...)` over declaring a local variable just to take its address
- The helper lives at `pkg/util/v0/ptr.go`: `func Ptr[T any](input T) *T { return &input }`
- This applies especially to event recording, API object construction, and any struct literal where the value is only referenced once
- For values used multiple times (e.g., a shared timestamp written to several events), a local variable is still appropriate — `util.Ptr` is for one-shot inline use

```go
// WRONG: verbose local variables just to take addresses
eventType := "Warning"
eventReason := "SSHConnectFailed"
eventMessage := fmt.Sprintf("failed to connect: %s", err)
timestamp := time.Now()
event := v0.WorkloadEvent{
    Type:      &eventType,
    Reason:    &eventReason,
    Message:   &eventMessage,
    Timestamp: &timestamp,
}

// CORRECT: inline with util.Ptr
event := v0.WorkloadEvent{
    Type:      util.Ptr("Warning"),
    Reason:    util.Ptr("SSHConnectFailed"),
    Message:   util.Ptr(fmt.Sprintf("failed to connect: %s", err)),
    Timestamp: util.Ptr(time.Now()),
}
```

### Variable Naming and Error-Return Shapes

Code should read as if every contributor shares the same naming sense.  The following rules fall out of patterns already in use across the codebase (see `pkg/client/v0/attached_object.go` for a representative example):

- **Loop variables use the singular of the collection's name in full**.  Prefer `for _, attachedObjectReference := range *attachedObjectReferences` over `for _, r := range refs`.  Single letters are only for indices (`i`, `j`), the error var (`err`), or trivially-scoped helpers inside a handful of lines.
- **Local variables mirror the API type's field/identifier names**, not ad-hoc shorthands.  Prefer `attachedObjectType` / `attachedObjectID` over `t` / `id`.  Short names are for structurally-anonymous values (iterator counters, buffers) — never for named domain concepts.
- **Inline one-shot struct literals at the call site.** When a struct is constructed and immediately passed into another function, pass the literal directly instead of assigning it to a named local first. A local like `ref := &v0.Foo{...}; Create(..., ref)` adds a line without adding meaning. Use a named local only when the value is referenced more than once or the name clarifies intent the struct type doesn't.
- **Return shape**: helpers return a single `error` and, optionally, a value.  Avoid `(error, error)` return signatures — split the concerns into separate functions.  Example: a finder returns `(results, total, error)`; a formatter returns `error`; the caller decides which kind of response to produce.
- **Un-wrapped errors use `errors.New(msg)`**, not `fmt.Errorf("%s", msg)`.  The format-verb pattern mimics wrapping without doing it and creates confusion during code review.

### File Naming in Library Packages

For shared library packages like `pkg/api-server/lib/v0/`, files are named by **capability** rather than implementation (see the existing `response.go`, `validator.go`, `pagination.go`, `context.go`).  A short noun describing the concern is preferred over a longer acronym-heavy name — e.g. `blocking_references.go` over `attached_object_reference_blocking_guard.go`.

### Struct-Field Comments

- **Skip self-evident comments on struct fields.** If the field name and type already say what the value is, don't add a comment. Only annotate when a reader can't infer the contract from the signature: nullability semantics, units, invariants, or a non-obvious default.
- **Don't reference other code files or consumers inside struct-field comments.** A comment like `// drives emission in reconciler_gen.go` rots as soon as that file moves or another consumer shows up. Field comments describe the field itself, not who reads it.

### Ensure vs Upsert Terminology

Threeport uses declarative `Ensure*` naming for client-side create-or-noop helpers: `EnsureAttachedObjectReferenceExists()`, `EnsureAttachedObjectReferenceRemoved()`, etc. Comments and prose about these operations should match: write "ensure the reference exists" rather than "upsert the reference". "Upsert" is a DB-layer verb; it's fine when specifically describing a SQL operation, but the function-level intent in this codebase is "ensure", and the two terms shouldn't be mixed in the same area of code.

## Inline Comment Conventions

### Step Comments Within Functions
- **Start with lowercase**: All inline comments describing steps within functions must start with lowercase letters
- **Use action verbs**: Start with verbs like "create", "set", "get", "delete", "configure", "validate", etc.
- **Be concise**: Keep comments brief and focused on the action being performed
- **Maintain indentation**: Follow the same indentation level as the code they describe
- **Describe what, not why-we-discussed-it**: Comments should state what the code does, not replay the reasoning or conversation that led to the implementation. If context is needed, keep it to one short clause — don't write paragraphs explaining trade-offs or alternative approaches in inline comments

### Examples of correct inline comment format:
```go
func (i *KubernetesRuntimeInfraOKE) CreateOCIResources() error {
    // set up OCI client using existing config provider
    configProvider := i.ConfigProvider

    // get tenancy OCID from the config
    tenancyOCID, err := configProvider.TenancyOCID()

    // create compartment for this threeport instance
    if err := i.createOCICompartment(client); err != nil {
        return fmt.Errorf("failed to create compartment: %w", err)
    }

    // delete all API keys for the user first
    for _, apiKey := range keysResponse.Items {
        // delete the API key
        _, err = client.DeleteApiKey(context.Background(), deleteKeyRequest)
    }
}
```

### Examples of INCORRECT inline comment format:
```go
// WRONG: Starting with uppercase
// Create compartment for this threeport instance

// WRONG: Too verbose
// This function creates a new compartment in OCI for the threeport instance

// WRONG: Missing action verb
// compartment for this threeport instance
```

### Comment Patterns:
- `// create X`
- `// delete X`
- `// get X from Y`
- `// set up X`
- `// configure X for Y`
- `// validate X`
- `// list X to find Y`
- `// update X with Y`

### Indentation Rules:
- Comments should be indented to the same level as the code they describe
- Use tabs for indentation consistency
- Place comments immediately before the code block they describe

## String Consistency and Shared Constants

### CRITICAL: Avoid String Duplication in Related Operations
When multiple functions or methods depend on the same string value (especially for resource names, identifiers, or configuration keys), **ALWAYS** extract the string into a shared constant or variable to prevent inconsistencies and bugs.

### Common Problem Patterns:
```go
// WRONG: Duplicate strings - prone to bugs
func createUser() {
    userName := fmt.Sprintf("threeport-service-%s", instanceName)
    // create user logic
}

func deleteUser() {
    userName := fmt.Sprintf("threeport-user-%s", instanceName)  // BUG: Different format!
    // delete user logic - will fail to find user
}
```

### Correct Solution: Shared Constants
```go
// CORRECT: Shared constants ensure consistency
const (
    serviceUserNameFormat = "threeport-service-%s"
    compartmentNameFormat = "threeport-%s"
    groupNameFormat       = "threeport-bootstrap-%s"
    policyNameFormat      = "threeport-bootstrap-policy-%s"
)

func createUser() {
    userName := fmt.Sprintf(serviceUserNameFormat, instanceName)
    // create user logic
}

func deleteUser() {
    userName := fmt.Sprintf(serviceUserNameFormat, instanceName)  // Always consistent!
    // delete user logic
}
```

### When to Use Shared Constants:
- **Resource naming**: Database names, cloud resource names, API endpoints
- **Configuration keys**: Environment variables, config file keys
- **Create/Delete pairs**: Any operation that creates and later deletes the same resource
- **Validation patterns**: Error messages, validation rules
- **Multiple references**: Any string used in 2+ places

### Benefits:
- **Prevents bugs**: Impossible to have mismatched strings between related operations
- **Single source of truth**: Changes only need to be made in one place
- **Easier refactoring**: Rename patterns across entire codebase safely
- **Better maintainability**: Clear documentation of all string patterns in use

### Examples from Real Issues:
```go
// This caused a critical bug where group deletion failed:
// Create: "threeport-bootstrap-test"
// Delete: "threeport-group-test"  // WRONG! Resource not found

// Fixed with constants:
const bootstrapGroupNameFormat = "threeport-bootstrap-%s"
```

**Rule**: If you find yourself typing the same string pattern twice, immediately extract it into a constant.

# Encrypted Field Handling in Config Package

Any API type in `pkg/api/v0/` with one or more `encrypt:"true"` fields requires matching treatment in the config abstraction and the tptctl command — **do not skip this for new types**. `pkg/config/v0/aws_account.go` is the canonical reference.

## The pattern

1. **Config struct `Get` takes an `encryptionKey string` parameter.** Inside the per-object loop, call one of:
   - Empty key → `encryption.RedactEncryptedValues(&obj)` so `tptctl get` output shows `"[encrypted value redacted]"` instead of ciphertext.
   - Non-empty key → `encryption.DecryptValues(&obj, encryptionKey)` to return plaintext.

2. **Composite `Config` wrappers thread the key through.** If the type has a composite abstraction (e.g. `MachineRuntimeConfig` wrapping definition + instance), its `Get`/`GetOperations` must accept `encryptionKey` and forward it to the inner `.Get()` calls. Create/Replace/Delete pass `""`.

3. **tptctl `get` command wires a `-d/--decrypt-secrets` flag.** Mirror `cmd/tptctl/cmd/aws.go`:
   ```go
   var encryptionKey string
   if <type>Decrypt {
       threeportConfig, _, err := cli.GetThreeportConfig(cliArgs.ControlPlaneName)
       // ...
       key, err := threeportConfig.GetThreeportEncryptionKey(requestedControlPlane)
       // ...
       encryptionKey = key
   }
   // ...
   result, err := config.Get(apiClient, apiEndpoint, encryptionKey)
   ```
   Register the flag on every Get command for the type:
   ```go
   GetFooCmd.Flags().BoolVarP(&fooDecrypt, "decrypt-secrets", "d", false, "Decrypt any encrypted secrets in output.")
   ```

## Why

Without this, `tptctl get` for those objects returns raw AES-GCM ciphertext — unusable and potentially confusing. Redacting by default keeps output readable; the `-d` flag opts into plaintext when the user wants to see credentials (e.g. during debugging, or exporting a config for another control plane).

## Checklist when adding an `encrypt:"true"` field

- [ ] API type field tagged `encrypt:"true"` (and, for nullable secrets, also `validate:"optional"`)
- [ ] `pkg/config/v0/<type>.go` `Get` takes `encryptionKey` + calls Redact/Decrypt
- [ ] Composite wrapper (if any) threads the key through
- [ ] `cmd/tptctl/cmd/<type>.go` Get commands have the decrypt block + `-d` flag

# Event Recording in Controllers

Events are user-facing signals about an API object — what `tptctl get events --for <kind>/<name>` returns. Controllers record them via `r.EventsRecorder.RecordEvent`. The generated reconciler already emits success/failure events for each reconcile op (Create/Update/Delete); hand-written reconcile code only needs to record events for things the generated layer can't see.

## API

```go
r.EventsRecorder.RecordEvent(
    &v0.Event{
        Type:   util.Ptr(event.TypeWarning), // or event.TypeNormal
        Reason: util.Ptr("ScriptTimedOut"),
        Note:   util.Ptr(fmt.Sprintf("create script timed out after %ds", timeout)),
    },
    *obj.ID,
    "v0",
    util.TypeName(v0.<ObjectType>{}),
)
```

The recorder writes both the `Event` row and the `AttachedObjectReference` that links it to the parent object; dedup-by-`(reason, note, type, objectid)` is handled server-side and increments `Count` on repeat.

## What belongs in an event

Surface things a user watching the object would want to know that **aren't already on the object's Status field**:

- **External-dependency outcomes** — SSH connection succeeded/failed, remote host unreachable, cluster not yet ready. These explain *why* reconciliation is stuck or recovered.
- **Operation outcomes with specific detail** — `ScriptSucceeded` / `ScriptFailed` with exit code, timeout, or a truncated stdout/stderr snippet. The generated layer only says "reconcile failed"; the event explains what failed.
- **One-time state transitions** — first successful reach, host key captured on first connect, credentials propagated. Useful milestones the user couldn't infer from a steady-state status.
- **Retryable errors the user should see** — e.g. "will retry after 30s because X". Helps operators distinguish "stuck but recovering" from "actually broken".

## What does NOT belong in an event

- **Heartbeat / per-tick noise** — "checked status, still healthy". Dedup helps, but don't emit these at all.
- **Anything derivable from Status** — if `obj.Status == "Healthy"` conveys the same info, the event is redundant.
- **Debug/trace** — use `log.V(1).Info(...)` for developer-level detail.
- **Implementation internals** — "acquired lock", "parsed config", "marshaled request". Users don't care.
- **Free-form prose reasons** — `Reason` is a dedup key. Use short, stable CamelCase identifiers (e.g. `SSHConnectFailed`, `ScriptTimedOut`, `HostKeyCaptured`), not sentences. Put the details in `Note`.

## Conventions

- **Type**: `event.TypeNormal` for success/informational, `event.TypeWarning` for failure/degraded.
- **Reason**: short, stable CamelCase. Treat it like a machine-readable identifier.
- **Note**: human-readable detail, free-form. Truncate large content (e.g. script stdout) with a clear marker — rows with multi-KB notes are fine but unbounded sizes are not.
- **Don't set `Timestamp`, `EventTime`, `LastObservedTime`, `Count`, or `ReportingController`** — the recorder handles those.

# Dependency Version Management

## CRITICAL: Always Check for Latest Versions
- **NEVER assume first search result is latest** - Always verify you have the most recent version
- **ALWAYS search web for latest version** of every new dependency before adding it
- **CHECK GITHUB RELEASES**: Always check the actual GitHub releases page for the official latest version
- **UPDATE EXISTING DEPENDENCIES**: When working with libraries, check if existing ones need updates too
- **SEARCH PATTERN**: Use queries like "library-name latest version 2024 2025 github releases"

### Examples:
```bash
# WRONG: Just adding first version found
go mod add github.com/some/library v1.2.0

# RIGHT: After web searching for latest
go mod add github.com/some/library v2.1.5  # (after confirming this is latest)
```

### Version Check Workflow:
1. **Web search**: "[library] latest version 2024 2025 github releases"
2. **Visit GitHub releases**: Check actual releases page for latest tag
3. **Verify compatibility**: Check if major version changes require code updates
4. **Update go.mod**: Use the confirmed latest version
5. **Run `go mod tidy`**: Let Go update transitive dependencies

## DEBUGGING RULE: Always Check Dependencies First

**CRITICAL**: When debugging any issue involving external dependencies, ALWAYS proactively check for version updates FIRST before diving into code analysis.

### When to Check Dependency Versions:
- **Any compilation error** involving external libraries
- **Runtime errors** from external dependencies (Pulumi, OCI SDK, etc.)
- **Nil pointer errors** or marshaling issues with providers
- **Compatibility issues** between CLI tools and SDKs
- **"Missing field" or "unknown field" errors**
- **Any provider-related errors** (AWS, OCI, GCP, etc.)

### Debugging Version Check Process:
1. **Immediately check current versions**: `grep -E "(dependency)" go.mod`
2. **Search for latest versions**: Check GitHub releases for each external dependency
3. **Show version comparison**: Display "Current: vX.Y.Z → Latest: vA.B.C"
4. **Recommend updates**: Suggest updating to latest compatible versions
5. **Update and test**: Apply updates before continuing debugging

### Example Response Pattern:
```
Current dependency versions:
- Pulumi CLI: v3.193.0 → Latest: v3.198.0 (5 versions behind)
- Pulumi SDK: v3.190.0 → Latest: v3.198.0 (8 versions behind)
- OCI Provider: v3.9.0 → Latest: v3.9.0 (up to date)

Recommendation: Update Pulumi CLI and SDK first - version mismatches often cause marshaling errors.
```

**Why This Matters**: Many debugging issues (especially marshaling errors, nil pointers, and compatibility problems) are caused by version mismatches between external dependencies. Always rule out version issues before deep-diving into code analysis.

# OCI Development

## Free Tier Testing Strategy
- OKE control plane creation is free; only worker nodes incur cost
- For workload cluster testing, set `workerInitialNodeCount` to 0
- Genesis control planes can be left running (free tier)

## Teardown Order (CRITICAL)
- Always destroy ALL workload clusters BEFORE genesis — even with 0 worker nodes
- 0 nodes ≠ no infrastructure (OKE cluster, VCN, subnets, gateways still exist)
- Workload cluster resources are managed by genesis controllers — once gone, orphaned
- Correct order: workload clusters → genesis
- NEVER delete DB records for clusters with real cloud infra

## tptdev and tptctl Flags
- Always push container images to remote registry — never assume local images are sufficient
- Always use `-n <name>` for `tptdev up/down/debug`
- Use `-t <branch>` for image tags in worktrees (auto-detection breaks)
- Temporarily reduce sleep/backoff durations for dev loops (don't commit)
