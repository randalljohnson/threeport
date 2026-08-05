# Threeport Controllers

Threeport controllers provide the features and functionality of the threeport
control plane.  They are triggered by the API and store persistent data through
the API.  They work in isolation on the Threeport objects they are responsible
for reconciling.  They can trigger reconcilation in other controllers through
updates to the API of objects, and can be triggered by other controllers - or
user changes - to reconcile the state of objects they're responsible for.

## Controller Fundamentals

When changes are made to an object that requires reconciliation in the system,
a reconciler within a controller is notified by the API through the NATS
Jetstream messaging system.  It then works on a non-terminating loop to bring
about the desired state until that state is achieved.

It uses NATS to create distributed locks on particular objects while reconciling
so no other controller attempts to reconcile the same object simultaneously.  If
it encounters a condition that prevents reconcilation from completing, it
requeues the notification for reconciliation so that it is retried later.  This
requeue uses a backoff mechanism to progressively extend the interval between
reconciliation attempts to provide the best balance of responsiveness and
resource consumption.

### Controllers

A controller is a piece of software that runs as a part of the Threeport control
plane.

Controllers are based on the API data model.  A controller's domain of operation
is scoped by a source file in`pkg/api`.  For example the
kubernetes-runtime-controller is responsible for reconciling objects defined in
`pkg/api/v0/kubernetes_runtime.go`.

### Reconcilers

A controller consists of one or more reconcilers.  Each reconciler is
responsible for reconciling state for a single object.  For example, the
kubernetes-workload-controller has two reconcilers:

* Kubernetes Workload Definition Reconciler:  It is responsible for reconciling state for
  `KubernetesWorkloadDefinition` objects.  It parses the `YAMLDocument` into separate
  Kubernetes resources and stores them each in a distinct
  `KubernetesWorkloadResourceDefinition` object.
* Kubernetes Workload Instance Reconciler:  It is responsible for reconciling state for
  `KubernetesWorkloadInstance` objects.  It takes all the `KubernetesWorkloadResourceDefinitions`
  and installs them in a target Kubernetes cluster.

## Creating a New Controller

The following steps outline creating a new controller.  Examples are used for
the kubernetes-runtime-controller.  Refer to the code for that controller and
its objects for examples.

1. Add an API object group to `sdk-config.yaml`.  The group's `Name` determines
   the names derived from it, so the `kubernetes_runtime` group reads its data
   model from `pkg/api/v0/kubernetes_runtime.go` and produces
   `cmd/kubernetes-runtime-controller` and `internal/kubernetes-runtime`.  List
   every API object the controller works with under `Objects`.
1. Set `Reconcilable: true` on those objects that will require reconciliation.
   See the `KubernetesRuntimeDefinition` and `KubernetesRuntimeInstance`
   entries in `sdk-config.yaml`.
   Note: not all objects necessarily require reconciliation.  Some just store
   data that is referred to when reconciling state for other objects.
1. Create a data model for the objects that will be used and reconciled.
   Example: `pkg/api/v0/kubernetes_runtime.go`.  To start from generated
   scaffolding rather than an empty file, create the API object source files
   from the SDK config.

   ```bash
   threeport-sdk create -c sdk-config.yaml
   ```
1. Embed the `Reconciliation` struct in each object you marked
   `Reconcilable: true`.  It carries the `Reconciled` field and the creation
   and deletion timestamps that the generated code expects.  This is not
   required if no reconciler exists for the object.
1. Build the SDK and run code generation.  Generation writes the controller's
   main package, its reconcilers, its NATS subjects and its lock bucket,
   creating any directories those files need.  It also registers the
   controller's NATS stream with the REST API in
   `cmd/rest-api/util/controller_stream_gen.go`, which is boilerplate and is
   rewritten on every generate, so never edit it by hand.

   ```bash
   mage install:sdk
   mage dev:generate
   ```
1. You will find a new file in `internal/kubernetes-runtime` for each
   reconciled object and API version.  This example has two objects marked
   `Reconcilable: true` in v0, so it gets two files:
   * `KubernetesRuntimeDefinition`:
     `internal/kubernetes-runtime/v0_kubernetes_runtime_definition.go`
   * `KubernetesRuntimeInstance`:
     `internal/kubernetes-runtime/v0_kubernetes_runtime_instance.go`
1. Add the business logic to reconcile the system to the empty functions in
   those files, i.e. what happens when a kubernetes runtime definition is
   created, updated or deleted.  These files are scaffolding, so the generator
   writes them once and leaves them alone from then on.  Each function returns
   the number of seconds to wait before reconciling the object again, or zero
   when the object needs no further reconciliation, along with any error.  The
   empty functions in
   `internal/kubernetes-runtime/v0_kubernetes_runtime_definition.go` will look
   as follows.

   ```go
   // v0KubernetesRuntimeDefinitionCreated performs reconciliation when a v0
   // KubernetesRuntimeDefinition has been created.
   func v0KubernetesRuntimeDefinitionCreated(
       r *controller.Reconciler,
       kubernetesRuntimeDefinition *v0.KubernetesRuntimeDefinition,
       log *logr.Logger,
   ) (int64, error) {
       return 0, nil
   }

   // v0KubernetesRuntimeDefinitionUpdated performs reconciliation when a v0
   // KubernetesRuntimeDefinition has been updated.
   func v0KubernetesRuntimeDefinitionUpdated(
       r *controller.Reconciler,
       kubernetesRuntimeDefinition *v0.KubernetesRuntimeDefinition,
       log *logr.Logger,
   ) (int64, error) {
       return 0, nil
   }

   // v0KubernetesRuntimeDefinitionDeleted performs reconciliation when a v0
   // KubernetesRuntimeDefinition has been deleted.
   func v0KubernetesRuntimeDefinitionDeleted(
       r *controller.Reconciler,
       kubernetesRuntimeDefinition *v0.KubernetesRuntimeDefinition,
       log *logr.Logger,
   ) (int64, error) {
       return 0, nil
   }
   ```
   Repeat for each reconciler.
1. Register the new controller with the control plane installer in
   `pkg/threeport-installer/v0/threeport.go`.  Add an image name constant and a
   component name constant next to those for the other controllers, then add an
   entry for the new controller to `ThreeportControllerList` so that it is
   deployed with the control plane.
1. Add a database migration for the tables that hold the new objects.  See
   [Threeport Data Model Updates](data-model-updates.md).
1. Build a container image for the new controller.  A single Dockerfile at the
   root of the repo builds every component, and code generation adds the build
   targets for the new controller, so there is nothing to add per controller.
   If the controller needs another tool in its image at runtime, set
   `DockerfileTarget` on its object group in `sdk-config.yaml` to one of that
   Dockerfile's release targets, e.g. `release-helm`.

   ```bash
   mage build:tptdev
   ./bin/tptdev build --names kubernetes-runtime-controller
   ```

