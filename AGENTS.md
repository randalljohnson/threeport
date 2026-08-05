# AGENTS.md

Orientation and conventions for working in the threeport codebase. `CLAUDE.md` in this
directory imports this file, so tools that discover either name load the same content.

This file carries the big picture and nothing else. Conventions that matter only while
editing one kind of file belong in the contributor docs under `docs/dev/`, where
`docs/dev/style-guide.md` holds the prose and naming rules. Splitting them further into
per-path rule files that load only for the files they govern is planned and has not
landed, so read the relevant page under `docs/dev/` instead.

The documentation map below says which directory covers which topic. A change to
behavior the documentation describes updates that documentation in the same change.

@docs/agents-index.md

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
| `LogStorageDefinition` | `LogStorageInstance` | log |

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
| ThreeportWorkload CRD instance | `kubernetes-workload-instance-{ID}` or `helm-workload-instance-{ID}` | `internal/agent/agent.go` |

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

These are defaults, not guarantees. `AddLabels()` writes each key only when the resource does not already carry it, so a manifest that sets `app.kubernetes.io/name` or `app.kubernetes.io/instance` itself keeps its own value and the table's `app.kubernetes.io/*` rows will not match. On the kinds that manage pods (`Deployment`, `StatefulSet`, `DaemonSet`, `ReplicaSet`, `Job`) the `control-plane.threeport.io/*` labels are written into the pod template unconditionally, so query by those when you need to find what Threeport is managing.

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

The SDK emits two kinds of file and the difference decides how you change each one, so the code uses a distinct word for each in its own header. Use the same two words in prose and in commit subjects.

**Boilerplate** is rewritten on every `threeport-sdk gen`. **Scaffolding** is written once and then belongs to you like any other source; a later regeneration skips it entirely.

**Never manually edit boilerplate.** Any change to it is silently overwritten on the next generate.

To change what boilerplate contains:

1. Find the corresponding generator in `pkg/sdk/v0/gen/` (handler generation is in `pkg/sdk/v0/gen/pkg/api-server/handlers.go`)
2. Modify the generator code (jennifer/jen Go code generation)
3. Rebuild the SDK: `mage install:sdk`
4. Regenerate: `threeport-sdk gen -c sdk-config.yaml`
5. Verify the generated output reflects your changes

To change the shape a scaffolding file is *born* with, edit the same generator, then delete the existing file before regenerating. A plain regenerate leaves it untouched, which reads as the generator change having done nothing.

## Boilerplate, Scaffolding, and Hand-Written

**Boilerplate, do not edit** (`_gen.go` suffix, header `// boilerplate generated by 'threeport-sdk gen' - do not edit`):

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

**Scaffolding, yours to modify** (no `_gen` suffix, header `// scaffolding generated by 'threeport-sdk gen' but will not be regenerated - intended for modification`):

- `internal/*/v0_*.go`: reconciliation business logic
- `pkg/api/v0/*_validate.go`: per-type validation
- `cmd/tptctl/cmd/*.go`: CLI command definitions

**Hand-written** (no generation header):

- `pkg/api/v0/*.go` without `_gen`: API struct definitions
- `pkg/controller/v0/*.go`: controller framework
- `pkg/notifications/v0/*.go`: notification types
- `pkg/kube/v0/*.go`: Kubernetes helpers
- `cmd/tptdev/cmd/*.go`: developer tool

## Hand-Written Mage Targets Alongside Generated Ones

`magefiles/` holds a hand-written `magefile.go` next to the generated `magefile_gen.go`.
That is the supported arrangement, not a conflict with the generator. Mage compiles every
Go file in the directory into a single binary, so targets from both files land in one
list: `mage -l` shows `build:sdk`, `install:sdk`, and `test:e2e` from the hand-written
file beside `test:unit`, `test:integration`, and `build:apiBin` from the generated one.

The generated file owns the namespace declarations (`Build`, `Test`, `Install`, `Dev`,
`Package`, `Download`). `magefile.go` declares no types of its own; it hangs additional
methods off those same namespaces. Add a target by writing it in `magefile.go`. A
regenerate rewrites `magefile_gen.go` and leaves `magefile.go` untouched, so the target
survives. The one constraint is naming: two methods with the same name on the same
namespace type will not compile, so a hand-written target cannot reuse a name the
generator emits.

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

