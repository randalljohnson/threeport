# Threeport Codebase Skill

## Architecture Overview

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

### Request Flow

1. Client sends REST request to API server
2. API server persists object to CockroachDB
3. If `Reconciled == false`, API publishes a `Notification` to the appropriate NATS subject
4. Controller pulls the message, acquires a lock (NATS KV), fetches latest object from API
5. Controller runs reconciliation logic, updates object status via API, releases lock, ACKs message

## Definition/Instance Pattern

Every managed resource follows a two-level abstraction:

- **Definition** — declares _what_ to deploy (template/config). Embeds `Common`, `Definition`, `Reconciliation`.
- **Instance** — declares _where_ to deploy it (binds a definition to a runtime). Embeds `Common`, `Instance`, `Reconciliation`.

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

### All Definition/Instance Pairs

| Definition | Instance | Domain |
|---|---|---|
| `WorkloadDefinition` | `WorkloadInstance` | workload |
| `HelmWorkloadDefinition` | `HelmWorkloadInstance` | helm-workload |
| `KubernetesRuntimeDefinition` | `KubernetesRuntimeInstance` | kubernetes-runtime |
| `AwsEksKubernetesRuntimeDefinition` | `AwsEksKubernetesRuntimeInstance` | aws |
| `OciOkeKubernetesRuntimeDefinition` | `OciOkeKubernetesRuntimeInstance` | oci |
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

API object structs are in `pkg/api/v0/*.go`. The SDK config `sdk-config.yaml` declares which objects are `Reconcilable: true`.

## NATS Streams, Subjects, Locks

### Streams

Each controller domain has one JetStream stream. Streams are created in `cmd/rest-api/util/controller_stream_gen.go` and named in `internal/*/notif/notif_gen.go`.

| Stream | Subjects (object types) |
|---|---|
| `workloadStream` | `workloadDefinition.*`, `workloadInstance.*` |
| `helmWorkloadStream` | `helmWorkloadDefinition.*`, `helmWorkloadInstance.*` |
| `kubernetesRuntimeStream` | `kubernetesRuntimeDefinition.*`, `kubernetesRuntimeInstance.*` |
| `awsStream` | `awsEksKubernetesRuntimeInstance.*` |
| `ociStream` | `ociOkeKubernetesRuntimeInstance.*` |
| `gatewayStream` | `gatewayDefinition.*`, `gatewayInstance.*`, `domainNameInstance.*` |
| `secretStream` | `secretDefinition.*`, `secretInstance.*` |
| `terraformStream` | `terraformDefinition.*`, `terraformInstance.*` |
| `controlPlaneStream` | `controlPlaneDefinition.*`, `controlPlaneInstance.*` |
| `observabilityStream` | `observabilityStack{Definition,Instance}.*`, `observabilityDashboard{Definition,Instance}.*`, `metrics{Definition,Instance}.*`, `logging{Definition,Instance}.*` |

### Subject Pattern

Format: `{camelCaseObjectType}.{operation}` where operation is `create`, `update`, or `delete`.

Examples: `workloadInstance.create`, `helmWorkloadDefinition.update`, `secretInstance.delete`

Controllers subscribe to wildcard: `{camelCaseObjectType}.*`

### Lock Buckets

Each domain has a NATS KV bucket for distributed locks (`internal/*/<domain>_gen.go`):

| Bucket | Domain |
|---|---|
| `workloadLock` | workload |
| `helmWorkloadLock` | helm-workload |
| `kubernetesRuntimeLock` | kubernetes-runtime |
| `awsLock` | aws |
| `ociLock` | oci |
| `gatewayLock` | gateway |
| `secretLock` | secret |
| `terraformLock` | terraform |
| `controlPlaneLock` | control-plane |
| `observabilityLock` | observability |

