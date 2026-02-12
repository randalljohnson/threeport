ARG ARCH=amd64
FROM golang:1.22 as builder
RUN mkdir /build
ADD . /build
WORKDIR /build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} go build -a -o agent cmd/agent/main.go
RUN CGO_ENABLED=0 go build -o /wait-for-tcp cmd/wait-for-tcp/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /build/agent .
COPY --from=builder /wait-for-tcp /wait-for-tcp
USER 65532:65532

ENTRYPOINT ["/agent"]
