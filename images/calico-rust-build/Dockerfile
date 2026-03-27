FROM almalinux:9 AS builder

ARG TARGETARCH

ARG PROTOC_VERSION=34.1

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
