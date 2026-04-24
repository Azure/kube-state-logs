# Source stage
FROM mcr.microsoft.com/azurelinux/base/core:3.0 AS source

ARG TARGETARCH
ARG GO_VERSION=1.26.2

# SHA256 Checksums can be found on the releases pages at https://github.com/microsoft/go/blob/microsoft/main/eng/doc/Downloads.md
ARG GO_SHA256SUM_AMD64=68bcd46d095165b37f4773450a8239ae5aa8d7e5be371eb69aea0510526ced5a
ARG GO_SHA256SUM_ARM64=0274f18451d73bf234798b1d1212e60b59ed1cd86d1b982701df3df76c563571

ENV GOROOT=/usr/local/go
ENV GOBIN=/usr/local/go/bin
ENV PATH=$PATH:$GOROOT/bin

RUN tdnf update -y && tdnf install -y gcc make binutils glibc-devel kernel kernel-headers ca-certificates tar awk curl curl-libs krb5 gcc build-essential jq && tdnf remove -y patch

# https://github.com/microsoft/go/releases
RUN GO_ARCHIVE="go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" && \
    if [ "${TARGETARCH}" = "arm64" ]; then GO_SHA256SUM="${GO_SHA256SUM_ARM64}"; else GO_SHA256SUM="${GO_SHA256SUM_AMD64}"; fi && \
    curl --retry 5 --retry-delay 10 --retry-all-errors -fOL "https://aka.ms/golang/release/latest/${GO_ARCHIVE}" \
        && echo "${GO_SHA256SUM}  ${GO_ARCHIVE}" | sha256sum --check \
        && tar -C /usr/local -zxf "${GO_ARCHIVE}" \
        && rm "${GO_ARCHIVE}" \
        && go version

ENV GOEXPERIMENT=ms_nocgo_opensslcrypto

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
FROM mcr.microsoft.com/azurelinux/distroless/minimal:3.0

COPY --from=build /out/kube-state-logs /kube-state-logs

USER nonroot:nonroot

ENTRYPOINT ["/kube-state-logs"]
