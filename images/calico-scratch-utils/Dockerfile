# A lean CI-tools image: the single static scratch-utils binary (createvm/deletevm/
# secret/runonvm subcommands) on distroless/static, which carries the CA certs the
# GCP API TLS needs. No gcloud, no bash, no shell -- the ArgoCI kind-rig steps run
# `scratch-utils <subcommand>` directly. The binary is built by ../cmd (make -C cmd build)
# and copied in by images/Makefile; this Dockerfile does no compilation.
FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETARCH

COPY bin/scratch-utils-${TARGETARCH} /usr/local/bin/scratch-utils

ENTRYPOINT ["/usr/local/bin/scratch-utils"]
