# The static scratch-utils binary on distroless/static, which carries the CA certs
# GCP API TLS needs. No gcloud, no bash, no shell -- workflow steps run
# `scratch-utils <subcommand>` directly. The binary is built by ../cmd and copied in
# by images/Makefile; this Dockerfile does no compilation.
FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETARCH

COPY bin/scratch-utils-${TARGETARCH} /usr/local/bin/scratch-utils

ENTRYPOINT ["/usr/local/bin/scratch-utils"]
