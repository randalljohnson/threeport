# syntax=docker/dockerfile:1.7
#
# Single Dockerfile for every threeport binary image.
#
# Why compile outside Docker?
#
# Native `go build` uses Go's content-addressed build cache, which
# invalidates per package: a single-file edit only recompiles the
# affected package and its dependents. Pulling that compile inside a RUN
# layer would tie the cache to Docker's layer-hash scheme, where any
# change in the COPY context invalidates the whole downstream layer.
# The cache delta we get from `go build` natively is much finer-grained
# than anything Docker layer caching can match.
#
# Cross-compile is pure-Go (CGO_ENABLED=0), so the build needs no C
# toolchain and no QEMU; one machine produces both arches in parallel.
#
# Callers (tptdev, mage build:allImages, CI) compile binaries first, then
# pass them in via the build context. The context root must contain
# per-arch subdirectories with the named binary:
#   ./<context>/amd64/<binary-name>
#   ./<context>/arm64/<binary-name>
#
# Build args:
#   BINARY            Binary file name within the per-arch directory.
#   TERRAFORM_VERSION Terraform release used by dev-terraform.
#   PULUMI_VERSION    Pulumi release used by dev-pulumi.
#
# Targets:
#   release          Distroless image with the compiled binary.
#   dev              Alpine + delve + the compiled binary.
#   live-reload      Alpine + air; expects source mounted at /threeport.
#   dev-terraform    dev + terraform binary.
#   dev-pulumi       dev + pulumi CLI.
#
# Multi-arch builds use `--platform=linux/amd64,linux/arm64`;
# ${TARGETARCH} below resolves per platform so one buildx invocation
# pulls the right binary for each arch. mirror.gcr.io is Docker Hub via
# Google's pull-through mirror (no per-user rate limit).

# ----- release: minimal distroless image with the compiled binary -----
#
# Binary lands at /${BINARY} so the deployment manifest's
# /<component-name> command resolves.
FROM gcr.io/distroless/static:nonroot AS release
ARG TARGETARCH
ARG BINARY
COPY ${TARGETARCH}/${BINARY} /${BINARY}
USER 65532:65532

# ----- dev-base: alpine with delve, shared by dev variants -----
FROM mirror.gcr.io/library/golang:1.24-alpine AS dev-base
ARG DELVE_VERSION=1.26.3
RUN apk add --no-cache ca-certificates
RUN go install github.com/go-delve/delve/cmd/dlv@v${DELVE_VERSION} && \
    mv /go/bin/dlv /usr/local/bin

# ----- dev: compiled binary + delve -----
FROM dev-base AS dev
ARG TARGETARCH
ARG BINARY
COPY ${TARGETARCH}/${BINARY} /${BINARY}

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
