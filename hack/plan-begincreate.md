# Plan: emit `BeginCreate` on every owner resource

Pending review. Nothing has been edited yet.

## Owner resource definition (grounded in reconciler code)

An **owner resource** is a reconciler that creates one or more threeport child objects and waits for their `Reconciled=true` before setting its own. Non-owners either do infrastructure side-effects (SSH, Pulumi) or have no reconciler (married-non-reconcilable).

Chose `owner` over `composite` to avoid confusion with crossplane's `Composite Resource` (XR). Matches threeport's existing `relationship:owns` tag vocabulary and Kubernetes' owner-reference pattern.

## Owner audit table (canonical reference)

Every object in the RouterMachineSet stack. Columns:
- **(a) BeginCreate**: does this reconciler emit a `BeginCreate` lifecycle event at the start of a reconcile pass?
- **(b) Owns reconcilable children**: does it create at least one threeport child that has its own reconciler and a `Reconciled` flag?
- **(c) Waits for children**: does it block `Reconciled=true` on itself until every owned reconcilable child reports `Reconciled=true`?

Invariant: **a and b must match**. c must be ✓ whenever b is ✓ (else the parent lies about being ready).

| # | KIND | API GROUP | Children created | (a) BeginCreate | (b) Owns reconcilable | (c) Waits for children |
|---|---|---|---|:-:|:-:|:-:|
| 1 | RouterMachineSet | sxalable.io | RouterMachineDefinition + N × RouterMachineInstance | ✓ | ✓ | ✓ |
| 2 | RouterMachineDefinition | sxalable.io | MachineDefinition + RouterDefinition | ✓ | ✓ | ✓ |
| 3 | RouterMachineInstance | sxalable.io | MachineInstance + RouterInstance | ✓ | ✓ | ✓ |
| 4 | MachineDefinition | sxalable.io | MachineRuntimeDefinition (fork) | ✓ | ✓ | ✓ |
| 5 | RouterDefinition | sxalable.io | MachineWorkloadDefinition (fork, non-reconcilable) | ✗ | ✗ | N/A |
| 6 | MachineInstance | sxalable.io | MachineRuntimeInstance (fork) | ✓ | ✓ | ✓ |
| 7 | RouterInstance | sxalable.io | MachineWorkloadInstance (fork) | ✓ | ✓ | ✓ |
| 8 | MachineRuntimeDefinition | threeport.io | GcpGceMachineRuntimeDefinition (married, non-reconcilable) | ✗ | ✗ | N/A |
| 9 | MachineRuntimeInstance | threeport.io | GcpGceMachineRuntimeInstance (via wiring) | ✓ | ✓ | ✓ (via SSH probe on hostname) |
| 10 | MachineWorkloadDefinition | threeport.io | none (leaf, no reconciler) | ✗ | ✗ | N/A |
| 11 | MachineWorkloadInstance | threeport.io | none (leaf; SSH + install script) | ✗ | ✗ | N/A |
| 12 | GcpGceMachineRuntimeDefinition | threeport.io | none (non-reconcilable) | ✗ | ✗ | N/A |
| 13 | GcpGceMachineRuntimeInstance | threeport.io | none (leaf; provisions VM via Pulumi) | ✗ | ✗ | N/A |

Rows 1-4, 6-7, 9 = 7 owners that emit BeginCreate. Rows 5, 8 = 2 owners of non-reconcilable children (no BeginCreate). Rows 10-13 = 4 non-owners.

**Every SuccessfulCreate event** fires on rows 1-9 (all owners, regardless of a/b/c). BeginCreate fires only where (a) is ✓.

## Sequence diagram, `SET_COUNT=1 phase3`, top-down

```
User: tptctl sxalable create router-machine-set (replicas=1)

  RouterMachineSet [demo-router-set]                       BeginCreate
    ├─ RouterMachineDefinition [demo-router-set]           BeginCreate
    │    ├─ MachineDefinition                              BeginCreate
    │    │    └─ MachineRuntimeDefinition (fork)           BeginCreate
    │    │         └─ GcpGceMachineRuntimeDefinition       (non-reconcilable)
    │    │           SuccessfulCreate on MachineRuntimeDefinition
    │    │         SuccessfulCreate on MachineDefinition
    │    └─ RouterDefinition                               BeginCreate
    │         └─ MachineWorkloadDefinition (fork)          (leaf; SuccessfulCreate)
    │           SuccessfulCreate on RouterDefinition
    │       SuccessfulCreate on RouterMachineDefinition
    │
    └─ RouterMachineInstance [demo-router-set-0]           BeginCreate
         ├─ MachineInstance                                BeginCreate
         │    └─ MachineRuntimeInstance (fork)             BeginCreate
         │         └─ GcpGceMachineRuntimeInstance         (leaf; Pulumi up ~30s)
         │           SuccessfulCreate on gcp-gce-*
         │         [waits for hostname, SSH connects]
         │         SuccessfulCreate on MachineRuntimeInstance
         │       SuccessfulCreate on MachineInstance
         └─ RouterInstance                                 BeginCreate
              └─ MachineWorkloadInstance (fork)            (leaf; SSH + script)
                [outcome depends on registry token; see two paths below]
```

