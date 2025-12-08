ARG TARGETARCH=${TARGETARCH}

FROM calico/bpftool:v7.4.0 AS bpftool

FROM registry.access.redhat.com/ubi9/ubi:latest AS ubi

ARG TARGETARCH

ARG CONTAINERREGISTRY_VERSION=v0.20.7
ARG CONTROLLER_TOOLS_VERSION=v0.18.0
ARG GO_LINT_VERSION=v2.8.0
ARG MOCKERY_VERSION=3.5.5
ARG PROTOC_VERSION=33.4
ARG YQ_VERSION=v4.50.1

ENV PATH=/usr/local/go/bin:$PATH

# Install system dependencies
RUN dnf upgrade -y && dnf install -y \
    autoconf \
    automake \
    bsdtar \
    gcc \
    gcc-c++ \
    git \
    iputils \
    jq \
    libcurl-devel \
    libpcap-devel \
    libtool \
    make \
    openssh-clients \
    patch \
    pcre-devel \
    pkg-config \
    wget \
    xz \
    zip

# Install system dependencies that are not in UBI repos
COPY almalinux/RPM-GPG-KEY-AlmaLinux-9 /etc/pki/rpm-gpg/RPM-GPG-KEY-AlmaLinux-9
COPY almalinux/almalinux*.repo /etc/yum.repos.d/

# Install yq and copy versions.yaml
COPY versions.yaml /etc/versions.yaml

RUN set -eux; \
    dnf --enablerepo=extras install -y epel-release && \
    dnf install -y yq

RUN set -eux; \
    llvm_version=$(yq -r .llvm.version /etc/versions.yaml); \
    dnf --enablerepo=appstream,baseos,crb install -y \
    clang-${llvm_version} \
    elfutils-libelf-devel \
    iproute-devel \
    iproute-tc \
    libbpf-devel \
    llvm-${llvm_version}

RUN set -eux; \
    if [ "${TARGETARCH}" = "amd64" ]; then \
        dnf --enablerepo=appstream,baseos,crb install -y \
            mingw64-gcc; \
    fi

# Install Google Cloud SDK for GCR/GAR
# See https://cloud.google.com/sdk/docs/install#rpm for installation details
COPY google/google-cloud-sdk.repo /etc/yum.repos.d/google-cloud-sdk.repo

RUN set -eux; \
    if [ "${TARGETARCH}" = "amd64" ] || [ "${TARGETARCH}" = "arm64" ]; then \
        dnf --enablerepo=google-cloud-cli install -y \
            google-cloud-cli \
            google-cloud-cli-docker-credential-gcr; \
    fi

RUN dnf clean all

# Install Go official release
RUN set -eux; \
    golang_version=$(yq -r .golang.version /etc/versions.yaml); \
    golang_checksum=$(yq -r .golang.checksum.sha256.${TARGETARCH} /etc/versions.yaml); \
    url=; \
    case "${TARGETARCH}" in \
    'amd64') \
        url="https://dl.google.com/go/go${golang_version}.linux-amd64.tar.gz"; \
        sha256="${golang_checksum}"; \
        ;; \
    'arm64') \
        url="https://dl.google.com/go/go${golang_version}.linux-arm64.tar.gz"; \
        sha256="${golang_checksum}"; \
        ;; \
    'ppc64le') \
        url="https://dl.google.com/go/go${golang_version}.linux-ppc64le.tar.gz"; \
        sha256="${golang_checksum}"; \
        ;; \
    's390x') \
        url="https://dl.google.com/go/go${golang_version}.linux-s390x.tar.gz"; \
        sha256="${golang_checksum}"; \
        ;; \
    *) echo >&2 "error: unsupported architecture '${TARGETARCH}'"; exit 1 ;; \
    esac; \
    \
    wget -O go.tgz.asc "$url.asc"; \
    wget -O go.tgz "$url" --progress=dot:giga; \
    echo "$sha256 *go.tgz" | sha256sum -c -; \
    \
    # https://github.com/golang/go/issues/14739#issuecomment-324767697
    GNUPGHOME="$(mktemp -d)"; export GNUPGHOME; \
    # https://www.google.com/linuxrepositories/
    gpg --batch --keyserver keyserver.ubuntu.com --recv-keys 'EB4C 1BFD 4F04 2F6D DDCC  EC91 7721 F63B D38B 4796'; \
    # let's also fetch the specific subkey of that key explicitly that we expect "go.tgz.asc" to be signed by, just to make sure we definitely have it
    gpg --batch --keyserver keyserver.ubuntu.com --recv-keys '2F52 8D36 D67B 69ED F998  D857 78BD 6547 3CB3 BD13'; \
    gpg --batch --verify go.tgz.asc go.tgz; \
    gpgconf --kill all; \
    rm -rf "$GNUPGHOME" go.tgz.asc; \
    \
    tar -C /usr/local -xzf go.tgz; \
    rm -f go.tgz*; \
    \
    go version

# don't auto-upgrade the gotoolchain
# https://github.com/docker-library/golang/issues/472
ENV GOTOOLCHAIN=local

ENV GOPATH=/go
ENV PATH=$GOPATH/bin:$PATH
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 1777 "$GOPATH"

# su-exec is used by the entrypoint script to execute the user's command with the right UID/GID.
RUN set -eux; \
    curl -sfL https://raw.githubusercontent.com/ncopa/su-exec/master/su-exec.c -o /tmp/su-exec.c; \
    gcc -Wall -O2 /tmp/su-exec.c -o /usr/bin/su-exec; \
    rm -f /tmp/su-exec.c

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

