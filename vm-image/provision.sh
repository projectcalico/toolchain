#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Provisioner for the ci-base VM image -- any CI job needing docker/go/kubectl on
# a GCE VM, not just the kind rig. Runs once as a throwaway builder's
# startup-script; its disk is then snapshotted into a reusable image (README.md).
# Baking the toolchain moves per-run install time to build time.
#
# Publishes /var/run/provision-done when done, which build-image.sh polls.
set -xeuo pipefail
export DEBIAN_FRONTEND=noninteractive

# Injected as a preamble by build-image.sh. Required, not defaulted: silently
# baking a stale Go is the drift this indirection exists to prevent.
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
# Checksum from versions.yaml, as the go-build Dockerfile does.
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
# Into the image's docker cache: the kind node image (3 clusters), go-build (the
# calico + operator builds), registry:2 (pull-through caches, local helm registry).
# Both tags are injected by build-image.sh, so neither can drift.
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