## Expected events table — sad path (registry token expired → ScriptFailed)

Newest first. `BeginCreate` rows are the owner lifecycle markers. `SuccessfulCreate` on `router-*` never fires because the workload script fails.

| # | TYPE | API GROUP | KIND | NAME | REASON | COUNT | AGE |
|---|---|---|---|---|---|---|---|
| 1 | Normal | threeport.io | machine-runtime-instance | demo-router-set-0 | SuccessfulCreate | 1 | 2m30s |
| 2 | Normal | threeport.io | gcp-gce-machine-runtime-instance | demo-router-set-0 | SuccessfulCreate | 1 | 2m40s |
| 3 | Warning | threeport.io | machine-workload-instance | demo-router-set-0 | ScriptFailed | 6 | 3m..30s |
| 4 | Normal | threeport.io | machine-runtime-instance | demo-router-set-0 | BeginCreate | 1 | 3m20s |
| 5 | Normal | sxalable.io | router-instance | demo-router-set-0 | BeginCreate | 1 | 3m30s |
| 6 | Normal | sxalable.io | machine-instance | demo-router-set-0 | BeginCreate | 1 | 3m30s |
| 7 | Normal | sxalable.io | router-machine-instance | demo-router-set-0 | BeginCreate | 1 | 3m40s |
| 8 | Normal | threeport.io | machine-workload-definition | demo-router-set | SuccessfulCreate | 1 | 3m45s |
| 9 | Normal | sxalable.io | router-definition | demo-router-set | SuccessfulCreate | 1 | 3m45s |
| 10 | Normal | threeport.io | machine-runtime-definition | demo-router-set | SuccessfulCreate | 1 | 3m50s |
| 11 | Normal | sxalable.io | machine-definition | demo-router-set | SuccessfulCreate | 1 | 3m50s |
| 12 | Normal | sxalable.io | router-machine-definition | demo-router-set | SuccessfulCreate | 1 | 3m55s |
| 13 | Normal | threeport.io | machine-runtime-definition | demo-router-set | BeginCreate | 1 | 3m55s |
| 14 | Normal | sxalable.io | router-definition | demo-router-set | BeginCreate | 1 | 3m56s |
| 15 | Normal | sxalable.io | machine-definition | demo-router-set | BeginCreate | 1 | 3m56s |
| 16 | Normal | sxalable.io | router-machine-definition | demo-router-set | BeginCreate | 1 | 3m58s |
| 17 | Normal | sxalable.io | router-machine-set | demo-router-set | **BeginCreate** | 1 | **4m** |

Blocked owners with no SuccessfulCreate: `RouterMachineSet`, `RouterMachineInstance`, `RouterInstance`.

## Expected events table — happy path (registry token valid → all reconciled)

Same shape, workload script succeeds, all owners emit `SuccessfulCreate` in bottom-up order.

| # | TYPE | API GROUP | KIND | NAME | REASON | COUNT | AGE |
|---|---|---|---|---|---|---|---|
| 1 | Normal | sxalable.io | router-machine-set | demo-router-set | **SuccessfulCreate** | 1 | 30s |
| 2 | Normal | sxalable.io | router-machine-instance | demo-router-set-0 | SuccessfulCreate | 1 | 35s |
| 3 | Normal | sxalable.io | machine-instance | demo-router-set-0 | SuccessfulCreate | 1 | 40s |
| 4 | Normal | sxalable.io | router-instance | demo-router-set-0 | SuccessfulCreate | 1 | 40s |
| 5 | Normal | threeport.io | machine-workload-instance | demo-router-set-0 | SuccessfulCreate | 1 | 50s |
| 6 | Normal | threeport.io | machine-runtime-instance | demo-router-set-0 | SuccessfulCreate | 1 | 1m10s |
| 7 | Normal | threeport.io | gcp-gce-machine-runtime-instance | demo-router-set-0 | SuccessfulCreate | 1 | 1m40s |
| 8 | Normal | threeport.io | machine-runtime-instance | demo-router-set-0 | BeginCreate | 1 | 2m30s |
| 9 | Normal | sxalable.io | router-instance | demo-router-set-0 | BeginCreate | 1 | 2m40s |
| 10 | Normal | sxalable.io | machine-instance | demo-router-set-0 | BeginCreate | 1 | 2m40s |
| 11 | Normal | sxalable.io | router-machine-instance | demo-router-set-0 | BeginCreate | 1 | 2m50s |
| 12 | Normal | threeport.io | machine-workload-definition | demo-router-set | SuccessfulCreate | 1 | 2m55s |
| 13 | Normal | sxalable.io | router-definition | demo-router-set | SuccessfulCreate | 1 | 2m55s |
| 14 | Normal | threeport.io | machine-runtime-definition | demo-router-set | SuccessfulCreate | 1 | 3m |
| 15 | Normal | sxalable.io | machine-definition | demo-router-set | SuccessfulCreate | 1 | 3m |
| 16 | Normal | sxalable.io | router-machine-definition | demo-router-set | SuccessfulCreate | 1 | 3m5s |
| 17 | Normal | threeport.io | machine-runtime-definition | demo-router-set | BeginCreate | 1 | 3m5s |
| 18 | Normal | sxalable.io | router-definition | demo-router-set | BeginCreate | 1 | 3m6s |
| 19 | Normal | sxalable.io | machine-definition | demo-router-set | BeginCreate | 1 | 3m6s |
| 20 | Normal | sxalable.io | router-machine-definition | demo-router-set | BeginCreate | 1 | 3m8s |
| 21 | Normal | sxalable.io | router-machine-set | demo-router-set | **BeginCreate** | 1 | **3m10s** |

