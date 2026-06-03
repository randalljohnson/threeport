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
#   TERRAFORM_VERSION Terraform release pulled by terraform-bin.
#   PULUMI_VERSION    Pulumi release pulled by pulumi-bin.
#
# Targets:
#   release          Distroless image with the compiled binary.
#   terraform        release + terraform CLI.
#   pulumi           release + pulumi CLI.
#   dev              Alpine + delve + the compiled binary.
#   dev-terraform    dev + terraform CLI.
#   dev-pulumi       dev + pulumi CLI.
#
# Multi-arch builds use `--platform=linux/amd64,linux/arm64`;
# ${TARGETARCH} below resolves per platform so one buildx invocation
# pulls the right binary for each arch. mirror.gcr.io is Docker Hub via
# Google's pull-through mirror (no per-user rate limit).

# ============================================================
# intermediate stages: build-only, never used as a final target
# ============================================================

# ----- dev-base: alpine + delve, foundation for every dev variant -----
FROM mirror.gcr.io/library/golang:1.24-alpine AS dev-base
ARG DELVE_VERSION=1.26.3
RUN apk add --no-cache ca-certificates
RUN go install github.com/go-delve/delve/cmd/dlv@v${DELVE_VERSION} && \
    mv /go/bin/dlv /usr/local/bin

# ----- terraform-bin: download terraform; consumed by terraform + dev-terraform -----
FROM mirror.gcr.io/library/alpine:3 AS terraform-bin
ARG TARGETARCH
ARG TERRAFORM_VERSION=1.7.3
RUN apk add --no-cache wget unzip && \
    wget -q https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip && \
    unzip -o terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip -d /out && \
    rm terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip

# ----- pulumi-bin: download pulumi; consumed by pulumi + dev-pulumi -----
FROM mirror.gcr.io/library/alpine:3 AS pulumi-bin
ARG PULUMI_VERSION=3.185.0
RUN apk add --no-cache curl && \
    curl -fsSL https://get.pulumi.com | sh -s -- --version ${PULUMI_VERSION}

# ==================================
# release targets: distroless images
# ==================================

# ----- release: minimal distroless image with the compiled binary -----
#
# Binary lands at /${BINARY} so the deployment manifest's
# /<component-name> command resolves.
FROM gcr.io/distroless/static:nonroot AS release
ARG TARGETARCH
ARG BINARY
COPY ${TARGETARCH}/${BINARY} /${BINARY}
USER 65532:65532

# ----- terraform: release + terraform CLI -----
FROM release AS terraform
COPY --from=terraform-bin /out/terraform /usr/local/bin/terraform

# ----- pulumi: release + pulumi CLI -----
#
# HOME=/home/nonroot is distroless/static:nonroot's default; pulumi
# writes its plugin cache under $HOME/.pulumi.
FROM release AS pulumi
COPY --from=pulumi-bin /root/.pulumi/bin /pulumi/bin
ENV PATH="/pulumi/bin:${PATH}"
ENV HOME=/home/nonroot

# =====================================
# dev targets: alpine + delve for debug
# =====================================

# ----- dev: dev-base + the compiled binary -----
FROM dev-base AS dev
ARG TARGETARCH
ARG BINARY
COPY ${TARGETARCH}/${BINARY} /${BINARY}

# ----- dev-terraform: dev + terraform CLI -----
FROM dev AS dev-terraform
COPY --from=terraform-bin /out/terraform /usr/local/bin/terraform

# ----- dev-pulumi: dev + pulumi CLI -----
FROM dev AS dev-pulumi
USER root
COPY --from=pulumi-bin /root/.pulumi/bin /pulumi/bin
ENV PATH="/pulumi/bin:${PATH}"
RUN mkdir -p /.threeport && chown 65532:65532 /.threeport
ENV HOME=/.threeport
USER 65532:65532
