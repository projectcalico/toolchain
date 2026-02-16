FROM almalinux:9 AS builder

ARG TARGETARCH

ARG PROTOC_VERSION=33.5

RUN dnf upgrade -y && dnf install -y epel-release && \
    dnf install -y yq

RUN dnf install -y \
    bsdtar \
    gcc \
    make \
    wget

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
        rustup_sha256='20a06e644b0d9bd2fbdbfd52d42540bdde820ea7df86e92e533c073da0cdd43c'; \
        ;; \
    'arm64') \
        rustup_arch='aarch64-unknown-linux-gnu'; \
        rustup_sha256='e3853c5a252fca15252d07cb23a1bdd9377a8c6f3efa01531109281ae47f841c'; \
        ;; \
    'ppc64le') \
        rustup_arch='powerpc64le-unknown-linux-gnu'; \
        rustup_sha256='acd89c42b47c93bd4266163a7b05d3f26287d5148413c0d47b2e8a7aa67c9dc0'; \
        ;; \
    's390x') \
        rustup_arch='s390x-unknown-linux-gnu'; \
        rustup_sha256='726b7fd5d8805e73eab4a024a2889f8859d5a44e36041abac0a2436a52d42572'; \
        ;; \
    *) echo >&2 "error: unsupported architecture '${TARGETARCH}'"; exit 1 ;; \
    esac; \
    \
    url="https://static.rust-lang.org/rustup/archive/1.28.2/${rustup_arch}/rustup-init"; \
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

ENV RUSTUP_HOME=/usr/local/rustup
ENV CARGO_HOME=/usr/local/cargo
ENV PATH=/usr/local/cargo/bin:$PATH

COPY --from=builder / /
