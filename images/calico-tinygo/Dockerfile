ARG TARGETARCH=${TARGETARCH}

FROM almalinux:9 AS builder

ARG TARGETARCH

# calico/tinygo ships amd64 + arm64 only — TinyGo upstream does not publish
# Linux ppc64le/s390x tarballs. Wasm output is architecture-agnostic (Envoy
# V8 JITs for the host arch at load time), but providing a native arm64
# image lets developers on arm64 Linux hosts build without qemu emulation.
RUN if [ "${TARGETARCH}" != "amd64" ] && [ "${TARGETARCH}" != "arm64" ]; then \
        echo >&2 "error: calico/tinygo: TinyGo upstream does not publish a Linux ${TARGETARCH} tarball"; \
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

# TinyGo internally invokes the host `go` toolchain to compile Go packages
# (parse, type-check, run mod resolution) before lowering through LLVM IR
# to wasm; the bundled `go` is therefore not optional. Pin both versions
# in versions.yaml so the toolchain image is reproducible.
RUN set -eux; \
    golang_version=$(yq -r .golang.version /etc/versions.yaml); \
    golang_sha256=$(yq -r ".golang.checksum.sha256.${TARGETARCH}" /etc/versions.yaml); \
    wget -q -O /tmp/go.tar.gz "https://dl.google.com/go/go${golang_version}.linux-${TARGETARCH}.tar.gz"; \
    echo "${golang_sha256}  /tmp/go.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz

# TinyGo upstream tarball is statically linked (file: ELF static; ldd: not a
# dynamic executable) so the AlmaLinux 9 base does not need to provide
# matching libstdc++ / glibc — just untar to /usr/local.
RUN set -eux; \
    tinygo_version=$(yq -r .tinygo.version /etc/versions.yaml); \
    tinygo_sha256=$(yq -r ".tinygo.checksum.sha256.${TARGETARCH}" /etc/versions.yaml); \
    wget -q -O /tmp/tinygo.tar.gz \
        "https://github.com/tinygo-org/tinygo/releases/download/v${tinygo_version}/tinygo${tinygo_version}.linux-${TARGETARCH}.tar.gz"; \
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
