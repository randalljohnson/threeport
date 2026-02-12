# this development image is used for
# live-reload and debug (delve) builds,
# not for the tptdev cli tool itself.

# base target
FROM golang:1.24-alpine AS base
RUN mkdir /threeport
RUN go install github.com/go-delve/delve/cmd/dlv@latest && \
    mv /go/bin/dlv /usr/local/bin
COPY cmd/wait-for-tcp/main.go /tmp/wait-for-tcp.go
RUN CGO_ENABLED=0 go build -o /wait-for-tcp /tmp/wait-for-tcp.go
WORKDIR /threeport

# live-reload development target
FROM base AS live-reload
RUN apk update && apk add --no-cache git
RUN go install github.com/cosmtrek/air@latest && \
    mv /go/bin/air /usr/local/bin

# static development target
FROM base AS dev
ARG BINARY
COPY bin/${BINARY} /${BINARY}

# live-reload development target for terraform controller
FROM base AS live-reload-terraform
RUN apk update && apk add --no-cache git ca-certificates wget unzip
RUN wget https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip \
    && unzip terraform_${TERRAFORM_VERSION}_linux_amd64.zip -d /usr/local/bin/ \
    && rm terraform_${TERRAFORM_VERSION}_linux_amd64.zip
RUN go install github.com/cosmtrek/air@latest && \
    mv /go/bin/air /usr/local/bin

# static development target for terraform controller
FROM base AS dev-terraform
ARG BINARY
ARG TERRAFORM_VERSION=1.7.3
COPY bin/${BINARY} /${BINARY}
RUN apk add --no-cache ca-certificates wget unzip
RUN wget https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip \
    && unzip terraform_${TERRAFORM_VERSION}_linux_amd64.zip -d /usr/local/bin/ \
    && rm terraform_${TERRAFORM_VERSION}_linux_amd64.zip

# static development target for OCI controller (with Pulumi)
FROM base AS dev-oci
ARG BINARY
ARG PULUMI_VERSION=3.198.0
COPY bin/${BINARY} /${BINARY}
RUN apk add --no-cache ca-certificates curl
RUN curl -fsSL https://get.pulumi.com | sh -s -- --version ${PULUMI_VERSION}
ENV PATH="/root/.pulumi/bin:${PATH}"