**Lock TTL:** 20 minutes (set in each controller's `main_gen.go`).

### Lock Key Format

From `pkg/controller/v0/reconcile.go:174`:

```
{ReconcilerName}.{ObjectID}
```

Example: `WorkloadInstanceReconciler.42`

The value stored is the controller's UUID. Consumer names follow: `{ReconcilerName}Consumer`.

## Reconciliation Fields and Lifecycle

### Reconciliation Struct (`pkg/api/v0/common.go:17-52`)

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

### Create Lifecycle

1. `POST /v0/<objects>` — API creates object with `Reconciled=false`, publishes `<object>.create`
2. Controller pulls message, acquires lock, fetches latest from API
3. Calls hand-written `v0<Object>Created()` in `internal/<domain>/v0_<object>.go`
4. On success: sets `Reconciled=true` via API update, releases lock, ACKs message
5. On failure: releases lock, requeues with exponential backoff (1s initial, 30s max)

### Update Lifecycle

1. `PATCH /v0/<objects>/:id` — API updates object, sets `Reconciled=false`, publishes `<object>.update`
2. Same lock/reconcile/release flow as create
3. Calls hand-written `v0<Object>Updated()`

### Delete Lifecycle (Two-Phase)

1. First `DELETE /v0/<objects>/:id` — API sets `DeletionScheduled=now()`, `Reconciled=false`, publishes `<object>.delete`
2. Subsequent DELETE while reconciling returns **409 Conflict**
3. Controller calls `v0<Object>Deleted()`, performs cleanup
4. Controller sets `DeletionAcknowledged`, `DeletionConfirmed`, `Reconciled=true`
5. Controller calls DELETE again — API sees `DeletionConfirmed` is set and removes row from DB

### Requeue Backoff (`pkg/controller/v0/requeue.go`)

- Initial delay: 1 second
- Maximum delay: 30 seconds
- Doubles based on elapsed time since notification `CreationTime`
- Uses NATS `NakWithDelay` to requeue messages

## Kubernetes Mapping

### Namespace Conventions

| Threeport Construct | Kubernetes Namespace | Source |
|---|---|---|
| Control plane components | `threeport-control-plane` | `pkg/threeport-installer/v0/threeport.go:64` |
| Gateway resources | `nukleros-gateway-system` | `pkg/util/v0/constants.go:17` |
| WorkloadInstance | `{name}-{10alphanumChars}` | `pkg/kube/v0/namespace.go:47` |
| HelmWorkloadInstance | `{name}-{10alphaChars}` (or user-specified `ReleaseNamespace`) | `internal/helm-workload/v0_helm_workload_instance.go:93` |

### Naming Patterns

| Resource | Name Pattern | Source |
|---|---|---|
| Helm release | `{instanceName}-release` | `internal/helm-workload/v0_helm_workload_instance.go:476` |
| ThreeportWorkload CRD instance | `workload-instance-{ID}` or `helm-workload-instance-{ID}` | `internal/agent/agent.go:30-32` |

### ThreeportWorkload CRD

- Cluster-scoped custom resource
- API group: `control-plane.threeport.io`
- Version: `v1alpha1`
- Source: `pkg/agent/api/v1alpha1/groupversion_info.go:29`, `threeportworkload_types.go`

### Labels Applied to Managed Resources

From `pkg/kube/v0/metadata.go:24-29` and `internal/agent/agent.go:18-19`:

| Label | Value | Applied To |
|---|---|---|
| `app.kubernetes.io/managed-by` | `threeport` | All managed resources |
| `app.kubernetes.io/name` | `{definitionName}` | All managed resources |
| `app.kubernetes.io/instance` | `{instanceName}` | All managed resources |
| `control-plane.threeport.io/managed-by` | `threeport` | All managed resources + namespaces |
| `control-plane.threeport.io/workload-instance` | `{ID}` | Workload resources |
| `control-plane.threeport.io/helm-workload-instance` | `{ID}` | Helm workload resources |

### kubectl Examples

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
kubectl logs deploy/threeport-workload-controller -n threeport-control-plane
kubectl logs deploy/threeport-helm-workload-controller -n threeport-control-plane
kubectl logs deploy/threeport-kubernetes-runtime-controller -n threeport-control-plane
```

## Debugging Decision Trees

### Workload Not Deploying

1. Check reconciliation status: `tptctl get workload-instances -o json` — look at `Reconciled`, `CreationFailed`, `InterruptReconciliation`
2. If `Reconciled=false` and no `CreationFailed`: controller may be processing or stuck
   - Check controller logs: `kubectl logs deploy/threeport-workload-controller -n threeport-control-plane`
   - Check for lock stuck in NATS KV (lock key: `WorkloadInstanceReconciler.{ID}`, TTL: 20 min)
3. If `CreationFailed=true`: check controller logs for error, fix issue, update object to retry
4. Check Kubernetes resources: `kubectl get all -n {workloadName}-{suffix}`
5. Check events: `kubectl get events -n {workloadName}-{suffix} --sort-by=.lastTimestamp`

### Workload Not Deleting

1. Check deletion status: `tptctl get workload-instances -o json` — look at `DeletionScheduled`, `DeletionAcknowledged`, `DeletionConfirmed`
2. If `DeletionScheduled` set but no `DeletionAcknowledged`: controller hasn't started cleanup
   - Check controller logs for errors
3. If `DeletionAcknowledged` set but no `DeletionConfirmed`: cleanup is in progress or stuck
   - Check Kubernetes for remaining resources: `kubectl get all -l control-plane.threeport.io/workload-instance={ID}`
4. Call DELETE again once `DeletionConfirmed` is set to remove from database

### Controller Not Processing Messages

1. Verify controller is running: `kubectl get pods -n threeport-control-plane`
2. Verify NATS connectivity: `kubectl logs deploy/threeport-{domain}-controller -n threeport-control-plane | grep -i nats`
3. Check for stuck locks: lock TTL is 20 minutes, stale locks auto-expire
4. Purge NATS streams if needed: `make dev-purge-streams`
5. Subscribe to all NATS messages for debugging: `make dev-sub-nats`

### Tearing Down a Control Plane — CRITICAL

**NEVER** delete a genesis control plane or run `tptdev down` / `tptctl down` while cloud provider resources are still deployed. This is especially easy to forget with kind clusters — deleting the kind cluster orphans cloud resources (EKS clusters, OKE clusters, VPCs, etc.) that will continue incurring costs and must be manually cleaned up.

**Always clean up in this order:**

1. Delete all workload instances and helm workload instances
2. Delete all kubernetes runtime instances (EKS, OKE, etc.) — wait for cloud resources to be fully deprovisioned
3. Delete all other managed instances (secrets, terraform, observability, etc.)
4. Verify no cloud resources remain: check AWS/OCI console
5. Only then: `tptdev down` or `tptctl down`

```bash
# Check what's still deployed before tearing down
tptctl get workload-instances -o json
tptctl get helm-workload-instances -o json
tptctl get kubernetes-runtime-instances -o json
tptctl get aws-eks-kubernetes-runtime-instances -o json
tptctl get oci-oke-kubernetes-runtime-instances -o json
tptctl get terraform-instances -o json
```

### Cloud Resource Modification — Use Threeport First

**ALWAYS** create, modify, and delete cloud resources (Kubernetes clusters, VPCs, load balancers, DNS records, etc.) through threeport's API and CLI. Threeport tracks the state of all managed resources in its database and reconciles them via controllers. Modifying resources directly through cloud-provider CLIs or MCP tools (e.g., `aws`, `oci`, `gcloud`, `kubectl apply`) will cause state drift — threeport won't know about the change and may overwrite it, fail to clean it up, or behave unpredictably.

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

## tptctl Command Reference

### Syntax

```
tptctl {verb} {object-type} [flags]
```

**Verbs:** `get`, `create`, `delete`, `replace`, `describe`

**Common flags:**
- `-n, --name` — Object name
- `-c, --config` — Path to YAML config file
- `--stdin` — Read config from stdin instead of file
- `-v, --version` — API version (default `v0`)
- `-o, --output` — Output format: `tabular` (default), `yaml`, `json`
- `-i, --control-plane-name` — Target control plane name

**Other commands:** `up`, `down`, `config`, `upgrade`, `version`

### Always Use `--stdin` for Create/Replace

**ALWAYS** pipe config into tptctl using `--stdin` instead of writing temporary files. This keeps commands readable for users following along:

```bash
# CORRECT: pipe config into --stdin
cat <<EOF | tptctl create workload --stdin
Workload:
  Name: my-app
  YAMLDocument: path/to/manifest.yaml
  WorkloadInstance:
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
tptctl create workload --config /tmp/workload.yaml
```

### Credential Safety

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

### Examples

```bash
# List all workload instances as JSON
tptctl get workload-instances -o json

# Get a specific workload definition as YAML
tptctl get workload-definitions --name my-def -o yaml

# List all helm workload instances
tptctl get helm-workload-instances -o json

# List all kubernetes runtime instances
tptctl get kubernetes-runtime-instances -o json

# Create a workload via stdin
cat <<EOF | tptctl create workload --stdin
Workload:
  Name: my-app
  YAMLDocument: path/to/manifest.yaml
  WorkloadInstance:
    Name: my-app
EOF

# Delete a workload instance by name
tptctl delete workload-instance --name my-instance

# Replace (update) a workload definition
cat <<EOF | tptctl replace workload-definition --stdin --name existing-def
WorkloadDefinition:
  Name: existing-def
  YAMLDocument: path/to/new-manifest.yaml
EOF
```

## tptdev Debug Workflow

### Development Setup

Always use the kind provider with debug mode for local development:

```bash
# Spin up a dev environment with kind
tptdev up

# Enable debug mode — ALWAYS do this after `tptdev up`
tptdev debug
```

**Why debug mode matters:** `tptdev debug` switches `ImagePullPolicy` to `Always` for all threeport components. Without this, Kubernetes may use cached images instead of freshly built ones, causing confusion where code changes appear to have no effect. Always enable debug mode to avoid stale image issues.

### What Debug Mode Changes

Debug mode is implemented across several functions in `pkg/threeport-installer/v0/components.go`:

| Aspect | Default | Debug Enabled |
|---|---|---|
| Image pull policy | `IfNotPresent` (cached) | `Always` (always pull fresh) |
| REST API verbose logging | Off | `--verbose=true` |

**Functions involved:** `getImagePullPolicy()`, `getRestApiArgs()`, `getControllerArgs()`, `getAgentArgs()`, `getCommand()` — all in `components.go`.

### Commands

```bash
# Enable debug mode for all components (sets debug images, enables delve, ImagePullPolicy=Always)
tptdev debug

# Debug specific components only
tptdev debug --names rest-api,workload-controller

# Enable live-reload (via air) for specific components
tptdev debug --names rest-api --live-reload

# Enable verbose logging
tptdev debug --verbose

# Disable debug mode (reverts ImagePullPolicy, removes debug images)
tptdev debug --disable

# Build and push images to remote registry
tptdev build --names rest-api,workload-controller --push

# Build, push, and restart pods to pick up new images immediately
tptdev build --names rest-api,workload-controller --push --restart

# Tear down dev environment
tptdev down
```

### tptctl Binary Location

`tptctl` is built by `mage build:tptctl` and placed in `bin/tptctl` relative to the repo root. It is NOT installed globally. To use it, either add the repo's `bin/` directory to `PATH` or reference it by full path:

```bash
export PATH="$(git rev-parse --show-toplevel)/bin:$PATH"
tptctl get workload-instances
```

### Build-Test Cycle

After making code changes:

1. Build and push the changed component: `tptdev build --names <component> --push`
2. Kubernetes pulls the new image automatically (debug mode ensures `ImagePullPolicy=Always`)
3. Check logs: `kubectl logs deploy/threeport-<component> -n threeport-control-plane -f`

### Makefile Targets

| Target | Description |
|---|---|
| `dev-logs-api` | Follow API server logs |
| `dev-logs-wrk` | Follow workload controller logs |
| `dev-logs-gw` | Follow gateway controller logs |
| `dev-logs-kr` | Follow kubernetes runtime controller logs |
| `dev-logs-aws` | Follow AWS controller logs |
| `dev-logs-cp` | Follow control plane controller logs |
| `dev-logs-agent` | Follow agent logs |
| `dev-query-crdb` | Open CockroachDB SQL shell |
| `dev-reset-crdb` | Truncate all working tables in CockroachDB |
| `dev-purge-streams` | Purge all NATS JetStream streams |
| `dev-sub-nats` | Subscribe to all NATS messages for debugging |
| `dev-debug-api` | Start delve session for API server |
| `dev-debug-wrk` | Start delve session for workload controller |
| `dev-debug-gateway` | Start delve session for gateway controller |

## SDK Code Generation Circular Dependency

`mage install:sdk` compiles the entire project because magefiles import project packages. If you rename or change a type in `pkg/api/v0/*.go`, the stale `_gen.go` files reference the old type and won't compile — blocking the SDK binary build needed to regenerate them.

**Never manually edit anything ending in `_gen.go`.** These files are regenerated by `threeport-sdk gen` and manual edits will be silently overwritten.

### Safe Workflow for Type Changes
1. **Build SDK binary FIRST** (before changing source types): `git stash` → `mage install:sdk` → `git stash pop` (or ensure `$GOPATH/bin/threeport-sdk` already exists from a prior build)
2. Edit `sdk-config.yaml` + `pkg/api/v0/*.go` (source of truth files)
3. Run `threeport-sdk gen -c sdk-config.yaml` to regenerate all `_gen.go` files
4. Manually update non-generated files (config, CLI, controller, provider, bootstrap, kube, migrations)
5. Verify: `go vet ./...`

### Post-Commit Check
After every commit touching `pkg/api/v0/*.go` or `sdk-config.yaml`:
```bash
threeport-sdk gen -c sdk-config.yaml
git diff --name-only | grep '_gen.go'
```
If any `_gen.go` files show changes, the commit is out of sync — regenerate before pushing.

## Dockerfile Patterns

### Generation Model
- SDK generates Dockerfiles once (`threeport-sdk gen`), then never overwrites them
- Header: `# generated by 'threeport-sdk gen' but will not be regenerated - can optionally be edited`
- Templates: `pkg/sdk/v0/util/dockerfile.go`, orchestrated by `pkg/sdk/v0/gen/cmd/cmd.go`
- Safe to customize after generation; SDK skips existing files

### Three Variants Per Component (`cmd/<component>/image/`)
- `Dockerfile` — production multi-stage: golang builder → distroless/static:nonroot, builds from source
- `Dockerfile-goreleaser` — CI/CD: copies pre-built binary into distroless
- `Dockerfile-alpine` — development: copies local `bin/<binary>` into golang-alpine

### Development Image (`cmd/tptdev/image/Dockerfile`)
- **NOT generated** — manually maintained, multi-target build
- Targets: `base` (delve + wait-for-tcp), `live-reload` (air), `dev` (standard), `dev-terraform` (+ Terraform binary), `dev-oci` (+ Pulumi CLI)
- `tptdev build` selects target via `--target` flag based on component type
- Build flow: compile Go binary locally → copy into image via target

### Customized Production Dockerfiles
- **terraform-controller** — downloads Terraform binary at build time
- **helm-workload-controller** — creates `/var/helm` cache with non-root permissions
- **agent** — builds from `main.go` not `main_gen.go`

### Build Commands
- Production: `docker buildx` with component's own `Dockerfile`
- Development: `tptdev build --names <component> --parallel 2 --push -t <tag> -r <repo>`
- All production images: non-root (USER 65532:65532), include wait-for-tcp

## Directory Layout

```
threeport/
├── cmd/                           # Binary entry points
│   ├── rest-api/                  # API server [generated main_gen.go]
│   ├── workload-controller/       # [generated main_gen.go]
│   ├── helm-workload-controller/  # [generated main_gen.go]
│   ├── kubernetes-runtime-controller/
│   ├── gateway-controller/
│   ├── aws-controller/
│   ├── oci-controller/
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
│   ├── workload/
│   │   ├── workload_gen.go                          # [generated] lock bucket consts
│   │   ├── notif/notif_gen.go                       # [generated] stream/subject consts
│   │   ├── workload_instance_reconciler_gen.go      # [generated] reconciler loop
│   │   ├── v0_workload_instance.go                  # [hand-written] business logic
│   │   └── v0_workload_definition.go                # [hand-written] business logic
│   ├── helm-workload/             # Same pattern as workload/
│   ├── kubernetes-runtime/
│   ├── gateway/
│   ├── aws/
│   ├── oci/
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
│   │   ├── workload.go            # WorkloadDefinition, WorkloadInstance
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

### Generated vs Hand-Written

**Generated — do not edit** (`_gen.go` suffix, header: `// generated by 'threeport-sdk gen' - do not edit`):
- `cmd/*/main_gen.go` — entry points
- `internal/*/notif/notif_gen.go` — NATS constants
- `internal/*/*_reconciler_gen.go` — reconciler loops
- `internal/*/*_gen.go` — lock bucket constants
- `pkg/api/v0/*_gen.go` — interface method implementations
- `pkg/api-server/v0/handlers/*_gen.go` — REST handlers
- `pkg/api-server/v0/routes/*_gen.go` — route registrations
- `pkg/client/v0/*_gen.go` — API client functions

**Generated once, then hand-modified** (no `_gen` suffix, header: `// generated by 'threeport-sdk gen' but will not be regenerated - intended for modification`):
- `internal/*/v0_*.go` — reconciliation business logic
- `cmd/tptctl/cmd/*.go` — CLI command definitions

**Hand-written** (no generation header):
- `pkg/api/v0/*.go` (without `_gen`) — API struct definitions
- `pkg/controller/v0/*.go` — controller framework
- `pkg/notifications/v0/*.go` — notification types
- `pkg/kube/v0/*.go` — Kubernetes helpers
- `cmd/tptdev/cmd/*.go` — developer tool
