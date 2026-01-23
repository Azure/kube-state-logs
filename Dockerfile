# Build stage
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.25-fips-azurelinux3.0 AS builder

WORKDIR /src

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/kube-state-logs .

# Runtime stage
FROM mcr.microsoft.com/azurelinux/base/core:3.0

# Create non-root user if it doesn't exist
RUN tdnf install -y shadow-utils && \
    id nonroot &>/dev/null || useradd -r -u 65532 -s /sbin/nologin nonroot && \
    tdnf clean all

COPY --from=builder /out/kube-state-logs /kube-state-logs

USER nonroot:nonroot

ENTRYPOINT ["/kube-state-logs"]
