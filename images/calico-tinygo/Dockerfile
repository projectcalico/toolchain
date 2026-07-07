# ctypecompat build stage: supply the musl ctype/wctype functions (toupper,
# tolower, the is*/isw* family and their _l locale variants) that TinyGo >= 0.38
# stopped compiling into its bundled wasi-libc. The WAF wasm this image builds
# links go-re2 v1.6.0's prebuilt static archives, which reference those as
# externs; without them the link fails with "wasm-ld: undefined symbol: toupper".
# go-re2 is frozen at v1.6.0 -- v1.8.0+ deleted the TinyGo static archives and
# moved cre2 to a wazero runtime that Envoy's V8 cannot host, so we cannot bump
# our way out of the missing symbols.
#
# The objects are lifted verbatim from TinyGo 0.34's prebuilt wasi-libc: its
# pinned wasi-libc commit matches newer TinyGo (0.38 only dropped the ctype glob
# from its libc build, the sources are unchanged), so they are ABI-compatible.
# The source image is digest-pinned and `ar rcsD` writes deterministic archives,
# so this extraction is reproducible. Built here rather than committed to the repo.
#
# TINYGO_VERSION is declared before any FROM so it is in scope for the final
# stage's FROM below; the Makefile passes it from versions.yaml.
ARG TINYGO_VERSION
FROM calico/tinygo@sha256:9ff0bed2e3598f695a2fc6222be901eb0f3a0153549b82d28aa640c8487e1854 AS ctypecompat
RUN set -eux; \
    libc=/usr/local/tinygo/lib/wasi-libc/sysroot/lib/wasm32-wasi/libc.a; \
    objs="__ctype_b_loc.o __ctype_get_mb_cur_max.o __ctype_tolower_loc.o __ctype_toupper_loc.o \
          isalnum.o isalpha.o isascii.o isblank.o iscntrl.o isdigit.o isgraph.o islower.o \
          isprint.o ispunct.o isspace.o isupper.o isxdigit.o \
          iswalnum.o iswalpha.o iswblank.o iswcntrl.o iswctype.o iswdigit.o iswgraph.o \
          iswlower.o iswprint.o iswpunct.o iswspace.o iswupper.o iswxdigit.o \
          toascii.o tolower.o toupper.o towctrans.o wcswidth.o wctrans.o"; \
    mkdir -p /work && cd /work; \
    ar x "$libc" $objs; \
    ar rcsD /libctypecompat.a $objs

# calico/tinygo builds the coraza WAF wasm (TinyGo wasip1 target).
#
# Based on the upstream tinygo/tinygo image rather than a bare distro base:
# TinyGo >= 0.38's release binary is dynamically linked against a newer
# libstdc++/glibc than AlmaLinux 9 provides (the linux-arm64 binary needs
# GLIBCXX_3.4.30 and GLIBC_2.36/2.38; EL9 ships glibc 2.34 + libstdc++ 3.4.29,
# and EL9 has no runtime libstdc++.so.6 with 3.4.30 -- gcc-toolset only ships
# static libs). The upstream image already carries a matching go + tinygo +
# libstdc++ on both amd64 and arm64, so basing on it sidesteps the runtime
# mismatch entirely. TINYGO_VERSION is passed by the Makefile from versions.yaml
# so the base image tag selects the toolchain. This is a tag, not a digest, so it
# fixes the toolchain version, not the exact bytes.
ARG TINYGO_VERSION
FROM tinygo/tinygo:${TINYGO_VERSION}

ARG TARGETARCH

# calico/tinygo ships amd64 + arm64 only -- TinyGo upstream does not publish
# Linux ppc64le/s390x images. Wasm output is architecture-agnostic (Envoy V8
# JITs for the host arch at load time), but providing a native arm64 image
# lets developers on arm64 Linux hosts build without qemu emulation.
RUN if [ "${TARGETARCH}" != "amd64" ] && [ "${TARGETARCH}" != "arm64" ]; then \
        echo >&2 "error: calico/tinygo: unsupported arch ${TARGETARCH}"; \
        exit 1; \
    fi

# Switch to root only for the setup steps that write root-owned paths, then
# switch back to `tinygo` at the end so the image is not root by default.
USER root

# The upstream image (Debian) lacks a few tools downstream builds/tests expect.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        file \
        git \
        make \
        wget && \
    rm -rf /var/lib/apt/lists/*

# ctype/wctype compat archive, built in the stage above. Downstream WAF builds
# append it to their TinyGo wasm link via
# `-ldflags=-extldflags=$CALICO_TINYGO_CTYPE_COMPAT`; the test fixture under
# test/fixture does the same in CI as a regression guard.
COPY --from=ctypecompat /libctypecompat.a /usr/local/lib/ctypecompat/libctypecompat.a
ENV CALICO_TINYGO_CTYPE_COMPAT=/usr/local/lib/ctypecompat/libctypecompat.a

# GOPATH world-writable so downstream builds running as any uid (via
# `--user $(id -u):$(id -g)`) can populate the module/build caches under it.
ENV GOPATH=/go
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 1777 "$GOPATH"
ENV PATH=$PATH:/go/bin

WORKDIR /go

# Recorded for build-log auditability.
RUN go version && tinygo version

# Restore the base image's non-root default so the image is not root by default.
USER tinygo
