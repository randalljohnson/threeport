# Developer Documentation

To get started with a development environment, see our [Quickstart
Guide](quickstart.md) and [Contribuing Guide](contributing.md).

## Overview

This threeport repo contains the core components of the threeport control plane.
In addition, this repo contains the threport command line tool `tptctl`, the
developer command line tool `tptdev` and a client library for the threeport API.

Following is an overview of what lives at the root of this repo:
* The `bin` directory is where binary artifacts are stored when built.
* The `cmd` directory contains the main package for each program that produces a
  binary artifact:
  * [agent](../../cmd/agent/README.md) is the runtime control plane agent.
  * [aws-controller](../../cmd/aws-controller/README.md) is the threeport
    controller that manages AWS resources as workload dependencies.
  * [sdk](../../cmd/sdk/README.md) generates scaffolding and boilerplate code
    for various components and packages.
  * [gateway-controller](../../cmd/gateway-controller/README.md) is the threeport
    controller that manages network ingress gateways and DNS records for workloads.
  * [kubernetes-runtime-controller](../../cmd/kubernetes-runtime-controller/README.md)
    is the threeport controller that manages Kubernetes clusters as runtime
    environments for workloads.
  * [rest-api](../../cmd/rest-api/README.md) is the RESTful API for the threeport
    control plane.
  * [tptctl](../../cmd/tptctl/README.md) is the primary client CLI for threeport uers.
  * [tptdev](../../cmd/tptdev/README.md) is a developer tool for threeport.
  * [kubernetes-workload-controller](../../cmd/kubernetes-workload-controller/README.md) is the threeport
    controller that manages containerized workloads on Kubernetes for users.
  * [control-plane-controller](../../cmd/control-plane-controller/README.md) is the threeport
    controller that manages control planes created by the threeport control plane itself.
  * [database-migrator](../../cmd/database-migrator/README.md) applies the
    database schema migrations required by the threeport API.
  * gcp-controller is the threeport controller that manages GKE clusters on
    Google Cloud.
  * [helm-workload-controller](../../cmd/helm-workload-controller/README.md)
    is the threeport controller that manages containerized workloads on
    Kubernetes using Helm charts.
  * machine-runtime-controller is the threeport controller that manages
    SSH-reachable machines as runtime environments for workloads.
  * machine-workload-controller is the threeport controller that manages
    workloads deployed to machine runtimes over SSH.
  * [observability-controller](../../cmd/observability-controller/README.md)
    is the threeport controller that manages metrics collection, log
    aggregation and observability dashboards for workloads.
  * oci-controller is the threeport controller that manages OKE clusters on
    Oracle Cloud Infrastructure.
  * [secret-controller](../../cmd/secret-controller/README.md) is the threeport
    controller that manages sensitive secrets for workloads.
  * [terraform-controller](../../cmd/terraform-controller/README.md) is the
    threeport controller that manages cloud infrastructure with Terraform.
* The `docs` directory contains these developer docs.
* The `samples` directory contains example configurations for testing threeport.
* The `hack` directory contains ad hoc scripts and utilities that have not made
  into a real package or `tptdev`.
* The `internal` directory contains packages that are used internally by core
  threeport components only.
* The `pkg` directory contains packages that are used by threeport and can be
  imported into other projects.
* The `test` directory contains testing components such end-to-end tests.

## Core Components from the Community

The threeport control plane core components consist of the RESTful API and the
various controllers that provide logic and functionality for the system.  In
addition there are two 3rd party components:
* [CockroachDB](https://github.com/cockroachdb/cockroach) serves as the
  persistence layer for the threeport API.
* [NATS](https://github.com/nats-io/nats-server) is the message broker used to
  relay notifications from the API to controllers, and by the controllers to
  place distributed locks on objects being reconciled.

## Makefile

This contains a collection of helpful developer make targets.  Run `make` to get
a list of available operations.

## Packages

Following is an index of package documentation:
* [`internal/agent`](../../internal/agent/README.md)
* [`internal/aws`](../../internal/aws/README.md)
* [`internal/gateway`](../../internal/gateway/README.md)
* [`internal/kubernetes-runtime`](../../internal/kubernetes-runtime/README.md)
* [`internal/kubernetes-workload`](../../internal/kubernetes-workload/README.md)
* [`internal/provider`](../../internal/provider/README.md)
* [`internal/version`](../../internal/version/README.md)
* [`pkg/agent`](../../pkg/agent/README.md)
* [`pkg/api`](../../pkg/api/README.md)
* [`pkg/api-server`](../../pkg/api-server/README.md)
* [`pkg/auth`](../../pkg/auth/README.md)
* [`pkg/cli`](../../pkg/cli/README.md)
* [`pkg/client`](../../pkg/client/README.md)
* [`pkg/config`](../../pkg/config/README.md)
* [`pkg/controller`](../../pkg/controller/README.md)
* [`pkg/encryption`](../../pkg/encryption/README.md)
* [`pkg/kube`](../../pkg/kube/README.md)
* [`pkg/log`](../../pkg/log/README.md)
* [`pkg/notifications`](../../pkg/notifications/README.md)
* [`pkg/sdk`](../../pkg/sdk/README.md)
* [`pkg/threeport-installer`](../../pkg/threeport-installer/README.md)
* [`pkg/threeport-installer/v0/tptdev`](../../pkg/threeport-installer/v0/tptdev/README.md)
* [`pkg/util`](../../pkg/util/README.md)

