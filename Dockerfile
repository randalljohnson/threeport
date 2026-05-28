# syntax=docker/dockerfile:1.7
#
# Single Dockerfile for every threeport binary image.
#
# Build args:
#   MAIN              Go source file to compile (path to a main package).
#   GCFLAGS           Optional gcflags string for the Go build.
#   GO_BUILD_FLAGS    Optional GOFLAGS string passed to `go build`. Local
#                     builds leave this empty (use full host parallelism);
#                     CI sets it to `-p=2` to cap peak memory on the
#                     free-tier 16 GB runners when both archs build in
#                     parallel on large-SDK components (aws, gcp, oci).
#   TERRAFORM_VERSION Terraform release used by the dev-terraform target.
#   PULUMI_VERSION    Pulumi release used by the dev-pulumi target.
#
# Targets:
#   release          Minimal distroless image with the compiled binary.
#   dev              Alpine + delve + the compiled binary, for debug pods.
#   live-reload      Alpine + air; expects source mounted at /threeport.
#   dev-terraform    dev + the terraform binary.
#   dev-pulumi       dev + the pulumi CLI.
#
# Select target with `--target=<name>`. Multi-arch builds use
# `--platform=linux/amd64,linux/arm64` on the buildx invocation.

# ----- builder: cross-compile MAIN at native host arch -----
# mirror.gcr.io is Google's free pull-through mirror of Docker Hub; using
# it for the base image avoids Docker Hub's per-user pull rate limit.
# BASE_IMAGE lets CI swap in a pre-warmed image carrying go mod download
# and compiled pkg/internal output in regular layers, so the per-component
# build only has to compile cmd/<name>/main.go.
ARG BASE_IMAGE=mirror.gcr.io/library/golang:1.24
FROM --platform=$BUILDPLATFORM ${BASE_IMAGE} AS builder
ARG TARGETARCH
ARG MAIN
ARG GCFLAGS=""
ARG GO_BUILD_FLAGS=""
ARG GOMEMLIMIT=""

# pin GOCACHE so derived BASE_IMAGEs that bake a warm build cache and the
# default golang image land on the same path. cache-mount overlays are
# intentionally omitted; they hide the base image's layer-baked caches.
ENV GOCACHE=/root/.cache/go-build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# GO_BUILD_FLAGS caps Go's compile parallelism (e.g. -p=2). GOMEMLIMIT
# soft-caps the Go runtime's heap; both are CI-only guardrails against
# OOM when a large-SDK component (aws, gcp, oci) compiles on a memory-
# constrained runner. Empty defaults leave local builds unconstrained.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    GOFLAGS="${GO_BUILD_FLAGS}" GOMEMLIMIT="${GOMEMLIMIT}" \
    go build -gcflags="${GCFLAGS}" -o /out/app ${MAIN}

# ----- release: minimal distroless image with the compiled binary -----
FROM gcr.io/distroless/static:nonroot AS release
COPY --from=builder /out/app /app
USER 65532:65532
ENTRYPOINT ["/app"]

# ----- dev-base: alpine with delve, shared by dev variants -----
FROM mirror.gcr.io/library/golang:1.24-alpine AS dev-base
ARG DELVE_VERSION=1.26.3
RUN apk add --no-cache ca-certificates
RUN go install github.com/go-delve/delve/cmd/dlv@v${DELVE_VERSION} && \
    mv /go/bin/dlv /usr/local/bin

# ----- dev: compiled binary + delve -----
FROM dev-base AS dev
COPY --from=builder /out/app /app
ENTRYPOINT ["/app"]

# ----- live-reload: air watcher; expects source mounted at /threeport -----
FROM dev-base AS live-reload
ARG AIR_VERSION=1.65.3
RUN apk add --no-cache git
RUN go install github.com/air-verse/air@v${AIR_VERSION} && \
    mv /go/bin/air /usr/local/bin
WORKDIR /threeport

# ----- dev-terraform: dev + terraform binary -----
FROM dev AS dev-terraform
USER root
ARG TARGETARCH
ARG TERRAFORM_VERSION=1.7.3
RUN apk add --no-cache wget unzip
RUN wget -q https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip && \
    unzip -o terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip -d /usr/local/bin/ && \
    rm terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip
USER 65532:65532

# ----- dev-pulumi: dev + pulumi CLI -----
FROM dev AS dev-pulumi
USER root
ARG PULUMI_VERSION=3.185.0
RUN apk add --no-cache curl
RUN curl -fsSL https://get.pulumi.com | sh -s -- --version ${PULUMI_VERSION}
ENV PATH="/root/.pulumi/bin:${PATH}"
RUN mkdir -p /.threeport
ENV HOME=/.threeport
USER 65532:65532
