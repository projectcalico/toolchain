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

PROJECT="${PROJECT:-unique-caldron-775}"
ZONE="${ZONE:-us-central1-a}"
# The node pool pins this exact image name in --secondary-boot-disk. Default is
# timestamped: GCE images are immutable and a pool binds one at create time, so a
# refresh is a NEW image + a pool update, not an in-place change. Override
# IMAGE_NAME for a stable name if your workflow recreates the pool each time.
IMAGE_NAME="${IMAGE_NAME:-go-build-preload-$(date +%Y%m%d-%H%M%S)}"
DISK_SIZE_GB="${DISK_SIZE_GB:-20}"
GCS_PATH="${GCS_PATH:?set GCS_PATH to a gs:// bucket/path for the builder logs}"
# Images to preload, space-separated. Each MUST carry a tag or digest -- the cache
# hits only the exact ref a pod requests, so a floating tag preloads nothing.
# The default is the go-build image this repo publishes, resolved from
# images/calico-go-build/versions.yaml by the same helper the release tagging and
# vm-image/build-image.sh use, so the preloaded tag cannot drift from it.
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
if [ -z "${CONTAINER_IMAGES:-}" ]; then
  command -v yq >/dev/null || { echo "yq is required to resolve the go-build tag" >&2; exit 1; }
  go_build_tag="$("$REPO/hack/generate-version-tag-name.sh" -f "$REPO/images/calico-go-build/versions.yaml")"
  CONTAINER_IMAGES="docker.io/calico/go-build:${go_build_tag}"
fi
# ai-on-gke/tools ref the builder is fetched at. Pin a commit SHA for reproducible
# builds; `main` is the moving default.
AI_ON_GKE_REF="${AI_ON_GKE_REF:-main}"

log() { echo "[preload-disk] $*"; }

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

log "fetching gke-disk-image-builder (ai-on-gke/tools @ ${AI_ON_GKE_REF})"
git clone --quiet --depth 1 --branch "$AI_ON_GKE_REF" --filter=blob:none --sparse \
  https://github.com/ai-on-gke/tools.git "$workdir/tools"
git -C "$workdir/tools" sparse-checkout set gke-disk-image-builder

args=(
  --project-name="$PROJECT"
  --image-name="$IMAGE_NAME"
  --zone="$ZONE"
  --gcs-path="$GCS_PATH"
  --disk-size-gb="$DISK_SIZE_GB"
)
for img in $CONTAINER_IMAGES; do args+=(--container-image="$img"); done

log "building disk image ${IMAGE_NAME} in ${PROJECT}"
log "preloading: ${CONTAINER_IMAGES}"
( cd "$workdir/tools/gke-disk-image-builder" && go run ./cli "${args[@]}" )

log "done: image ${IMAGE_NAME} (project ${PROJECT})"
log "attach it to a node pool (must be in the cluster's project; image streaming required):"
log "  gcloud container node-pools create <pool> --cluster=<cluster> --location=<loc> \\"
log "    --enable-image-streaming \\"
log "    --secondary-boot-disk=disk-image=projects/${PROJECT}/global/images/${IMAGE_NAME},mode=CONTAINER_IMAGE_CACHE"
