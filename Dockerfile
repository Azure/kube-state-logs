# Source stage
FROM mcr.microsoft.com/azurelinux/base/core:3.0@sha256:3d16d8799f71a78f5bddb3b7cda4a57df1f40ecfa58bac89933010596aa86880 AS source

RUN tdnf update -y && tdnf install -y golang gcc make binutils glibc-devel kernel kernel-headers ca-certificates tar awk curl curl-libs krb5 gcc build-essential jq && tdnf remove -y patch && go version

WORKDIR /src

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Test stage — run with: docker build --target test .
FROM source AS test
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN mkdir -p /out && \
    go test -v -coverprofile=/out/coverage.txt ./... 2>&1 | tee /out/test-report.txt

# Build stage
FROM source AS build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/kube-state-logs .

# Runtime stage
FROM mcr.microsoft.com/azurelinux/distroless/base:3.0@sha256:11ecd8f7cd1cad0e4fbb565ab98768f1a2ce35cb2730c867217baac011ae8645

COPY --from=build /out/kube-state-logs /kube-state-logs

USER nonroot:nonroot

ENTRYPOINT ["/kube-state-logs"]