# Install Go utilities

# controller-gen is used for generating CRD files.
COPY patches/controller-gen-Support-Calico-NumOrString-types.patch /tmp/controller-tools/calico.patch

RUN set -eux; \
    curl -sfL https://github.com/kubernetes-sigs/controller-tools/archive/refs/tags/${CONTROLLER_TOOLS_VERSION}.tar.gz | tar xz --strip-components 1 -C /tmp/controller-tools; \
    cd /tmp/controller-tools && patch -p1 < calico.patch && CGO_ENABLED=0 go build -o /usr/local/bin/controller-gen -v -buildvcs=false \
        -ldflags "-X sigs.k8s.io/controller-tools/pkg/version.version=${CONTROLLER_TOOLS_VERSION} -s -w" ./cmd/controller-gen; \
    rm -fr /tmp/controller-tools

# crane is needed for our release targets to copy images from the dev registries to the release registries.
RUN set -eux; \
    if [ "${TARGETARCH}" = "amd64" ]; then \
        curl -sfL https://github.com/google/go-containerregistry/releases/download/${CONTAINERREGISTRY_VERSION}/go-containerregistry_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin crane; \
    fi

RUN curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b /usr/local/bin $GO_LINT_VERSION

# Install necessary Kubernetes binaries used in tests.
RUN set -eux; \
    k8s_version=$(yq -r .kubernetes.version /etc/versions.yaml); \
    curl -sfL https://dl.k8s.io/v${k8s_version}/bin/linux/${TARGETARCH}/kube-apiserver -o /usr/local/bin/kube-apiserver && chmod +x /usr/local/bin/kube-apiserver && \
    curl -sfL https://dl.k8s.io/release/v${k8s_version}/bin/linux/${TARGETARCH}/kubectl -o /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl && \
    curl -sfL https://dl.k8s.io/v${k8s_version}/bin/linux/${TARGETARCH}/kube-controller-manager -o /usr/local/bin/kube-controller-manager && chmod +x /usr/local/bin/kube-controller-manager

RUN set -eux; \
    case "${TARGETARCH}" in \
    'amd64') \
        curl -sfL https://github.com/vektra/mockery/releases/download/v${MOCKERY_VERSION}/mockery_${MOCKERY_VERSION}_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin --extract mockery; \
        ;; \
    'arm64') \
        curl -sfL https://github.com/vektra/mockery/releases/download/v${MOCKERY_VERSION}/mockery_${MOCKERY_VERSION}_Linux_arm64.tar.gz | tar xz -C /usr/local/bin --extract mockery; \
        ;; \
    *) echo >&2 "warning: unsupported architecture '${TARGETARCH}'" ;; \
    esac

# Install go programs that we rely on
# Install ginkgo v2 as ginkgo2 and keep ginkgo v1 as ginkgo
RUN set -eux; \
    k8s_libs_version=$(yq -r .kubernetes.version /etc/versions.yaml | sed 's/^1/0/'); \
    go install github.com/onsi/ginkgo/v2/ginkgo@v2.27.5 && mv /go/bin/ginkgo /go/bin/ginkgo2 && \
    go install github.com/onsi/ginkgo/ginkgo@v1.16.5 && \
    go install github.com/jstemmer/go-junit-report@v1.0.0 && \
    go install github.com/wadey/gocovmerge@v0.0.0-20160331181800-b5bfa59ec0ad && \
    go install golang.org/x/tools/cmd/goimports@v0.41.0 && \
    go install golang.org/x/tools/cmd/stringer@v0.41.0 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0 && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 && \
    go install gotest.tools/gotestsum@v1.13.0 && \
    go install k8s.io/code-generator/cmd/client-gen@v${k8s_libs_version} && \
    go install k8s.io/code-generator/cmd/conversion-gen@v${k8s_libs_version} && \
    go install k8s.io/code-generator/cmd/deepcopy-gen@v${k8s_libs_version} && \
    go install k8s.io/code-generator/cmd/defaulter-gen@v${k8s_libs_version} && \
    go install k8s.io/code-generator/cmd/informer-gen@v${k8s_libs_version} && \
    go install k8s.io/code-generator/cmd/lister-gen@v${k8s_libs_version} && \
    go install k8s.io/kube-openapi/cmd/openapi-gen@v0.0.0-20251125145642-4e65d59e963e && \
    go install mvdan.cc/gofumpt@v0.9.2

# Cleanup module cache after we have built and installed all Go utilities
RUN go clean -modcache && go clean -cache

# Ensure that everything under the GOPATH is writable by everyone
RUN chmod -R 777 $GOPATH

# Do not create mail box.
RUN sed -i 's/^CREATE_MAIL_SPOOL=yes/CREATE_MAIL_SPOOL=no/' /etc/default/useradd

# Allow validated remote servers
COPY ssh_known_hosts /etc/ssh/ssh_known_hosts

# Copy bpftool for Felix UT/FV.
COPY --from=bpftool /bpftool /usr/bin

# Copy semvalidator release tool.
COPY bin/semvalidator-${TARGETARCH} /usr/local/bin/semvalidator

COPY entrypoint.sh /usr/local/bin/entrypoint.sh

# Squash into a single layer
FROM scratch

ENV GOPATH=/go
ENV GOTOOLCHAIN=local
ENV PATH=$GOPATH/bin:/usr/local/go/bin:$PATH

COPY --from=ubi / /

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