Row 21 (bottom): owner root emits `BeginCreate` at t=0. Row 1 (top): same root emits `SuccessfulCreate` once every child reports Reconciled. Total wall-clock ~3 minutes for 1 replica.

## Files to edit

### Fork (threeport-randalljohnson/dev)

1. `pkg/event/v0/event.go` — add reason constants:
   ```go
   ReasonBeginCreate = "BeginCreate"
   ReasonBeginUpdate = "BeginUpdate"
   ReasonBeginDelete = "BeginDelete"
   ```
2. `internal/machine-runtime/v0_machine_runtime_definition.go` — emit `BeginCreate` at top of `v0MachineRuntimeDefinitionCreated`
3. `internal/machine-runtime/v0_machine_runtime_instance.go` — emit `BeginCreate` at top of `v0MachineRuntimeInstanceCreated` (before the `reconcileProviderInstance` guard)

### Module (sxalable-threeport/dev)

4. `internal/router/v0_router_machine_set.go` — emit `BeginCreate` at top of `v0RouterMachineSetCreated`
5. `internal/router/v0_router_machine_definition.go` — emit at top of `v0RouterMachineDefinitionCreated`
6. `internal/router/v0_router_machine_instance.go` — emit at top of `v0RouterMachineInstanceCreated`
7. `internal/router/v0_machine_definition.go` — emit at top of `v0MachineDefinitionCreated`
8. `internal/router/v0_machine_instance.go` — emit at top of `v0MachineInstanceCreated`
9. `internal/router/v0_router_definition.go` — emit at top of `v0RouterDefinitionCreated`
10. `internal/router/v0_router_instance.go` — emit at top of `v0RouterInstanceCreated`

Each emission guarded by an "already reconciled?" early-exit so requeues don't spam. Dedup handles the case where guard is missed.

## Commands to run (after edits land)

### Rebuild + push fork images (always the first step)

Local kind pulls from `localhost:5001`. `tptctl up` needs those images to already exist in the registry, otherwise pods sit in ImagePullBackOff.

```bash
cd /home/ubuntu/worktrees/threeport-randalljohnson/dev
mage build:allImages
```

```bash
mage install:tptctl
```

### Bring up local control plane (if needed)

```bash
tptctl up --control-plane-only \
    --name 1 \
    --cluster-name kind-threeport-1 \
    -r localhost:5001 \
    --debug
```

`-r localhost:5001` is required on local kind so pods pull from the local registry the fork images are pushed to; without it, tptctl defaults to dockerhub and pods hang in ImagePullBackOff.

### Roll fork controllers if cluster was already up

Skip this if `tptctl up` just ran (fresh pods pulled the new images). If iterating against an existing cluster, roll the fork pods to pick up the new images:

```bash
tptdev build --push --restart -r localhost:5001 -t v0.7.0-dev --parallel 8
```

### Regen module code + rebuild + push (per rebuild-together memory rule)

Rebuild the fork's `threeport-sdk` binary before running module codegen so the module's generated code reflects any fork-side SDK changes.

Fork: rebuild `threeport-sdk`:

```bash
cd /home/ubuntu/worktrees/threeport-randalljohnson/dev
mage install:sdk
```

Module: regenerate code:

```bash
cd /home/ubuntu/worktrees/sxalable-threeport/dev
threeport-sdk gen -c sdk-config.yaml
```

Module: rebuild + push images:

```bash
mage build:allImages
```

Module: install (idempotent, safe to rerun even if already installed):

```bash
tptctl sxalable install -r localhost:5001 -t v0.1.0-dev --debug
```

### Wait for both rollouts

```bash
kubectl -n threeport-control-plane rollout status deploy/threeport-api-server --timeout=90s
kubectl -n threeport-sxalable rollout status deploy/threeport-sxalable-router-controller --timeout=90s
```

### Run the demo (1 replica)

Build once:

```bash
cd /home/ubuntu/worktrees/sxalable-threeport/dev/hack/demo-runbook
go build -o /tmp/demo-runbook .
```

Run:

```bash
REG_TOKEN_FILE=/home/ubuntu/.sxalable/registry-token \
RUNTIME_KEY_FILE=/home/ubuntu/.sxalable/runtime-key \
SET_COUNT=1 \
/tmp/demo-runbook --phase3
```

The two `*_FILE` env vars are required; without them the runbook errors out with `!! missing required env vars: REG_TOKEN_FILE, RUNTIME_KEY_FILE`. They point at the local files the sxalable install script reads on the target VM.

### Watch events

```bash
tptctl get events --since 10m
```
