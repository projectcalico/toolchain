# calico/tinygo builds the coraza WAF wasm (TinyGo wasip1 target).
#
# Based on the upstream tinygo/tinygo image rather than a bare distro base:
# TinyGo >= 0.38's release binary is dynamically linked against a newer
# libstdc++/glibc than AlmaLinux 9 provides (the linux-arm64 binary needs
# GLIBCXX_3.4.30 and GLIBC_2.36/2.38; EL9 ships glibc 2.34 + libstdc++ 3.4.29,
# and EL9 has no runtime libstdc++.so.6 with 3.4.30 -- gcc-toolset only ships
# static libs). The upstream image already carries a matching go + tinygo +
# libstdc++ on both amd64 and arm64, so basing on it sidesteps the runtime
# mismatch entirely. TINYGO_VERSION is passed by the Makefile from
# versions.yaml so the base image tag pins the toolchain reproducibly.
ARG TINYGO_VERSION
FROM tinygo/tinygo:${TINYGO_VERSION}

ARG TARGETARCH

# The upstream image runs as the unprivileged `tinygo` user; switch to root for
# the package install + filesystem setup below. Downstream builds run the image
# with an explicit `--user $(id -u):$(id -g)`, so the final USER is not relied
# on, but leave it as root (the base default before its own USER line) to keep
# the caches under GOPATH (chmod 1777 below) writable by any invoking uid.
USER root

# calico/tinygo ships amd64 + arm64 only -- TinyGo upstream does not publish
# Linux ppc64le/s390x images. Wasm output is architecture-agnostic (Envoy V8
# JITs for the host arch at load time), but providing a native arm64 image
# lets developers on arm64 Linux hosts build without qemu emulation.
RUN if [ "${TARGETARCH}" != "amd64" ] && [ "${TARGETARCH}" != "arm64" ]; then \
        echo >&2 "error: calico/tinygo: unsupported arch ${TARGETARCH}"; \
        exit 1; \
    fi

# The upstream image (Debian) lacks a few tools downstream builds/tests expect.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        file \
        git \
        make \
        wget && \
    rm -rf /var/lib/apt/lists/*

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

# GOPATH world-writable so downstream builds running as `--user $(id -u)` can
# populate the module/build caches under it.
ENV GOPATH=/go
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 1777 "$GOPATH"
ENV PATH=$PATH:/go/bin

WORKDIR /go

# Recorded for build-log auditability.
RUN go version && tinygo version