**Verbs:** `get`, `create`, `delete`, `replace`

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

# CORRECT: name the credential files instead of the credentials themselves
cat <<EOF | tptctl create aws-provider --stdin
AwsProvider:
  Name: my-aws
  AccountID: "555555555555"
  DefaultProvider: true
  LocalConfig: /path/to/.aws/config
  LocalCredentials: /path/to/.aws/credentials
  LocalProfile: default
EOF

# WRONG: don't write config files
tptctl create kubernetes-workload --config /tmp/workload.yaml
```

## Credential Safety

**NEVER** read private keys, credentials, or secrets directly with the Read tool.

**Preferred: hand tptctl the paths and let it do the reading.** The AWS provider config takes `LocalConfig`, `LocalCredentials`, and `LocalProfile` in place of the credentials themselves. tptctl reads the named profile out of those files as it builds the create request, and the API server stores `AccessKeyID` and `SecretAccessKey` encrypted at rest. The secret never lands in the command, the shell history, or the transcript. A config supplies either those three fields or the explicit trio of `DefaultRegion`, `AccessKeyID`, and `SecretAccessKey`, never both.

```bash
# CORRECT: name the credential files instead of the credentials themselves
cat <<EOF | tptctl create aws-provider --stdin
AwsProvider:
  Name: my-aws
  AccountID: "555555555555"
  DefaultProvider: true
  LocalConfig: /path/to/.aws/config
  LocalCredentials: /path/to/.aws/credentials
  LocalProfile: default
EOF
```

**Fallback: a shell variable.** When the credential does not live in an AWS config file, load it into a shell variable first and reference the variable in the heredoc:

```bash
# CORRECT: load credential into variable, reference in heredoc
AWS_KEY=$(cat ~/.aws/secret-key)
cat <<EOF | tptctl create aws-provider --stdin
AwsProvider:
  Name: my-aws
  AccountID: "555555555555"
  DefaultRegion: us-east-1
  AccessKeyID: $AWS_ACCESS_KEY_ID
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
| REST API verbose logging | Off | `-verbose=true` |

**Functions involved:** `getImagePullPolicy()` and `getAPIArgs()` in `components.go` are
the two that change what gets deployed. `getControllerArgs()` and `getAgentArgs()` also
branch on `cpi.Opts.Debug`, but each branch builds the same argument list, so neither
changes a controller or agent deployment.

Debug mode does not put a debugger in the cluster. No component image carries `dlv`, and
`getCommand()` returns the plain binary path whether or not debug mode is on. To step
through code with delve, run it locally against a port-forwarded control plane with the
`dev-debug-*` Makefile targets.

## Commands

```bash
# Enable debug mode for all components (ImagePullPolicy=Always, verbose API server logging)
tptdev debug

# Debug specific components only
tptdev debug --names rest-api,kubernetes-workload-controller

# Enable verbose logging
tptdev debug --verbose

# Disable debug mode (ImagePullPolicy back to IfNotPresent, API server logging back to quiet)
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
- Always pass the control plane name: `-n <name>` on `tptdev up` and `tptdev down`, where
  `-n` is short for `--name`. On `tptdev debug`, `-n` is short for `--names`, the
  comma-delimited list of components to update; that command spells the control plane name
  `-c, --control-plane-name`.
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
- For workload cluster testing, set `WorkerNodeInitialCount` to 0. The field lives on
  `OciOkeKubernetesRuntimeDefinition`, not on the instance, so it is set on the definition
  half of the config.
- Genesis control planes can be left running (free tier)

# Dockerfile Patterns

One `Dockerfile` at the repository root builds every component. It is scaffolding, so the SDK writes it once and a later generate leaves it alone; the template lives at `pkg/sdk/v0/gen/root/dockerfile.go`.

It is a multi-target build rather than one file per component. The `release` target puts a single binary into `gcr.io/distroless/static:nonroot` and runs as `USER 65532:65532`; which binary is chosen by the `BINARY` build argument. Three targets extend `release` for the components that need an extra tool at runtime: `release-terraform`, `release-helm`, and `release-pulumi`, each copying from its own Alpine stage. Version pins for those tools are build arguments in the same file.

Build with `tptdev build --names <component> --parallel 2 --push -t <tag> -r <repo>`, which selects the target for you.
