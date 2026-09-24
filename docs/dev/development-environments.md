# Development Environments

## New Dev Environment

The following instructions allow you to stand up a local Threeport control plane
built from the code you have locally.

Create a local container registry.  This allows you to build new container
images and use them without having to wait for pushes to - and pulls from - a
remote registry.

```bash
mage dev:localRegistryUp
```

Build contianer images for each of the control plane components and push them to
the local container registry.

```bash
mage build:tptdev
./bin/tptdev build -r localhost:5001 -t dev --push
```

Install a local control plane using images pulled from the local registry.

```bash
./bin/tptdev up -r localhost:5001 -t dev --local-registry
```

## Update Dev Environment

Build new images and reinstall the stateless control plane so every
controller and the API server come back on the current specs.

```bash
./bin/tptdev build -r localhost:5001 -t dev --push
./bin/tptdev reinstall -r localhost:5001 -t dev
```

To refresh a single component image without a full reinstall, build it
and load it into kind, then delete that pod so it restarts on the new
image:

```bash
./bin/tptdev build -r localhost:5001 -t dev --load --names kubernetes-workload-controller
kubectl delete po [kubernetes workload controller pod name]
```

## Remove a Dev Environment

Spin down the control plane cluster.

```bash
./bin/tptdev down
```

Stop and remove the registry container.

```bash
mage dev:localRegistryDown
```

