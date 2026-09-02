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

GO_VERSION="${GO_VERSION:-1.26.5}"   # keep in sync with the rig go.mod

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

# --- go / kind / kubectl / gh (what run-kindrig.sh installs today) ----------
curl -sSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz
rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz
# go + a per-user GOBIN on PATH for interactive + non-interactive shells.
printf 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin\n' > /etc/profile.d/go.sh

curl -sSLo /usr/local/bin/kind "https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64"
chmod 0755 /usr/local/bin/kind

curl -sSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/$(curl -sSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod 0755 /usr/local/bin/kubectl

gh_ver="$(curl -sSL https://api.github.com/repos/cli/cli/releases/latest | grep -oP '"tag_name":\s*"v\K[0-9.]+')"
curl -sSLo /tmp/gh.tgz "https://github.com/cli/cli/releases/latest/download/gh_${gh_ver}_linux_amd64.tar.gz"
tar -C /tmp -xzf /tmp/gh.tgz && install -m 0755 /tmp/gh_*/bin/gh /usr/local/bin/gh

# --- inotify limits for 3 kind clusters (persist across boots) --------------
cat > /etc/sysctl.d/99-kind.conf <<EOF
fs.inotify.max_user_instances=512
fs.inotify.max_user_watches=524288
EOF

# --- pre-pull the heavy CI docker images so jobs skip the pull --------------
# Baked into the image's docker cache: the kind node image (3 clusters), the
# calico go-build image (make build-calico-image + the operator build), and
# registry:2 (the pull-through caches + the local helm registry). POC: tags are
# hardcoded -- kindest/node from lib/kind DefaultNodeImage, go-build from
# metadata.mk GO_BUILD_VER; re-run build-image.sh to refresh when they bump.
systemctl start docker
PREPULL_IMAGES=(
  "kindest/node:v1.33.7@sha256:d26ef333bdb2cbe9862a0f7c3803ecc7b4303d8cea8e814b481b09949d353040"
  "calico/go-build:1.26.5-llvm21.1.8-k8s1.37.0-beta.0-1"
  "registry:2"
)
for img in "${PREPULL_IMAGES[@]}"; do
  retry docker pull "$img" || echo "warn: could not pre-pull $img"
done

# Readiness marker for the image-build poll.
touch /var/run/provision-done
