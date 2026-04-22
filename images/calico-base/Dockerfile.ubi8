FROM registry.access.redhat.com/ubi8/ubi-minimal:latest AS ubi

ARG LDSONAME
ARG TARGETARCH

RUN microdnf upgrade -y

# Prepare a rootfs for necessary files from UBI.
# Symbolic links are preserved.
RUN mkdir -p /rootfs/lib64 /rootfs/etc

# Copy dynamic loader and symbolic links.
# For s390x architecture, modify the /lib/${LDSONAME} symlink to ../lib64/${LDSONAME}
# instead of /usr/lib64 as the /usr/lib64 directory is not included in our base.
RUN cp /lib64/ld-2.28.so /rootfs/lib64/ld-2.28.so
RUN set -eux; \
    cp -a /lib64/${LDSONAME} /rootfs/lib64/${LDSONAME}; \
    if [ -f /lib/${LDSONAME} ]; then \
        mkdir -p /rootfs/lib && cp -a /lib/${LDSONAME} /rootfs/lib/${LDSONAME}; \
        if [ "${TARGETARCH}" = "s390x" ]; then \
            ln -sf ../lib64/${LDSONAME} /rootfs/lib/${LDSONAME}; \
        fi \
    fi

# Required external C dependencies for CGO builds.
RUN cp /lib64/libc.so.6 /rootfs/lib64/libc.so.6
RUN cp /lib64/libpthread.so.0 /rootfs/lib64/libpthread.so.0
RUN cp /lib64/libresolv.so.2 /rootfs/lib64/libresolv.so.2

# Standard C library dependencies required for C/C++ applications.
RUN cp /lib64/libdl.so.2 /rootfs/lib64/libdl.so.2
RUN cp /lib64/libgcc_s.so.1 /rootfs/lib64/libgcc_s.so.1
RUN cp /lib64/libm.so.6 /rootfs/lib64/libm.so.6
RUN cp /lib64/librt.so.1 /rootfs/lib64/librt.so.1
RUN cp /lib64/libstdc++.so.6 /rootfs/lib64/libstdc++.so.6

# Glibc NSS plugins and config files.
# Use our customized configuration, since the base image only includes the dns and files plugins.
COPY nsswitch.conf /rootfs/etc/nsswitch.conf

RUN cp /lib64/libnss_dns.so.2 /rootfs/lib64/libnss_dns.so.2
RUN cp /lib64/libnss_files.so.2 /rootfs/lib64/libnss_files.so.2

RUN cp /etc/host.conf /rootfs/etc/host.conf
RUN cp /etc/hosts /rootfs/etc/hosts
RUN cp /etc/networks /rootfs/etc/networks

# Copy base image release info.
RUN cp /etc/os-release /rootfs/etc/os-release

FROM scratch AS source

COPY --from=ubi /rootfs /

# Verify if glibc can be properly loaded.
# This check ensures that the dynamic loader and symbolic links are copied correctly.
RUN ["/lib64/libc.so.6"]

# tmp.tar has a /tmp with the correct permissions 01777.
ADD tmp.tar /

COPY licenses /licenses/

FROM scratch

COPY --from=source / /
