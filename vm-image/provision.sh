#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Provisioner for the general CI VM base image (family: ci-base) -- for ANY CI
# job that needs docker/go/kubectl/etc on a GCE VM, not just the kind rig. Runs
# once, as the startup-script of a throwaway builder VM; the builder's disk is
# then snapshotted into a reusable image (see README.md). Bakes in the common CI
# toolchain so a VM created from it boots ready and jobs do zero installs -- the
# whole point is to move the per-run install time to build time.
#
# It publishes /var/run/provision-done when finished, which the image-build script
# polls over SSH before stopping the builder.
set -xeuo pipefail
export DEBIAN_FRONTEND=noninteractive

# Versions are injected as a preamble by build-image.sh, read from
# images/calico-go-build/versions.yaml -- the same file the calico/go-build image
# is built from, so a VM job sees the Go that job would have seen in go-build.
# Required rather than defaulted: silently baking a stale Go into the image is the
# exact drift this indirection exists to prevent, so run this via build-image.sh.
GO_VERSION="${GO_VERSION:?set by build-image.sh from images/calico-go-build/versions.yaml}"
GO_SHA256="${GO_SHA256:?set by build-image.sh from images/calico-go-build/versions.yaml}"
GO_BUILD_IMAGE="${GO_BUILD_IMAGE:?set by build-image.sh from images/calico-go-build/versions.yaml}"
KUBECTL_VERSION="${KUBECTL_VERSION:?set by build-image.sh from images/calico-go-build/versions.yaml}"
# kind and gh are pinned in vm-image/versions.yaml.
KIND_VERSION="${KIND_VERSION:?set by build-image.sh from vm-image/versions.yaml}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:?set by build-image.sh from vm-image/versions.yaml}"
GH_VERSION="${GH_VERSION:?set by build-image.sh from vm-image/versions.yaml}"

APT=(apt-get -o DPkg::Lock::Timeout=600 -y)
retry() { local n=8; for i in $(seq 1 $n); do "$@" && return 0; echo "retry $i/$n: $*"; sleep 5; done; return 1; }

# --- base packages ----------------------------------------------------------
retry "${APT[@]}" update
retry "${APT[@]}" install --no-install-recommends ca-certificates curl gnupg git make iproute2 zstd

# --- docker (kind's runtime); ubuntu joins the docker group -----------------
install -m 0755 -d /etc/apt/keyrings
retry curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
codename=$(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")
printf 'Types: deb\nURIs: https://download.docker.com/linux/ubuntu\nSuites: %s\nComponents: stable\nSigned-By: /etc/apt/keyrings/docker.asc\n' "$codename" > /etc/apt/sources.list.d/docker.sources
retry "${APT[@]}" update
retry "${APT[@]}" install --no-install-recommends docker-ce docker-ce-cli containerd.io docker-buildx-plugin
usermod -a -G docker ubuntu
systemctl enable docker

# --- go / kind / kubectl / gh (all pinned; see the preamble above) -----------
curl -sSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz
# Verify against the checksum in versions.yaml, as the go-build Dockerfile does.
echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum -c -
rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz
# go + a per-user GOBIN on PATH for interactive + non-interactive shells.
printf 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin\n' > /etc/profile.d/go.sh

curl -sSLo /usr/local/bin/kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-amd64"
chmod 0755 /usr/local/bin/kind

curl -sSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl"
chmod 0755 /usr/local/bin/kubectl

curl -sSLo /tmp/gh.tgz "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_amd64.tar.gz"
tar -C /tmp -xzf /tmp/gh.tgz && install -m 0755 "/tmp/gh_${GH_VERSION}_linux_amd64/bin/gh" /usr/local/bin/gh

# --- inotify limits for 3 kind clusters (persist across boots) --------------
cat > /etc/sysctl.d/99-kind.conf <<EOF
fs.inotify.max_user_instances=512
fs.inotify.max_user_watches=524288
EOF

# --- pre-pull the heavy CI docker images so jobs skip the pull --------------
# Baked into the image's docker cache: the kind node image (3 clusters), the
# calico go-build image (make build-calico-image + the operator build), and
# registry:2 (the pull-through caches + the local helm registry). Both tags are
# injected by build-image.sh -- go-build from images/calico-go-build/versions.yaml,
# the kind node image from vm-image/versions.yaml -- so neither can drift.
systemctl start docker
PREPULL_IMAGES=(
  "$KIND_NODE_IMAGE"
  "$GO_BUILD_IMAGE"
  "registry:2"
)
for img in "${PREPULL_IMAGES[@]}"; do
  retry docker pull "$img" || echo "warn: could not pre-pull $img"
done

# Readiness marker for the image-build poll.
touch /var/run/provision-done
