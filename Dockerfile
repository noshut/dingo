# Cross-compiles natively on the build platform; no QEMU needed for multi-arch.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /dingo ./cmd/dingo

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /dingo /dingo
# Queries the local /healthz endpoint (health.listen, default 127.0.0.1:9180).
HEALTHCHECK --interval=1m --timeout=10s --start-period=1m --retries=3 \
    CMD ["/dingo", "-healthcheck"]
ENTRYPOINT ["/dingo"]
CMD ["-config", "/etc/dingo/config.yaml"]
