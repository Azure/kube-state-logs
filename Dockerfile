# Source stage
FROM mcr.microsoft.com/azurelinux/base/core:3.0 AS source

ARG TARGETARCH
ARG GO_VERSION=1.26.6

# SHA256 Checksums can be found on the releases pages at https://github.com/microsoft/go/blob/microsoft/main/eng/doc/Downloads.md
ARG GO_SHA256SUM_AMD64=d150752f0dfad988313e5dae993faac0f4adb3f253647714718383621a1f3362
ARG GO_SHA256SUM_ARM64=7ea6883405ac48b5fcf850f214fcd0482dc7f1b64eb68484e046c8417d6378fa

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
FROM mcr.microsoft.com/azurelinux/distroless/base:3.0

COPY --from=build /out/kube-state-logs /kube-state-logs

USER nonroot:nonroot

ENTRYPOINT ["/kube-state-logs"]
