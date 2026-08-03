# AGENTS.md

Orientation and conventions for working in the threeport codebase. `CLAUDE.md` in this
directory imports this file, so tools that discover either name load the same content.

# Architecture Overview

Threeport is a control plane for managing application workloads across Kubernetes clusters. The system uses four core components:

```
Client (tptctl) ──HTTP──> REST API Server ──GORM──> CockroachDB
                              │
                              │ NATS JetStream publish
                              ▼
                     NATS JetStream Broker
                              │
                              │ Pull subscription
                              ▼
                    Domain Controllers ──HTTP──> REST API Server
```

**REST API Server** (`cmd/rest-api/main_gen.go`): Echo-based HTTP server on port 1323. Persists objects to CockroachDB via GORM, publishes NATS notifications when reconcilable objects change. Health check on port 8081 at `/readyz`.

**CockroachDB** (`pkg/api-server/v0/database/database_gen.go`): Database `threeport_api`, default host `crdb:26257`, SSL mode `verify-full`. GORM with PostgreSQL dialect.

**NATS JetStream**: Message broker for async controller notifications. Streams initialized at API server startup (`cmd/rest-api/util/controller_stream_gen.go`). Also provides KV stores for distributed locking.

**Controllers** (`cmd/*-controller/main_gen.go`): One binary per domain. Each creates durable pull subscriptions, runs reconciler goroutines, and communicates back to the API server via HTTP client.

## Request Flow

1. Client sends REST request to API server
2. API server persists object to CockroachDB
3. If `Reconciled == false`, API publishes a `Notification` to the appropriate NATS subject
4. Controller pulls the message, acquires a lock (NATS KV), fetches latest object from API
5. Controller runs reconciliation logic, updates object status via API, releases lock, ACKs message

## API-First Understanding

Before digging into code to understand the API, refer to the Swagger API spec first to get a full picture of available endpoints, request/response shapes, and object relationships. The spec is the authoritative reference for API behavior.

# Definition/Instance Pattern

Every managed resource follows a two-level abstraction:

- **Definition**: declares _what_ to deploy (template/config). Embeds `Common`, `Definition`, `Reconciliation`.
- **Instance**: declares _where_ to deploy it (binds a definition to a runtime). Embeds `Common`, `Instance`, `Reconciliation`.

Base types in `pkg/api/v0/class.go`:

```go
type Definition struct {
    Name      *string  // required, unique name
    ProfileID *uint    // optional association
    TierID    *uint    // optional association
}

type Instance struct {
    Name   *string  // required, unique name
    Status *string  // optional status string
}
```

Common fields in `pkg/api/v0/common.go`:

```go
type Common struct {
    ID        *uint
    CreatedAt *time.Time
    UpdatedAt *time.Time
    DeletedAt *gorm.DeletedAt
}
```

## All Definition/Instance Pairs

| Definition | Instance | Domain |
|---|---|---|
| `KubernetesWorkloadDefinition` | `KubernetesWorkloadInstance` | kubernetes-workload |
| `HelmWorkloadDefinition` | `HelmWorkloadInstance` | helm-workload |
| `MachineWorkloadDefinition` | `MachineWorkloadInstance` | machine-workload |
| `KubernetesRuntimeDefinition` | `KubernetesRuntimeInstance` | kubernetes-runtime |
| `MachineRuntimeDefinition` | `MachineRuntimeInstance` | machine-runtime |
| `AwsEksKubernetesRuntimeDefinition` | `AwsEksKubernetesRuntimeInstance` | aws |
| `OciOkeKubernetesRuntimeDefinition` | `OciOkeKubernetesRuntimeInstance` | oci |
| `GcpGkeKubernetesRuntimeDefinition` | `GcpGkeKubernetesRuntimeInstance` | gcp |
| `GatewayDefinition` | `GatewayInstance` | gateway |
| `DomainNameDefinition` | `DomainNameInstance` | gateway |
| `SecretDefinition` | `SecretInstance` | secret |
| `TerraformDefinition` | `TerraformInstance` | terraform |
| `ControlPlaneDefinition` | `ControlPlaneInstance` | control-plane |
| `ObservabilityStackDefinition` | `ObservabilityStackInstance` | observability |
| `ObservabilityDashboardDefinition` | `ObservabilityDashboardInstance` | observability |
| `MetricsDefinition` | `MetricsInstance` | observability |
| `LoggingDefinition` | `LoggingInstance` | observability |
| `LogStorageDefinition` | `LogStorageInstance` | observability |

API object structs are in `pkg/api/v0/*.go`. The SDK config `sdk-config.yaml` declares which
objects are `Reconcilable: true`. Not every pair above is reconcilable on both halves. For the
cloud runtime domains and the machine domains, only the instance is reconcilable.

# NATS Streams, Subjects, Locks

## Streams

Each controller domain has one JetStream stream. Streams are created in `cmd/rest-api/util/controller_stream_gen.go` and named in `internal/*/notif/notif_gen.go`.

| Stream | Subjects (object types) |
|---|---|
| `kubernetesWorkloadStream` | `kubernetesWorkloadDefinition.*`, `kubernetesWorkloadInstance.*` |
| `helmWorkloadStream` | `helmWorkloadDefinition.*`, `helmWorkloadInstance.*` |
| `machineWorkloadStream` | `machineWorkloadInstance.*` |
| `kubernetesRuntimeStream` | `kubernetesRuntimeDefinition.*`, `kubernetesRuntimeInstance.*` |
| `machineRuntimeStream` | `machineRuntimeInstance.*` |
| `awsStream` | `awsEksKubernetesRuntimeInstance.*` |
| `ociStream` | `ociOkeKubernetesRuntimeInstance.*` |
| `gcpStream` | `gcpGkeKubernetesRuntimeInstance.*` |
| `gatewayStream` | `gatewayDefinition.*`, `gatewayInstance.*`, `domainNameInstance.*` |
| `secretStream` | `secretDefinition.*`, `secretInstance.*` |
| `terraformStream` | `terraformDefinition.*`, `terraformInstance.*` |
| `controlPlaneStream` | `controlPlaneDefinition.*`, `controlPlaneInstance.*` |
| `observabilityStream` | `observabilityStack{Definition,Instance}.*`, `observabilityDashboard{Definition,Instance}.*`, `metrics{Definition,Instance}.*`, `logging{Definition,Instance}.*` |

