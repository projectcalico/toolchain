FROM fedora:43 AS qemu

ARG QEMU_VERSION

RUN dnf install -y qemu-user-static-${QEMU_VERSION}

FROM scratch AS source

ARG TARGETARCH

COPY --from=qemu /usr/bin/qemu-aarch64-static /usr/bin/qemu-aarch64
COPY --from=qemu /usr/bin/qemu-ppc64le-static /usr/bin/qemu-ppc64le
COPY --from=qemu /usr/bin/qemu-s390x-static /usr/bin/qemu-s390x
COPY --from=qemu /usr/bin/qemu-x86_64-static /usr/bin/qemu-x86_64

COPY bin/binfmt-${TARGETARCH} /usr/bin/binfmt

FROM scratch

COPY --from=source / /

ENTRYPOINT [ "/usr/bin/binfmt" ]
