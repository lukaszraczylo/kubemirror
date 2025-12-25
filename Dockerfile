# Build stage
FROM golang:1.25-alpine AS builder

# Install ca-certificates for HTTPS support
RUN apk add --no-cache ca-certificates

WORKDIR /workspace

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build the binary with security flags
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build \
    -trimpath \
    -ldflags="-w -s -extldflags '-static'" \
    -tags=netgo \
    -o kubemirror \
    ./cmd/kubemirror

# Runtime stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.title="kubemirror"
LABEL org.opencontainers.image.description="Kubernetes controller for mirroring resources across namespaces"
LABEL org.opencontainers.image.source="https://github.com/lukaszraczylo/kubemirror"
LABEL org.opencontainers.image.licenses="MIT"

WORKDIR /

# Copy ca-certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary from builder
COPY --from=builder /workspace/kubemirror /kubemirror

# Use nonroot user (uid:gid 65532:65532)
USER 65532:65532

ENTRYPOINT ["/kubemirror"]
