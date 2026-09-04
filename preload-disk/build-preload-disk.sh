#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Build a GKE secondary-boot-disk image with calico/go-build PRELOADED, so argoci
# build pods start with it already on the node -- no multi-hundred-MB pull per run.
# Wraps Google's gke-disk-image-builder (github.com/ai-on-gke/tools): a throwaway
# builder VM pulls the images onto a data disk in the containerd image-streaming
# layout and snapshots it. Attach the result to a node pool (README.md).
#
#   PROJECT=<cluster-project> ZONE=us-central1-a \
#   GCS_PATH=gs://<log-bucket> ./build-preload-disk.sh
#
# Needs: gcloud authed with compute instance/disk/image create+delete and write to
# the log bucket in PROJECT; a local Go toolchain (runs `go run ./cli`); git; yq.
# Takes ~5-8 min (most of it the builder VM pulling the images).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
command -v yq >/dev/null || { echo "yq is required to read the pinned versions" >&2; exit 1; }

PROJECT="${PROJECT:-unique-caldron-775}"
ZONE="${ZONE:-us-central1-a}"
# The node pool pins this exact name in --secondary-boot-disk. Named off the
# go-build release tag, as the go-build images are, so the disk and the image it
# preloads match by eye. A pool binds an image at create time, so refreshing one is
# still a new image plus a pool update.
# Prefix is terse because GKE caps a secondary boot disk image name at 39 chars,
# well under GCE's 63: "gbp" leaves room for the full go-build tag even in its
# longest real shape (...-k8s1-37-0-rc-1-1, 34 chars), so the disk and the image it
# caches still match by eye. -m makes an over-long name fail here rather than when
# a node pool tries to attach it.
IMAGE_NAME="${IMAGE_NAME:-$("$REPO/hack/generate-image-name.sh" -p gbp -m 39)}"
DISK_SIZE_GB="${DISK_SIZE_GB:-20}"
# The builder VM's network. Not "default": that network is LEGACY in this project
# (10.240.0.0/16, no subnets at all), and the builder always asks for a subnetwork,
# so it fails validation with subnetworkResourceDoesNotExist. Point at the network
# the project's CI VMs actually use.
NETWORK="${NETWORK:-semaphore-autotest}"
SUBNET="${SUBNET:-semaphore-autotest}"
GCS_PATH="${GCS_PATH:?set GCS_PATH to a gs:// bucket/path for the builder logs}"
# Space-separated. Each MUST carry a tag or digest: the cache hits only the exact
# ref a pod requests, so a floating tag preloads nothing. Defaults to the go-build
# image this repo publishes, so the preloaded tag cannot drift from it.
if [ -z "${CONTAINER_IMAGES:-}" ]; then
  go_build_tag="$("$REPO/hack/generate-version-tag-name.sh" -f "$REPO/images/calico-go-build/versions.yaml")"
  CONTAINER_IMAGES="docker.io/calico/go-build:${go_build_tag}"
fi
# Pinned in versions.yaml so every run of a given toolchain commit uses the same
# builder. Override to test an upstream change; a branch or tag also works.
AI_ON_GKE_REF="${AI_ON_GKE_REF:-$(yq -r '.ai-on-gke.ref' "$HERE/versions.yaml")}"

log() { echo "[preload-disk] $*"; }

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# Not `clone --branch`: that takes only branch and tag names, and cannot check out
# a commit. init + fetch + checkout FETCH_HEAD accepts any of the three.
log "fetching gke-disk-image-builder (ai-on-gke/tools @ ${AI_ON_GKE_REF})"
git init -q "$workdir/tools"
git -C "$workdir/tools" remote add origin https://github.com/ai-on-gke/tools.git
git -C "$workdir/tools" sparse-checkout init --cone
git -C "$workdir/tools" sparse-checkout set gke-disk-image-builder
git -C "$workdir/tools" fetch -q --depth 1 --filter=blob:none origin "$AI_ON_GKE_REF"
git -C "$workdir/tools" checkout -q FETCH_HEAD
log "builder at $(git -C "$workdir/tools" rev-parse HEAD)"

args=(
  --project-name="$PROJECT"
  --image-name="$IMAGE_NAME"
  --zone="$ZONE"
  --gcs-path="$GCS_PATH"
  --disk-size-gb="$DISK_SIZE_GB"
  --network="$NETWORK"
  --subnet="$SUBNET"
)
for img in $CONTAINER_IMAGES; do args+=(--container-image="$img"); done

# Same pre-flight and release/branch split as vm-image. A node pool pins this name
# in --secondary-boot-disk but stores the resource PATH, not an image id, so a
# same-name recreate leaves its config valid -- safe for the only thing that should
# pin a branch image, a test pool. Release images are never replaced.
if gcloud compute images describe "$IMAGE_NAME" --project="$PROJECT" >/dev/null 2>&1; then
  if [ "${SEMAPHORE_GIT_REF_TYPE:-}" = "tag" ]; then
    log "image $IMAGE_NAME already exists in $PROJECT -- this release is already built."
    log "to rebuild it: gcloud compute images delete $IMAGE_NAME --project=$PROJECT"
    log "or set IMAGE_NAME=<name> to build under a different name."
    exit 1
  fi
  log "replacing existing branch image $IMAGE_NAME"
  log "note: existing nodes keep their copy (the disk attaches at node creation)."
  log "      Only nodes created before this build finishes miss the cache."
  gcloud --quiet compute images delete "$IMAGE_NAME" --project="$PROJECT"
fi

log "building disk image ${IMAGE_NAME} in ${PROJECT} (network ${NETWORK}/${SUBNET})"
log "preloading: ${CONTAINER_IMAGES}"
( cd "$workdir/tools/gke-disk-image-builder" && go run ./cli "${args[@]}" )

log "done: image ${IMAGE_NAME} (project ${PROJECT}, ${#IMAGE_NAME}/39 chars)"
log "attach it to a node pool (image streaming required; cross-project is fine --"
log "see README.md for the compute.imageUser grants a cluster elsewhere needs):"
log "  gcloud container node-pools create <pool> --cluster=<cluster> --location=<loc> \\"
log "    --enable-image-streaming \\"
log "    --secondary-boot-disk=disk-image=projects/${PROJECT}/global/images/${IMAGE_NAME},mode=CONTAINER_IMAGE_CACHE"
