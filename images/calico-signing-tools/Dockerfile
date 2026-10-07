# Pinned cosign and syft on scratch, with the TLS trust store both need to reach
# registries. No entrypoint: callers run `cosign ...` or `syft ...` directly.
ARG UBI_VERSION=ubi9

FROM registry.access.redhat.com/${UBI_VERSION}/ubi-minimal:latest AS rootfs

ARG TARGETARCH
ARG COSIGN_VERSION
ARG COSIGN_SHA256
ARG SYFT_VERSION
ARG SYFT_SHA256

RUN microdnf install -y tar gzip && microdnf clean all

RUN set -eux; \
    mkdir -p /rootfs/etc/ssl/certs /rootfs/tmp /rootfs/usr/local/bin; \
    cp /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem /rootfs/etc/ssl/certs/ca-certificates.crt; \
    chmod 1777 /rootfs/tmp; \
    curl -sfL -o /rootfs/usr/local/bin/cosign \
        https://github.com/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-${TARGETARCH}; \
    echo "${COSIGN_SHA256} */rootfs/usr/local/bin/cosign" | sha256sum -c -; \
    chmod +x /rootfs/usr/local/bin/cosign; \
    curl -sfL -o /tmp/syft.tar.gz \
        https://github.com/anchore/syft/releases/download/v${SYFT_VERSION}/syft_${SYFT_VERSION}_linux_${TARGETARCH}.tar.gz; \
    echo "${SYFT_SHA256} */tmp/syft.tar.gz" | sha256sum -c -; \
    tar -xzf /tmp/syft.tar.gz -C /rootfs/usr/local/bin syft

FROM scratch

COPY --from=rootfs /rootfs /

# cosign keeps its sigstore trust root under $HOME, which must be writable by a
# uid with no /etc/passwd entry.
ENV HOME=/tmp
ENV PATH=/usr/local/bin

USER 65532
