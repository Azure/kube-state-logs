# Source stage
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.25-fips-azurelinux3.0 AS source

WORKDIR /src

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Test stage — run with: docker build --target test .
FROM source AS test
RUN mkdir -p /out && \
    go test -v -coverprofile=/out/coverage.txt ./... 2>&1 | tee /out/test-report.txt

# Build stage
FROM source AS build
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/kube-state-logs .

# Runtime stage
FROM mcr.microsoft.com/azurelinux/base/core:3.0

# Create non-root user if it doesn't exist
RUN tdnf install -y shadow-utils && \
    id nonroot &>/dev/null || useradd -r -u 65532 -s /sbin/nologin nonroot && \
    tdnf clean all

COPY --from=build /out/kube-state-logs /kube-state-logs

USER nonroot:nonroot

ENTRYPOINT ["/kube-state-logs"]
