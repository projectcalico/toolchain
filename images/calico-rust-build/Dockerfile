FROM almalinux:9 AS builder

ARG TARGETARCH

ARG PROTOC_VERSION=34.1

ENV CC=clang
ENV CXX=clang++

RUN dnf upgrade -y && dnf install -y epel-release && \
    dnf install -y yq

RUN dnf install -y \
    binutils-aarch64-linux-gnu \
    bsdtar \
    clang \
    cmake \
    cpio \
    lld \
    make \
    wget

# clang uses the verbatim target triple as the binutils prefix, so
# `--target=aarch64-unknown-linux-gnu` looks for `aarch64-unknown-linux-gnu-ld`.
# The EPEL package only ships the shorter `aarch64-linux-gnu-` prefix; bridge
# the two with symlinks so clang picks the cross ld/as/ar instead of the host
# /usr/bin/ld (which rejects aarch64 emulations).
RUN set -eux; \
    for tool in ld ld.bfd as ar nm objcopy objdump ranlib readelf strip; do \
        ln -sfn "aarch64-linux-gnu-${tool}" "/usr/bin/aarch64-unknown-linux-gnu-${tool}"; \
    done

RUN dnf clean all

COPY versions.yaml /etc/versions.yaml

# Install Rust official release
ENV RUSTUP_HOME=/usr/local/rustup
ENV CARGO_HOME=/usr/local/cargo
ENV PATH=/usr/local/cargo/bin:$PATH

RUN set -eux; \
    case "${TARGETARCH}" in \
    'amd64') \
        rustup_arch='x86_64-unknown-linux-gnu'; \
        rustup_sha256='4acc9acc76d5079515b46346a485974457b5a79893cfb01112423c89aeb5aa10'; \
        ;; \
    'arm64') \
        rustup_arch='aarch64-unknown-linux-gnu'; \
        rustup_sha256='9732d6c5e2a098d3521fca8145d826ae0aaa067ef2385ead08e6feac88fa5792'; \
        ;; \
    'ppc64le') \
        rustup_arch='powerpc64le-unknown-linux-gnu'; \
        rustup_sha256='4bfff85bd3967d988e14567aa9cc6ab0ea386f0ffeff0f9f14d23f0103bf1f97'; \
        ;; \
    's390x') \
        rustup_arch='s390x-unknown-linux-gnu'; \
        rustup_sha256='66c2c132428b6b77803facb02cbdf33b89d20c00bd20da142be8cb651f2e7cd8'; \
        ;; \
    *) echo >&2 "error: unsupported architecture '${TARGETARCH}'"; exit 1 ;; \
    esac; \
    \
    url="https://static.rust-lang.org/rustup/archive/1.29.0/${rustup_arch}/rustup-init"; \
    wget --progress=dot:giga "$url"; \
    echo "${rustup_sha256} *rustup-init" | sha256sum -c -; \
    \
    chmod +x rustup-init; \
    ./rustup-init -y \
        --no-modify-path \
        --profile minimal \
        --component clippy --component llvm-tools --component rustfmt \
        --default-toolchain $(yq -r .rust.version /etc/versions.yaml) --default-host ${rustup_arch}; \
    rm rustup-init; \
    chmod -R a+w $RUSTUP_HOME $CARGO_HOME; \
    \
    rustup --version; \
    cargo --version; \
    rustc --version

# Install cross-compilation sysroot for arm64 (amd64 host only).
# Mirrors images/calico-go-build/Dockerfile: target-arch RPMs are downloaded
# from AlmaLinux mirrors and extracted under /usr/<triple>/sys-root, with
# clang acting as the cross compiler via --target / --sysroot.
RUN set -eux; \
    if [ "${TARGETARCH}" = "amd64" ]; then \
        arch=aarch64; \
        triple=aarch64-linux-gnu; \
        sysroot="/usr/${triple}/sys-root"; \
        mkdir -p "${sysroot}"; \
        tmpdir=$(mktemp -d); \
        dnf download --destdir="${tmpdir}" \
            --repofrompath="alma-${arch}-baseos,https://repo.almalinux.org/almalinux/9/BaseOS/${arch}/os/" \
            --repofrompath="alma-${arch}-appstream,https://repo.almalinux.org/almalinux/9/AppStream/${arch}/os/" \
            --repo="alma-${arch}-baseos" \
            --repo="alma-${arch}-appstream" \
            --setopt="alma-${arch}-baseos.gpgcheck=1" \
            --setopt="alma-${arch}-baseos.gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-AlmaLinux-9" \
            --setopt="alma-${arch}-appstream.gpgcheck=1" \
            --setopt="alma-${arch}-appstream.gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-AlmaLinux-9" \
            --forcearch="${arch}" \
            gcc libgcc \
            glibc glibc-devel \
            kernel-headers; \
        for rpm in "${tmpdir}"/*.rpm; do \
            rpm2cpio "${rpm}" | cpio -idm -D "${sysroot}"; \
        done; \
        rm -rf "${tmpdir}"; \
        # Replicate AlmaLinux's usrmerge layout (/lib64 -> usr/lib64) so absolute
        # paths in glibc's linker scripts resolve correctly under --sysroot.
        for dir in lib lib64; do \
            if [ -d "${sysroot}/${dir}" ] && [ ! -L "${sysroot}/${dir}" ]; then \
                mkdir -p "${sysroot}/usr/${dir}"; \
                cp -a "${sysroot}/${dir}/." "${sysroot}/usr/${dir}/"; \
                rm -rf "${sysroot}/${dir}"; \
            fi; \
            if [ ! -e "${sysroot}/${dir}" ]; then \
                ln -s "usr/${dir}" "${sysroot}/${dir}"; \
            fi; \
        done; \
        rustup target add aarch64-unknown-linux-gnu; \
        \
        # Bake linker / sysroot config so `cargo build --target aarch64-unknown-linux-gnu`
        # works with no extra env vars.
        mkdir -p "${CARGO_HOME}"; \
        printf '%s\n' \
            '[target.aarch64-unknown-linux-gnu]' \
            'linker = "clang"' \
            'rustflags = ["-C", "link-arg=--target=aarch64-linux-gnu", "-C", "link-arg=--sysroot=/usr/aarch64-linux-gnu/sys-root", "-C", "link-arg=-fuse-ld=lld"]' \
            > "${CARGO_HOME}/config.toml"; \
    fi

# Install Protocol Buffers compiler
RUN set -eux; \
    case "${TARGETARCH}" in \
    'amd64') \
        url="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip"; \
        ;; \
    'arm64') \
        url="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-aarch_64.zip"; \
        ;; \
    'ppc64le') \
        url="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-ppcle_64.zip"; \
        ;; \
    's390x') \
        url="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-s390_64.zip"; \
        ;; \
    *) echo >&2 "error: unsupported architecture '${TARGETARCH}'"; exit 1 ;; \
    esac; \
    \
    curl -sfL "$url" | bsdtar -xf - -C /usr && chmod +x /usr/bin/protoc; \
    \
    protoc --version

FROM scratch

ENV CC=clang
ENV CXX=clang++
ENV RUSTUP_HOME=/usr/local/rustup
ENV CARGO_HOME=/usr/local/cargo
ENV PATH=/usr/local/cargo/bin:$PATH

COPY --from=builder / /
