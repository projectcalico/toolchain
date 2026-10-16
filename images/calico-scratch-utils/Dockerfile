# The static scratch-utils binary on scratch. Beyond itself it needs exactly two
# things, so they are prepared from UBI the same way calico-base builds its rootfs:
# a TLS trust store (every GCP API call fails without one) and a writable /tmp
# (the compute SA key is written there). No shell, no package manager -- workflow
# steps run `scratch-utils <subcommand>` directly. The binary is built by ../cmd
# and copied in by images/Makefile; this Dockerfile does no compilation.
ARG UBI_VERSION=ubi9

FROM registry.access.redhat.com/${UBI_VERSION}/ubi-minimal:latest AS rootfs
RUN mkdir -p /rootfs/etc/ssl/certs /rootfs/tmp \
    && cp /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem \
          /rootfs/etc/ssl/certs/ca-certificates.crt \
    && chmod 1777 /rootfs/tmp

FROM scratch

ARG TARGETARCH

COPY --from=rootfs /rootfs /
COPY bin/scratch-utils-${TARGETARCH} /usr/local/bin/scratch-utils

# Numeric so no /etc/passwd is needed; same uid distroless nonroot uses.
USER 65532

ENTRYPOINT ["/usr/local/bin/scratch-utils"]
