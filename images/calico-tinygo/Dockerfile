ARG TARGETARCH=${TARGETARCH}

# Upstream TinyGo image, used only as the source of a libstdc++ runtime that
# matches the TinyGo binary this image ships (see the COPY below). TINYGO_VERSION
# is passed by the Makefile from versions.yaml so it stays in lockstep.
ARG TINYGO_VERSION
FROM tinygo/tinygo:${TINYGO_VERSION} AS tinygobin

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

# TinyGo >= 0.38's release binary is dynamically linked against a newer
# libstdc++ than AlmaLinux 9 ships (arm64 needs GLIBCXX_3.4.30; the EL9 base
# tops out at 3.4.29 and gcc-toolset does NOT ship a drop-in runtime .so), so
# `tinygo version` fails at image-build time with
# "libstdc++.so.6: version `GLIBCXX_3.4.30' not found". Lift the exact
# libstdc++ runtime from the upstream TinyGo image (the lib TinyGo is built
# against and known to run with) for this build's arch, place it on the loader
# path, and register with ldconfig. libstdc++ depends only on libgcc_s + libc,
# both present in the AlmaLinux 9 base, so the cross-distro copy is safe. The
# TinyGo image tag is derived from versions.yaml so it stays in lockstep.
COPY --from=tinygobin /usr/lib/*-linux-gnu/libstdc++.so.6.0.* /usr/local/lib64/
RUN so="$(find /usr/local/lib64 -name 'libstdc++.so.6.0.*' -print -quit)"; \
    test -n "$so" || { echo >&2 "error: libstdc++ runtime not copied from tinygo image"; exit 1; }; \
    strings "$so" | grep -q GLIBCXX_3.4.30 || { echo >&2 "error: $so lacks GLIBCXX_3.4.30"; exit 1; }; \
    ln -sf "$(basename "$so")" /usr/local/lib64/libstdc++.so.6; \
    echo "/usr/local/lib64" > /etc/ld.so.conf.d/tinygo-libstdcxx.conf; \
    ldconfig

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

# TinyGo's upstream tarball is untarred straight to /usr/local. The binary is
# dynamically linked against libstdc++ (arm64 needs GLIBCXX_3.4.30, supplied by
# the libstdc++ copied from the upstream tinygo image + ldconfig'd above); glibc
# from the AlmaLinux 9 base is sufficient.
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

# ctype/wctype compat archive. TinyGo 0.38 rewrote how it builds its bundled
# wasi-libc (builder/wasilibc.go) and dropped the musl ctype/*.c sources, so
# toupper/tolower and the is*/isw*_l ctype family are no longer emitted into
# libc.a. Prebuilt wasm archives that reference those as externs -- notably
# wasilibs/go-re2 v1.6.0 (libc++.a) and wasilibs/nottinygc v0.7.1
# (libmimalloc.a), the frozen archives WAF consumers must keep using -- then
# fail to link with "wasm-ld: undefined symbol: toupper". Ship the objects
# here (lifted verbatim from TinyGo 0.34's wasi-libc, identical pinned
# wasi-libc commit) so downstream builds can append them to the wasm link via
# `-ldflags=-extldflags=$CALICO_TINYGO_CTYPE_COMPAT`. See ctypecompat/README.md.
COPY ctypecompat/libctypecompat.a /usr/local/lib/ctypecompat/libctypecompat.a
ENV CALICO_TINYGO_CTYPE_COMPAT=/usr/local/lib/ctypecompat/libctypecompat.a

# Recorded for build-log auditability.
RUN go version && tinygo version

FROM scratch

ENV GOPATH=/go
ENV PATH=/usr/local/go/bin:/usr/local/tinygo/bin:/go/bin:$PATH
# Path to the ctype/wctype compat archive baked into the builder stage above;
# re-declared here because ENV does not survive across build stages. Downstream
# WAF builds link it via `-ldflags=-extldflags=$CALICO_TINYGO_CTYPE_COMPAT`.
ENV CALICO_TINYGO_CTYPE_COMPAT=/usr/local/lib/ctypecompat/libctypecompat.a

COPY --from=builder / /

WORKDIR /go
