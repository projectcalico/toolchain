#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Build a GKE secondary-boot-disk image with the calico/go-build image PRELOADED,
# so argoci build pods (e.g. the kind-rig build-artifacts step) start with go-build
# already on the node -- no multi-hundred-MB image pull per run. Wraps Google's
# gke-disk-image-builder (github.com/ai-on-gke/tools, under gke-disk-image-builder):
# it spins up a throwaway builder VM, pulls the images onto a data disk in the
# containerd image-streaming layout, snapshots that disk into a GCE image, and
# cleans the builder up. Attach the image to a node pool as a secondary boot disk
# (see README.md).
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
# The node pool pins this exact image name in --secondary-boot-disk. Named off the
# go-build release tag, exactly as the go-build images are, so the disk and the
# go-build image it preloads are matchable by eye (dots become hyphens: GCE names
# are RFC1035). GCE images are immutable and a pool binds one at create time, so
# refreshing a pool is still a NEW image + a pool update -- but the new image now
# has a name that says which release it belongs to.
IMAGE_NAME="${IMAGE_NAME:-$("$REPO/hack/generate-image-name.sh" -p go-build-preload)}"
DISK_SIZE_GB="${DISK_SIZE_GB:-20}"
GCS_PATH="${GCS_PATH:?set GCS_PATH to a gs:// bucket/path for the builder logs}"
# Images to preload, space-separated. Each MUST carry a tag or digest -- the cache
# hits only the exact ref a pod requests, so a floating tag preloads nothing.
# The default is the go-build image this repo publishes, resolved from
# images/calico-go-build/versions.yaml by the same helper the release tagging and
# vm-image/build-image.sh use, so the preloaded tag cannot drift from it.
if [ -z "${CONTAINER_IMAGES:-}" ]; then
  go_build_tag="$("$REPO/hack/generate-version-tag-name.sh" -f "$REPO/images/calico-go-build/versions.yaml")"
  CONTAINER_IMAGES="docker.io/calico/go-build:${go_build_tag}"
fi
# The ai-on-gke/tools commit the builder is fetched at, pinned in versions.yaml so
# every run of a given toolchain commit builds the disk with the same builder.
# Override to test an upstream change; a branch or tag name works too.
AI_ON_GKE_REF="${AI_ON_GKE_REF:-$(yq -r '.ai-on-gke.ref' "$HERE/versions.yaml")}"

log() { echo "[preload-disk] $*"; }

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# Fetch by ref rather than `clone --branch`: --branch takes only branch and tag
# names, so it cannot check out the pinned commit (and upstream publishes no tags).
# init + fetch + checkout FETCH_HEAD accepts a SHA, a branch or a tag alike.
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
)
for img in $CONTAINER_IMAGES; do args+=(--container-image="$img"); done

# Same pre-flight as vm-image: a deterministic name collides on a rebuild of the
# same release, and finding that out after the builder VM has run is expensive.
if gcloud compute images describe "$IMAGE_NAME" --project="$PROJECT" >/dev/null 2>&1; then
  log "image $IMAGE_NAME already exists in $PROJECT -- this release is already built."
  log "to rebuild it: gcloud compute images delete $IMAGE_NAME --project=$PROJECT"
  log "or set IMAGE_NAME=<name> to build under a different name."
  exit 1
fi

log "building disk image ${IMAGE_NAME} in ${PROJECT}"
log "preloading: ${CONTAINER_IMAGES}"
( cd "$workdir/tools/gke-disk-image-builder" && go run ./cli "${args[@]}" )

log "done: image ${IMAGE_NAME} (project ${PROJECT})"
log "attach it to a node pool (must be in the cluster's project; image streaming required):"
log "  gcloud container node-pools create <pool> --cluster=<cluster> --location=<loc> \\"
log "    --enable-image-streaming \\"
log "    --secondary-boot-disk=disk-image=projects/${PROJECT}/global/images/${IMAGE_NAME},mode=CONTAINER_IMAGE_CACHE"
