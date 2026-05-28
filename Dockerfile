# syntax=docker/dockerfile:1.7
#
# Single Dockerfile for every threeport binary image.
#
# Build args:
#   MAIN              Go source file to compile (path to a main package).
#   GCFLAGS           Optional gcflags string for the Go build.
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
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETARCH
ARG MAIN
ARG GCFLAGS=""

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -gcflags="${GCFLAGS}" -o /out/app ${MAIN}

# ----- release: minimal distroless image with the compiled binary -----
FROM gcr.io/distroless/static:nonroot AS release
COPY --from=builder /out/app /app
USER 65532:65532
ENTRYPOINT ["/app"]

# ----- dev-base: alpine with delve, shared by dev variants -----
FROM golang:1.24-alpine AS dev-base
RUN apk add --no-cache ca-certificates
RUN go install github.com/go-delve/delve/cmd/dlv@latest && \
    mv /go/bin/dlv /usr/local/bin

# ----- dev: compiled binary + delve -----
FROM dev-base AS dev
COPY --from=builder /out/app /app
ENTRYPOINT ["/app"]

# ----- live-reload: air watcher; expects source mounted at /threeport -----
FROM dev-base AS live-reload
RUN apk add --no-cache git
RUN go install github.com/air-verse/air@latest && \
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
