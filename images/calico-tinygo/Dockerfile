ARG TARGETARCH=${TARGETARCH}

FROM almalinux:9 AS builder

ARG TARGETARCH

# calico/tinygo publishes a single host-arch image because TinyGo wasm output
# is architecture-agnostic — the same .wasm bytes execute on amd64, arm64,
# ppc64le and s390x runtimes (Envoy V8 JITs for the host arch at load time).
# Building on arm64/ppc64le/s390x would just duplicate CI work without
# producing a different artifact.
RUN if [ "${TARGETARCH}" != "amd64" ]; then \
        echo >&2 "error: calico/tinygo is amd64 only — wasm output is arch-neutral"; \
        exit 1; \
    fi

RUN dnf upgrade -y && dnf install -y epel-release && \
    dnf install -y yq

RUN dnf install -y \
        bsdtar \
        file \
        git \
        make \
        wget && \
    dnf clean all

COPY versions.yaml /etc/versions.yaml

# TinyGo invokes the host `go` toolchain to compile Go packages before
# lowering to LLVM IR, so calico/tinygo bundles both Go and TinyGo. Pin both
# in versions.yaml.
RUN set -eux; \
    golang_version=$(yq -r .golang.version /etc/versions.yaml); \
    golang_sha256=$(yq -r .golang.checksum.sha256.amd64 /etc/versions.yaml); \
    wget -q -O /tmp/go.tar.gz "https://dl.google.com/go/go${golang_version}.linux-amd64.tar.gz"; \
    echo "${golang_sha256}  /tmp/go.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz

# TinyGo upstream tarball is statically linked (file: ELF static; ldd: not a
# dynamic executable) so the AlmaLinux 9 base does not need to provide
# matching libstdc++ / glibc — just untar to /usr/local.
RUN set -eux; \
    tinygo_version=$(yq -r .tinygo.version /etc/versions.yaml); \
    tinygo_sha256=$(yq -r .tinygo.checksum.sha256.amd64 /etc/versions.yaml); \
    wget -q -O /tmp/tinygo.tar.gz \
        "https://github.com/tinygo-org/tinygo/releases/download/v${tinygo_version}/tinygo${tinygo_version}.linux-amd64.tar.gz"; \
    echo "${tinygo_sha256}  /tmp/tinygo.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/tinygo.tar.gz; \
    rm /tmp/tinygo.tar.gz

ENV GOPATH=/go
ENV PATH=/usr/local/go/bin:/usr/local/tinygo/bin:$PATH

RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 1777 "$GOPATH"

# Recorded for build-log auditability.
RUN go version && tinygo version

FROM scratch

ENV GOPATH=/go
ENV PATH=/usr/local/go/bin:/usr/local/tinygo/bin:/go/bin:$PATH

COPY --from=builder / /

WORKDIR /go