## Subject Pattern

Format: `{camelCaseObjectType}.{operation}` where operation is `create`, `update`, or `delete`.

Examples: `kubernetesWorkloadInstance.create`, `helmWorkloadDefinition.update`, `secretInstance.delete`

Controllers subscribe to wildcard: `{camelCaseObjectType}.*`

## Lock Buckets

Each domain has a NATS KV bucket for distributed locks (`internal/*/<domain>_gen.go`):

| Bucket | Domain |
|---|---|
| `kubernetesWorkloadLock` | kubernetes-workload |
| `helmWorkloadLock` | helm-workload |
| `machineWorkloadLock` | machine-workload |
| `kubernetesRuntimeLock` | kubernetes-runtime |
| `machineRuntimeLock` | machine-runtime |
| `awsLock` | aws |
| `ociLock` | oci |
| `gcpLock` | gcp |
| `gatewayLock` | gateway |
| `secretLock` | secret |
| `terraformLock` | terraform |
| `controlPlaneLock` | control-plane |
| `observabilityLock` | observability |

**Lock TTL:** 20 minutes (set in each controller's `main_gen.go`).

## Lock Key Format

From `pkg/controller/v0/reconcile.go`:

```
{ReconcilerName}.{ObjectID}
```

Example: `KubernetesWorkloadInstanceReconciler.42`

The value stored is the controller's UUID. Consumer names follow: `{ReconcilerName}Consumer`.

# Reconciliation Fields and Lifecycle

## Reconciliation Struct (`pkg/api/v0/common.go`)

| Field | Type | Purpose |
|---|---|---|
| `Reconciled` | `*bool` | `false` until controller finishes; set `true` on success |
| `CreationAcknowledged` | `*time.Time` | Controller acknowledges creation has begun |
| `CreationConfirmed` | `*time.Time` | Controller confirms creation is complete |
| `CreationFailed` | `*bool` | Set `true` if creation fails |
| `DeletionScheduled` | `*time.Time` | API server sets on first DELETE call |
| `DeletionAcknowledged` | `*time.Time` | Controller acknowledges deletion has begun |
| `DeletionConfirmed` | `*time.Time` | Controller confirms deletion is complete |
| `InterruptReconciliation` | `*bool` | Stops future reconciliation (e.g., to prevent runaway infra) |

## Create Lifecycle

1. `POST /v0/<objects>`: API creates object with `Reconciled=false`, publishes `<object>.create`
2. Controller pulls message, acquires lock, fetches latest from API
3. Calls hand-written `v0<Object>Created()` in `internal/<domain>/v0_<object>.go`
4. On success: sets `Reconciled=true` via API update, releases lock, ACKs message
5. On failure: releases lock, requeues with exponential backoff (1s initial, 30s max)

## Update Lifecycle

1. `PATCH /v0/<objects>/:id`: API updates object, sets `Reconciled=false`, publishes `<object>.update`
2. Same lock/reconcile/release flow as create
3. Calls hand-written `v0<Object>Updated()`

## Delete Lifecycle (Two-Phase)

1. First `DELETE /v0/<objects>/:id`: API sets `DeletionScheduled=now()`, `Reconciled=false`, publishes `<object>.delete`
2. Subsequent DELETE while reconciling returns **409 Conflict**
3. Controller calls `v0<Object>Deleted()`, performs cleanup
4. Controller sets `DeletionAcknowledged`, `DeletionConfirmed`, `Reconciled=true`
5. Controller calls DELETE again, API sees `DeletionConfirmed` is set and removes row from DB

## Requeue Backoff (`pkg/controller/v0/requeue.go`)

- Initial delay: 1 second
- Maximum delay: 30 seconds
- Doubles based on elapsed time since notification `CreationTime`
- Uses NATS `NakWithDelay` to requeue messages

# Kubernetes Mapping

## Namespace Conventions

| Threeport Construct | Kubernetes Namespace | Source |
|---|---|---|
| Control plane components | `threeport-control-plane` | `pkg/threeport-installer/v0/threeport.go` |
| Gateway resources | `nukleros-gateway-system` | `pkg/util/v0/constants.go` |
| KubernetesWorkloadInstance | `{name}-{10alphanumChars}` | `pkg/kube/v0/namespace.go` |
| HelmWorkloadInstance | `{name}-{10alphaChars}` (or user-specified `ReleaseNamespace`) | `internal/helm-workload/v0_helm_workload_instance.go` |

## Naming Patterns

| Resource | Name Pattern | Source |
|---|---|---|
| Helm release | `{instanceName}-release` | `internal/helm-workload/v0_helm_workload_instance.go` |
| ThreeportWorkload CRD instance | `workload-instance-{ID}` or `helm-workload-instance-{ID}` | `internal/agent/agent.go` |

## ThreeportWorkload CRD

- Cluster-scoped custom resource
- API group: `control-plane.threeport.io`
- Version: `v1alpha1`
- Source: `pkg/agent/api/v1alpha1/groupversion_info.go`, `threeportworkload_types.go`

## Labels Applied to Managed Resources

From `pkg/kube/v0/metadata.go` and `internal/agent/agent.go`:

| Label | Value | Applied To |
|---|---|---|
| `app.kubernetes.io/managed-by` | `threeport` | All managed resources |
| `app.kubernetes.io/name` | `{definitionName}` | All managed resources |
| `app.kubernetes.io/instance` | `{instanceName}` | All managed resources |
| `control-plane.threeport.io/managed-by` | `threeport` | All managed resources + namespaces |
| `control-plane.threeport.io/kubernetes-workload-instance` | `{ID}` | Workload resources |
| `control-plane.threeport.io/helm-workload-instance` | `{ID}` | Helm workload resources |

## kubectl Examples

```bash
# List all Threeport-managed namespaces
kubectl get ns -l control-plane.threeport.io/managed-by=threeport

# List resources in a workload namespace
kubectl get all -n {workloadName}-{randomSuffix}

# List resources by instance name
kubectl get all -l app.kubernetes.io/instance={instanceName}

# List ThreeportWorkload CRD instances
kubectl get threeportworkloads

# Control plane pods
kubectl get pods -n threeport-control-plane

# Control plane component logs
kubectl logs deploy/threeport-api-server -n threeport-control-plane
kubectl logs deploy/threeport-kubernetes-workload-controller -n threeport-control-plane
kubectl logs deploy/threeport-helm-workload-controller -n threeport-control-plane
kubectl logs deploy/threeport-kubernetes-runtime-controller -n threeport-control-plane
```

# Directory Layout

```
threeport/
├── cmd/                           # Binary entry points
│   ├── rest-api/                  # API server [generated main_gen.go]
│   ├── kubernetes-workload-controller/       # [generated main_gen.go]
│   ├── helm-workload-controller/  # [generated main_gen.go]
│   ├── machine-workload-controller/
│   ├── kubernetes-runtime-controller/
│   ├── machine-runtime-controller/
│   ├── gateway-controller/
│   ├── aws-controller/
│   ├── oci-controller/
│   ├── gcp-controller/
│   ├── control-plane-controller/
│   ├── observability-controller/
│   ├── secret-controller/
│   ├── terraform-controller/
│   ├── database-migrator/         # [generated main_gen.go]
│   ├── agent/                     # Kubernetes agent
│   ├── tptctl/                    # CLI tool [generated-then-modified cmd/*.go]
│   ├── tptdev/                    # Developer tool [hand-written]
│   └── sdk/                       # SDK code generation tool
├── internal/                      # Controller reconciliation logic (per domain)
│   ├── kubernetes-workload/
│   │   ├── kubernetes_workload_gen.go                          # [generated] lock bucket consts
│   │   ├── notif/notif_gen.go                                  # [generated] stream/subject consts
│   │   ├── kubernetes_workload_instance_reconciler_gen.go      # [generated] reconciler loop
│   │   ├── v0_kubernetes_workload_instance.go                  # [hand-written] business logic
│   │   └── v0_kubernetes_workload_definition.go                # [hand-written] business logic
│   ├── helm-workload/             # Same pattern as kubernetes-workload/
│   ├── machine-workload/
│   ├── kubernetes-runtime/
│   ├── machine-runtime/
│   ├── gateway/
│   ├── aws/
│   ├── oci/
│   ├── gcp/
│   ├── control-plane/
│   ├── observability/
│   ├── secret/
│   ├── terraform/
│   ├── agent/                     # Agent constants and helpers
│   ├── provider/                  # Cloud provider utilities
│   └── version/
├── pkg/                           # Shared library packages
│   ├── api/v0/                    # [hand-written] API object struct definitions
│   │   ├── *_gen.go               # [generated] interface method implementations
│   │   ├── common.go              # Common, Reconciliation structs
│   │   ├── class.go               # Definition, Instance base structs
│   │   ├── kubernetes_workload.go # KubernetesWorkloadDefinition, KubernetesWorkloadInstance
│   │   └── ...                    # Other domain object definitions
│   ├── api/lib/v0/                # ReconciledThreeportApiObject interface
│   ├── api-server/v0/
│   │   ├── handlers/*_gen.go      # [generated] REST CRUD handlers
│   │   ├── handlers/handlers.go   # [hand-written] Handler struct
│   │   ├── routes/*_gen.go        # [generated] route registrations
│   │   └── database/database_gen.go # [generated] DB init
│   ├── client/v0/*_gen.go         # [generated] API client functions
│   ├── controller/v0/             # [hand-written] controller framework
│   │   ├── reconcile.go           # Reconciler struct, PullMessage, Lock, ReleaseLock
│   │   └── requeue.go             # Backoff delay calculation
│   ├── notifications/v0/          # [hand-written] Notification types
│   ├── config/v0/                 # [generated] CLI config structures
│   ├── cli/v0/                    # CLI output utilities (JSON, YAML, tabular)
│   ├── kube/v0/                   # Kubernetes helpers (namespace, labels, client)
│   ├── agent/api/v1alpha1/        # ThreeportWorkload CRD types
│   ├── threeport-installer/v0/    # Control plane installer
│   └── util/v0/                   # General utilities (random strings, constants)
├── docs/                          # Documentation
├── hack/                          # Development helper files (env files, scripts)
├── magefiles/                     # Mage build system files
├── samples/                       # Example config files
├── test/                          # Tests
├── sdk-config.yaml                # SDK code generation configuration
├── Makefile                       # Dev/debug targets
└── go.mod
```

# Generated Files

**Never manually edit a file ending in `_gen.go`.** These are regenerated by `threeport-sdk gen` and any manual edits are silently overwritten.

To change behavior in generated code:

1. Find the corresponding generator in `pkg/sdk/v0/gen/` (handler generation is in `pkg/sdk/v0/gen/pkg/api-server/handlers.go`)
2. Modify the generator code (jennifer/jen Go code generation)
3. Rebuild the SDK: `mage install:sdk`
4. Regenerate: `threeport-sdk gen -c sdk-config.yaml`
5. Verify the generated output reflects your changes

## Generated, Generated-Once, and Hand-Written

**Generated, do not edit** (`_gen.go` suffix, header `// generated by 'threeport-sdk gen' - do not edit`):

- `cmd/*/main_gen.go`: entry points
- `internal/*/notif/notif_gen.go`: NATS stream and subject constants
- `internal/*/*_reconciler_gen.go`: reconciler loops
- `internal/*/*_gen.go`: lock bucket constants
- `pkg/api/v0/*_gen.go`, `pkg/api/v0/table_name_gen.go`: interface method implementations
- `pkg/api-server/v0/handlers/*_gen.go`: REST CRUD handlers
- `pkg/api-server/v0/routes/*_gen.go`: route registrations
- `pkg/api-server/v0/versions/*_gen.go`
- `pkg/api-server/v0/module_gen.go`, `pkg/api-server/v0/tagged_fields_gen.go`
- `pkg/api-server/v0/database/database_gen.go`: database initialization
- `pkg/client/v0/*_gen.go`, `pkg/client/v0/delete_object_gen.go`: API client functions
- `magefiles/magefile_gen.go`

**Generated once, then hand-modified** (no `_gen` suffix, header `// generated by 'threeport-sdk gen' but will not be regenerated - intended for modification`):

- `internal/*/v0_*.go`: reconciliation business logic
- `cmd/tptctl/cmd/*.go`: CLI command definitions
- `cmd/*/image/Dockerfile*`: see Dockerfile Patterns below

**Hand-written** (no generation header):

- `pkg/api/v0/*.go` without `_gen`: API struct definitions
- `pkg/controller/v0/*.go`: controller framework
- `pkg/notifications/v0/*.go`: notification types
- `pkg/kube/v0/*.go`: Kubernetes helpers
- `cmd/tptdev/cmd/*.go`: developer tool

## SDK Code Generation Circular Dependency

`mage install:sdk` compiles the entire project because the magefiles import project packages. If you rename or change a type in `pkg/api/v0/*.go`, the stale `_gen.go` files reference the old type and will not compile, which blocks the SDK binary build needed to regenerate them.

Safe workflow for type changes:

1. Build the SDK binary first, before changing source types: `git stash`, then `mage install:sdk`, then `git stash pop`. Or confirm `$GOPATH/bin/threeport-sdk` already exists from a prior build.
2. Edit `sdk-config.yaml` and `pkg/api/v0/*.go`, the source-of-truth files
3. Regenerate: `threeport-sdk gen -c sdk-config.yaml`
4. Update the non-generated files by hand (config, CLI, controller, provider, bootstrap, kube, migrations)
5. Verify with the regeneration idempotence check under Local Verification below

# Build and Verify

**IMPORTANT**: Use mage for building tptctl, NOT `go build`.

```bash
# Build tptctl binary
mage build:tptctl

# NOT: go build ./...
```

## Local Verification

Default to these local checks before committing, in order:

1. `go vet ./...`: static check
2. `mage build:tptctl`: CLI compiles
3. `tptdev build --names <changed components> --parallel=2`: verify the container images that actually get deployed compile (no `--push` needed locally)
4. `threeport-sdk gen -c sdk-config.yaml` followed by `git diff --name-only | grep '_gen.go'`: should produce no diff (idempotence). A diff means either you manually edited a `_gen.go` file, or you changed source types in `pkg/api/v0/*.go` or `sdk-config.yaml` without regenerating.

Run check 4 after every commit touching `pkg/api/v0/*.go` or `sdk-config.yaml`. If any `_gen.go` files show changes, the commit is out of sync; regenerate before pushing.

**Do not default to `go test ./...`.** Every test in this repo is integration or e2e and assumes a live control plane, so local runs fail for infrastructure reasons rather than code correctness. Run them only against a real environment (`mage test:integration`, `mage test:e2eLocal`, etc.).

**Do not use `go build ./cmd/<controller>/`.** It leaves untracked binaries in the repo root (e.g. `database-migrator`), and `main_gen.go` is not the actual deployment artifact anyway. `tptdev build` produces the image that gets deployed and is fast on subsequent runs thanks to the Go build cache. If you run `go build` for any reason, delete the binary immediately; they are not gitignored.

## Using threeport-sdk

- **Don't stat or which-check for the `threeport-sdk` binary.** Just run `mage install:sdk` when you need it. The build is a no-op when up to date and cheap when not, and it guarantees the binary matches the current source. Running `ls ~/go/bin/threeport-sdk` or `which threeport-sdk` first is wasted motion (and often triggers a permission prompt for no benefit).

## Prefer mage, with one exception

- **Always use mage** as the entrypoint for tasks that have a mage target: `mage install:sdk`, `mage build:tptctl`, `mage dev:generate`, `mage test:integration`, etc. It's the canonical interface and knows how to wire up dependencies.
- **Exception, source types in flux.** `mage` itself must compile every magefile before running any target, and the magefiles import project packages. If you've changed a source type in `pkg/api/v0/*.go` or removed an object from `sdk-config.yaml` without regenerating yet, `mage` will fail with `undefined: …` errors before it can even start. In that narrow window, call the already-installed underlying binary directly: `threeport-sdk gen -c sdk-config.yaml`. Once regeneration is complete and the tree compiles again, go back to using `mage`.

# tptctl

## Binary Location

`mage build:tptctl` places the binary at `bin/tptctl` relative to the repo root. `mage install:tptctl` builds it and copies it into `$GOBIN` (or `$GOPATH/bin`). If you only built it, either add the repo's `bin/` directory to `PATH` or reference it by full path:

```bash
export PATH="$(git rev-parse --show-toplevel)/bin:$PATH"
tptctl get kubernetes-workload-instances
```

## Syntax

```
tptctl {verb} {object-type} [flags]
```

**Verbs:** `get`, `create`, `delete`, `replace`, `describe`

**Common flags:**
- `-n, --name`: Object name
- `-c, --config`: Path to YAML config file
- `--stdin`: Read config from stdin instead of file
- `-v, --version`: API version (default `v0`)
- `-o, --output`: Output format: `tabular` (default), `yaml`, `json`
- `-i, --control-plane-name`: Target control plane name

**Other commands:** `up`, `down`, `config`, `upgrade`, `version`

Kubernetes workload object types carry short aliases: `kw` for `kubernetes-workload`, `kwd` for `kubernetes-workload-definition`, `kwi` for `kubernetes-workload-instance`.

## Always Use `--stdin` for Create/Replace

**ALWAYS** pipe config into tptctl using `--stdin` instead of writing temporary files. This keeps commands readable for users following along:

```bash
# CORRECT: pipe config into --stdin
cat <<EOF | tptctl create kubernetes-workload --stdin
KubernetesWorkload:
  Name: my-app
  YAMLDocument: path/to/manifest.yaml
  KubernetesWorkloadInstance:
    Name: my-app
EOF

# CORRECT: with variable substitution for credentials
cat <<EOF | tptctl create aws-account --stdin
AwsAccount:
  Name: my-aws
  AccessKeyID: $AWS_ACCESS_KEY_ID
  SecretAccessKey: $AWS_SECRET_ACCESS_KEY
EOF

# WRONG: don't write config files
tptctl create kubernetes-workload --config /tmp/workload.yaml
```

## Credential Safety

**NEVER** read private keys, credentials, or secrets directly with the Read tool. If a credential file exists on disk, load it into a shell variable first:

```bash
# CORRECT: load credential into variable, reference in heredoc
AWS_KEY=$(cat ~/.aws/secret-key)
cat <<EOF | tptctl create aws-account --stdin
AwsAccount:
  Name: my-aws
  SecretAccessKey: $AWS_KEY
EOF

# WRONG: never read credential files directly
# Read tool on ~/.aws/credentials  <-- DO NOT DO THIS
```

## Examples

```bash
# List all kubernetes workload instances as JSON
tptctl get kubernetes-workload-instances -o json

# Get a specific kubernetes workload definition as YAML
tptctl get kubernetes-workload-definitions --name my-def -o yaml

# List all helm workload instances
tptctl get helm-workload-instances -o json

# List all kubernetes runtime instances
tptctl get kubernetes-runtime-instances -o json

# Create a kubernetes workload via stdin
cat <<EOF | tptctl create kubernetes-workload --stdin
KubernetesWorkload:
  Name: my-app
  YAMLDocument: path/to/manifest.yaml
  KubernetesWorkloadInstance:
    Name: my-app
EOF

# Delete a kubernetes workload instance by name
tptctl delete kubernetes-workload-instance --name my-instance

# Replace (update) a kubernetes workload definition
cat <<EOF | tptctl replace kubernetes-workload-definition --stdin --name existing-def
KubernetesWorkloadDefinition:
  Name: existing-def
  YAMLDocument: path/to/new-manifest.yaml
EOF
```

# tptdev Debug Workflow

## Development Setup

Always use the kind provider with debug mode for local development:

```bash
# Spin up a dev environment with kind
tptdev up

# Enable debug mode, ALWAYS do this after `tptdev up`
tptdev debug
```

**Why debug mode matters:** `tptdev debug` switches `ImagePullPolicy` to `Always` for all threeport components. Without this, Kubernetes may use cached images instead of freshly built ones, causing confusion where code changes appear to have no effect. Always enable debug mode to avoid stale image issues.

## What Debug Mode Changes

Debug mode is implemented across several functions in `pkg/threeport-installer/v0/components.go`:

| Aspect | Default | Debug Enabled |
|---|---|---|
| Image pull policy | `IfNotPresent` (cached) | `Always` (always pull fresh) |
| REST API verbose logging | Off | `--verbose=true` |

**Functions involved:** `getImagePullPolicy()`, `getRestApiArgs()`, `getControllerArgs()`, `getAgentArgs()`, `getCommand()`, all in `components.go`.

## Commands

```bash
# Enable debug mode for all components (sets debug images, enables delve, ImagePullPolicy=Always)
tptdev debug

# Debug specific components only
tptdev debug --names rest-api,kubernetes-workload-controller

# Enable verbose logging
tptdev debug --verbose

# Disable debug mode (reverts ImagePullPolicy, removes debug images)
tptdev debug --disable

# Build and push images to remote registry, limiting concurrent builds
tptdev build --names rest-api,kubernetes-workload-controller --push --parallel=2

# Build, push, and restart pods to pick up new images immediately
tptdev build --names rest-api,kubernetes-workload-controller --push --restart

# Tear down dev environment
tptdev down
```

Raise `--parallel` above 2 only if the machine has resources to spare.

## Flags

- Always push container images to a remote registry; never assume local images are sufficient
- Always use `-n <name>` for `tptdev up/down/debug`
- Use `-t <branch>` for image tags in worktrees (auto-detection breaks)
- Temporarily reduce sleep/backoff durations for dev loops, and don't commit those changes

## Build-Test Cycle

After making code changes:

1. Build and push the changed component: `tptdev build --names <component> --push`
2. Kubernetes pulls the new image automatically (debug mode ensures `ImagePullPolicy=Always`)
3. Check logs: `kubectl logs deploy/threeport-<component> -n threeport-control-plane -f`

## Makefile Targets

| Target | Description |
|---|---|
| `dev-logs-api` | Follow API server logs |
| `dev-logs-wrk` | Follow kubernetes workload controller logs |
| `dev-logs-gw` | Follow gateway controller logs |
| `dev-logs-kr` | Follow kubernetes runtime controller logs |
| `dev-logs-aws` | Follow AWS controller logs |
| `dev-logs-cp` | Follow control plane controller logs |
| `dev-logs-agent` | Follow agent logs |
| `dev-query-crdb` | Open CockroachDB SQL shell |
| `dev-reset-crdb` | Truncate all working tables in CockroachDB |
| `dev-purge-streams` | Purge all NATS JetStream streams |
| `dev-sub-nats` | Subscribe to all NATS messages for debugging |
| `dev-uninstall-helm` | Uninstall helm releases installed by the control plane |
| `dev-debug-api` | Start delve session for API server |
| `dev-debug-wrk` | Start delve session for kubernetes workload controller |
| `dev-debug-gateway` | Start delve session for gateway controller |

# Command Readability

Before running a `kubectl exec` command (especially piped one-liners), always describe in plain text what the command does. These commands are often long and don't fit on the user's terminal, so the description ensures the user understands what is being executed.

# Debugging Decision Trees

## Workload Not Deploying

1. Check reconciliation status: `tptctl get kubernetes-workload-instances -o json`, look at `Reconciled`, `CreationFailed`, `InterruptReconciliation`
2. If `Reconciled=false` and no `CreationFailed`: controller may be processing or stuck
   - Check controller logs: `kubectl logs deploy/threeport-kubernetes-workload-controller -n threeport-control-plane`
   - Check for lock stuck in NATS KV (lock key: `KubernetesWorkloadInstanceReconciler.{ID}`, TTL: 20 min)
3. If `CreationFailed=true`: check controller logs for error, fix issue, update object to retry
4. Check Kubernetes resources: `kubectl get all -n {workloadName}-{suffix}`
5. Check events: `kubectl get events -n {workloadName}-{suffix} --sort-by=.lastTimestamp`

## Workload Not Deleting

1. Check deletion status: `tptctl get kubernetes-workload-instances -o json`, look at `DeletionScheduled`, `DeletionAcknowledged`, `DeletionConfirmed`
2. If `DeletionScheduled` set but no `DeletionAcknowledged`: controller hasn't started cleanup
   - Check controller logs for errors
3. If `DeletionAcknowledged` set but no `DeletionConfirmed`: cleanup is in progress or stuck
   - Check Kubernetes for remaining resources: `kubectl get all -l control-plane.threeport.io/kubernetes-workload-instance={ID}`
4. Call DELETE again once `DeletionConfirmed` is set to remove from database

## Controller Not Processing Messages

1. Verify controller is running: `kubectl get pods -n threeport-control-plane`
2. Verify NATS connectivity: `kubectl logs deploy/threeport-{domain}-controller -n threeport-control-plane | grep -i nats`
3. Check for stuck locks: lock TTL is 20 minutes, stale locks auto-expire
4. Purge NATS streams if needed: `make dev-purge-streams`
5. Subscribe to all NATS messages for debugging: `make dev-sub-nats`

# Cloud Resources

Always be mindful of cloud resource state being managed. Threeport is, at its core, infrastructure management, and excess or forgotten resources cost real money in both development and production. Clean up resources promptly after testing. Don't leave clusters, VPCs, or other billable resources running unnecessarily.

## Use Threeport First

**ALWAYS** create, modify, and delete cloud resources (Kubernetes clusters, VPCs, load balancers, DNS records, etc.) through threeport's API and CLI. Threeport tracks the state of all managed resources in its database and reconciles them via controllers. Modifying resources directly through cloud-provider CLIs or MCP tools (e.g., `aws`, `oci`, `gcloud`, `kubectl apply`) will cause state drift; threeport won't know about the change and may overwrite it, fail to clean it up, or behave unpredictably.

**When to use cloud-provider tools:**
- **Debugging**: Inspecting resource state, checking logs, describing cloud objects to understand failures
- **Orphaned resource cleanup**: After a failed threeport operation leaves resources behind that threeport can no longer manage (e.g., after a force-deleted control plane)
- **Read-only queries**: Listing resources, checking quotas, verifying configuration

**Always check the correct region.** When debugging or cleaning up cloud resources, make sure you are querying the same region where threeport deployed them. It is easy to conclude that resources are orphaned or missing when you are simply looking in the wrong region. Check the threeport config or runtime instance to confirm the target region before running cloud-provider commands.

```bash
# CORRECT: create infrastructure through threeport
cat <<EOF | tptctl create kubernetes-runtime --stdin
KubernetesRuntime:
  Name: my-cluster
  ...
EOF

# CORRECT: use cloud CLI for debugging
aws eks describe-cluster --name my-cluster  # read-only inspection
oci ce cluster get --cluster-id ocid1...    # read-only inspection

# WRONG: create or modify cloud resources directly
aws eks create-cluster --name my-cluster    # threeport won't know about this
oci ce cluster update --cluster-id ocid1... # will cause state drift
```

## Tearing Down a Control Plane, CRITICAL

**NEVER** delete a genesis control plane or run `tptdev down` / `tptctl down` while cloud provider resources are still deployed. This is especially easy to forget with kind clusters; deleting the kind cluster orphans cloud resources (EKS clusters, OKE clusters, VPCs, etc.) that will continue incurring costs and must be manually cleaned up.

**Always clean up in this order:**

1. Delete all workload instances and helm workload instances
2. Delete all kubernetes runtime instances (EKS, OKE, GKE, etc.), and wait for cloud resources to be fully deprovisioned
3. Delete all other managed instances (secrets, terraform, observability, etc.)
4. Verify no cloud resources remain: check the cloud provider console
5. Only then: `tptdev down` or `tptctl down`

Two things that make this easy to get wrong:

- **Zero worker nodes is not zero infrastructure.** A cluster scaled to 0 nodes still has the managed control plane, VCN or VPC, subnets, and gateways.
- **Workload cluster resources are managed by genesis controllers.** Once genesis is gone, they are orphaned. Never delete DB records for clusters that still have real cloud infrastructure.

```bash
# Check what's still deployed before tearing down
tptctl get kubernetes-workload-instances -o json
tptctl get helm-workload-instances -o json
tptctl get kubernetes-runtime-instances -o json
tptctl get aws-eks-kubernetes-runtime-instances -o json
tptctl get oci-oke-kubernetes-runtime-instances -o json
tptctl get gcp-gke-kubernetes-runtime-instances -o json
tptctl get terraform-instances -o json
```

## OCI Free Tier Testing Strategy

- OKE control plane creation is free; only worker nodes incur cost
- For workload cluster testing, set `workerInitialNodeCount` to 0
- Genesis control planes can be left running (free tier)

# Dockerfile Patterns

## Generation Model
- SDK generates Dockerfiles once (`threeport-sdk gen`), then never overwrites them
- Header: `# generated by 'threeport-sdk gen' but will not be regenerated - can optionally be edited`
- Templates: `pkg/sdk/v0/util/dockerfile.go`, orchestrated by `pkg/sdk/v0/gen/cmd/cmd.go`
- Safe to customize after generation; SDK skips existing files

## Three Variants Per Component (`cmd/<component>/image/`)
- `Dockerfile`: production multi-stage, golang builder into distroless/static:nonroot, builds from source
- `Dockerfile-goreleaser`: CI/CD, copies pre-built binary into distroless
- `Dockerfile-alpine`: development, copies local `bin/<binary>` into golang-alpine

## Development Image (`cmd/tptdev/image/Dockerfile`)
- **NOT generated**, manually maintained, multi-target build
- Targets: `base` (delve + wait-for-tcp), `dev` (standard), `dev-terraform` (+ Terraform binary), `dev-oci` (+ Pulumi CLI)
- `tptdev build` selects target via `--target` flag based on component type
- Build flow: compile Go binary locally, then copy into image via target

## Customized Production Dockerfiles
- **terraform-controller**: downloads Terraform binary at build time
- **helm-workload-controller**: creates `/var/helm` cache with non-root permissions
- **agent**: builds from `main.go` not `main_gen.go`

## Build Commands
- Production: `docker buildx` with component's own `Dockerfile`
- Development: `tptdev build --names <component> --parallel 2 --push -t <tag> -r <repo>`
- All production images: non-root (USER 65532:65532), include wait-for-tcp

# Threeport Code Conventions

## Reading Before Writing

Before writing comments, docstrings, or new code in an existing package, grep that package for analogous code and match the local pattern. The rules below state defaults; the package is the authoritative source for the *shape* of code. If the package convention conflicts with a rule below, surface the conflict before changing direction.

## Moving Logic Means Moving It

When asked to "move" or "extract" logic, relocate it verbatim. Preserve inline comments, local variable names, loop structure, and multi-line forms, even if the result feels awkward in its new home or you'd write it differently from scratch. Refactoring during a move conflates two changes and makes review much harder. If the moved code obviously needs cleanup, surface it as a follow-up; don't bundle it with the move.

## Return Validation Errors Early

In validation functions, reject and return as soon as a precondition fails. Each check should be a flat top-level guard: validate, return on failure, fall through to the next. Code below the check can then assume the error case has been handled, which makes the function easy to extend: append a new check at the bottom and trust everything above ran cleanly.

The inverted shape, wrapping the happy path inside nested `if`/`else` branches that depend on prior conditions, makes the function fragile. Logic added later can land in a branch that's never reached, with no compile-time signal that anything is wrong.

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
- Loop and very-short-scope variables (`i`, `j`, `err`, `ok`) are fine as-is; the rule is about *abbreviations of domain words*, not standard Go idioms.

## Import Aliases
- **Alias by the package's conceptual name, not its version.** Versioned threeport packages all declare `package v0`, so importing them without an alias would name them all `v0` and clash. Pick the meaningful suffix:
  - `pkg/api/v0` -> alias `api`
  - `pkg/client/v0` -> alias `client`
  - `pkg/encryption/v0` -> alias `encryption`
  - `pkg/sdk/v0` -> alias `sdk`
  - `pkg/util/v0` -> alias `util`
- **Avoid `v0`, `v01`, `api_v0`, `client_v0`** as aliases. `v0` says nothing about which package; `api_v0` is redundantly versioned in a file that already implies v0.
- **Use the longer form (e.g. `api_v0`) only when the compiler requires it**, i.e. two distinct versioned packages with the same conceptual name needing to coexist in the same file. A file in `package api` importing `pkg/api/v0` as `api` is fine; Go allows the alias to shadow the local package name without symbol collision.
- **Function parameter wins over import alias when names collide.** If a function parameter naturally takes the package's conceptual name (e.g. `func F(gen *gen.Generator)`), keep the parameter on its natural name and give the import a longer alias (`sdkgen "github.com/.../sdk/v0/gen"`) so package-level calls inside the body remain reachable. This matters when the body calls a package function (e.g. `sdkgen.ParseRelationshipTagValue(rel)`); otherwise the parameter shadows the package inside the function body and the package call fails to compile.

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
- **NO EMOJIS**, use plain text only
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
- `cli.Info("message")` produces "Info: message"
- `cli.Error("message", err)` produces "Error: message" (in red)
- `cli.Warning("message")` produces "Warning: message" (in yellow)
- `cli.Complete("message")` produces "Complete: message" (in green)

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
- **Use `if ...; err != nil` whenever possible.** Inline the call in the `if`, check the error in the block, and keep the happy path after the `if`, not nested in an `else` branch. When the call returns a value that needs to survive past the `if`, pre-declare the variable with `var` and use `=` in the `if` statement.

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

Avoid the `val, err := f(); if err != nil { ... }` two-line form and avoid wrapping the happy path in an `else` branch; both are less consistent with the rest of the codebase.

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
- **Do not hand-align struct literal fields**, always rely on `gofmt` (or the editor's format-on-save, which runs gofmt)
- `gofmt` aligns struct literal fields per-literal based on the longest field name in that specific literal, using tabs
- Different struct literals in the same file will have different alignment depending on their longest field. This is correct behavior, don't try to make them all match
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
- **yaml**: drop. threeport uses `sigs.k8s.io/yaml` for all YAML handling (tptctl config files, CLI output, SDK config), which routes YAML through JSON via `encoding/json` with case-insensitive field matching. Yaml struct tags are functionally vestigial; the JSON tag determines the wire format. The historical concern about `yaml`'s library lowercasing field names by default applies only to `gopkg.in/yaml.v2`/`v3` used directly, which the codebase doesn't. Don't add yaml tags to new types; drop them when touching existing types.
- **query**: forbidden. The `QueryBinder` in `pkg/api-server/lib/v0/binder.go` derives keys from `strings.ToLower(field.Name)`. An explicit query tag is noise at best and a silent rename hazard at worst.

Tag order convention (hand-written types): `json`, then `validate`, then `gorm`, then `encrypt`, then `relationship`, then `persist`. Example:

```go
KubernetesWorkloadDefinitionID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`
```

Rationale: `json:",omitempty"` and `validate:"required|optional"` are the strongest semantic pair (they together drive the `PayloadCheck()` null-on-required guard), so keeping them adjacent makes the contract scannable at a glance.

jen's `.Tag(map)` sorts alphabetically (gorm before json), so generated code does not naturally land in convention order. `pkg/sdk/v0/create/api.go` uses a custom ordered-tag helper (`util.Tag`) to emit the convention order at scaffold time.

Why per-field rather than a global toggle: Go's `encoding/json` doesn't support "omitempty by default" as a marshaler option. The experimental `encoding/json/v2` proposal ([#71497](https://github.com/golang/go/issues/71497), in Go 1.25) redefines what "empty" means but still requires per-field tagging.

### Nil Checks on API Type Pointer Fields
- **Do not add defensive nil checks for fields that are guaranteed non-nil by GORM constraints.** If a field has `gorm:"not null"` and `validate:"required"`, it cannot be nil when read from the database
- Reconcilers and other code that receives API objects from the database or notification payloads can dereference these fields directly without a nil guard
- Only check for nil on fields that are actually nullable, i.e. those with `validate:"optional"` and no `gorm:"not null"` constraint
- The distinction matters: defensive nil checks on non-nullable fields add noise, suggest to readers that the field might legitimately be missing (it can't), and hide real bugs if the field does somehow end up nil (the reconciler silently skips instead of failing loudly)

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
- For values used multiple times (e.g., a shared timestamp written to several events), a local variable is still appropriate; `util.Ptr` is for one-shot inline use

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
- **Local variables mirror the API type's field/identifier names**, not ad-hoc shorthands.  Prefer `attachedObjectType` / `attachedObjectID` over `t` / `id`.  Short names are for structurally-anonymous values (iterator counters, buffers), never for named domain concepts.
- **Inline one-shot struct literals at the call site.** When a struct is constructed and immediately passed into another function, pass the literal directly instead of assigning it to a named local first. A local like `ref := &v0.Foo{...}; Create(..., ref)` adds a line without adding meaning. Use a named local only when the value is referenced more than once or the name clarifies intent the struct type doesn't.
- **Return shape**: helpers return a single `error` and, optionally, a value.  Avoid `(error, error)` return signatures; split the concerns into separate functions.  Example: a finder returns `(results, total, error)`; a formatter returns `error`; the caller decides which kind of response to produce.
- **Un-wrapped errors use `errors.New(msg)`**, not `fmt.Errorf("%s", msg)`.  The format-verb pattern mimics wrapping without doing it and creates confusion during code review.

### File Naming in Library Packages

For shared library packages like `pkg/api-server/lib/v0/`, files are named by **capability** rather than implementation (see the existing `response.go`, `validator.go`, `pagination.go`, `context.go`).  A short noun describing the concern is preferred over a longer acronym-heavy name, e.g. `blocking_references.go` over `attached_object_reference_blocking_guard.go`.

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
- **Describe what, not why-we-discussed-it**: Comments should state what the code does, not replay the reasoning or conversation that led to the implementation. If context is needed, keep it to one short clause; don't write paragraphs explaining trade-offs or alternative approaches in inline comments

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

Any API type in `pkg/api/v0/` with one or more `encrypt:"true"` fields requires matching treatment in the config abstraction and the tptctl command. **Do not skip this for new types.** `pkg/config/v0/aws_account.go` is the canonical reference.

## The pattern

1. **Config struct `Get` takes an `encryptionKey string` parameter.** Inside the per-object loop, call one of:
   - Empty key: `encryption.RedactEncryptedValues(&obj)` so `tptctl get` output shows `"[encrypted value redacted]"` instead of ciphertext.
   - Non-empty key: `encryption.DecryptValues(&obj, encryptionKey)` to return plaintext.

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

Without this, `tptctl get` for those objects returns raw AES-GCM ciphertext, which is unusable and potentially confusing. Redacting by default keeps output readable; the `-d` flag opts into plaintext when the user wants to see credentials (e.g. during debugging, or exporting a config for another control plane).

## Checklist when adding an `encrypt:"true"` field

- [ ] API type field tagged `encrypt:"true"` (and, for nullable secrets, also `validate:"optional"`)
- [ ] `pkg/config/v0/<type>.go` `Get` takes `encryptionKey` + calls Redact/Decrypt
- [ ] Composite wrapper (if any) threads the key through
- [ ] `cmd/tptctl/cmd/<type>.go` Get commands have the decrypt block + `-d` flag

# Event Recording in Controllers

Events are user-facing signals about an API object, what `tptctl get events --for <kind>/<name>` returns. Controllers record them via `r.EventsRecorder.RecordEvent`. The generated reconciler already emits success/failure events for each reconcile op (Create/Update/Delete); hand-written reconcile code only needs to record events for things the generated layer can't see.

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

- **External-dependency outcomes**: SSH connection succeeded/failed, remote host unreachable, cluster not yet ready. These explain *why* reconciliation is stuck or recovered.
- **Operation outcomes with specific detail**: `ScriptSucceeded` / `ScriptFailed` with exit code, timeout, or a truncated stdout/stderr snippet. The generated layer only says "reconcile failed"; the event explains what failed.
- **One-time state transitions**: first successful reach, host key captured on first connect, credentials propagated. Useful milestones the user couldn't infer from a steady-state status.
- **Retryable errors the user should see**: e.g. "will retry after 30s because X". Helps operators distinguish "stuck but recovering" from "actually broken".

## What does NOT belong in an event

- **Heartbeat / per-tick noise**: "checked status, still healthy". Dedup helps, but don't emit these at all.
- **Anything derivable from Status**: if `obj.Status == "Healthy"` conveys the same info, the event is redundant.
- **Debug/trace**: use `log.V(1).Info(...)` for developer-level detail.
- **Implementation internals**: "acquired lock", "parsed config", "marshaled request". Users don't care.
- **Free-form prose reasons**: `Reason` is a dedup key. Use short, stable CamelCase identifiers (e.g. `SSHConnectFailed`, `ScriptTimedOut`, `HostKeyCaptured`), not sentences. Put the details in `Note`.

## Conventions

- **Type**: `event.TypeNormal` for success/informational, `event.TypeWarning` for failure/degraded.
- **Reason**: short, stable CamelCase. Treat it like a machine-readable identifier.
- **Note**: human-readable detail, free-form. Truncate large content (e.g. script stdout) with a clear marker; rows with multi-KB notes are fine but unbounded sizes are not.
- **Don't set `Timestamp`, `EventTime`, `LastObservedTime`, `Count`, or `ReportingController`**; the recorder handles those.

# Dependency Version Management

## CRITICAL: Always Check for Latest Versions
- **NEVER assume first search result is latest**, always verify you have the most recent version
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
3. **Show version comparison**: Display "Current: vX.Y.Z -> Latest: vA.B.C"
4. **Recommend updates**: Suggest updating to latest compatible versions
5. **Update and test**: Apply updates before continuing debugging

### Example Response Pattern:
```
Current dependency versions:
- Pulumi CLI: v3.193.0 -> Latest: v3.198.0 (5 versions behind)
- Pulumi SDK: v3.190.0 -> Latest: v3.198.0 (8 versions behind)
- OCI Provider: v3.9.0 -> Latest: v3.9.0 (up to date)

Recommendation: Update Pulumi CLI and SDK first - version mismatches often cause marshaling errors.
```

**Why This Matters**: Many debugging issues (especially marshaling errors, nil pointers, and compatibility problems) are caused by version mismatches between external dependencies. Always rule out version issues before deep-diving into code analysis.
