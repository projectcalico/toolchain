#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Build a GKE secondary-boot-disk image with the images CI pulls most already on
# it, so pods skip the pull. Wraps Google's gke-disk-image-builder.
#
#   PROJECT=tigera-cc-dev GCS_PATH=gs://<bucket> ./build-preload-disk.sh
#
# Needs gcloud, git, yq and a Go toolchain. ~5-8 min. Attach the result at node
# pool CREATE time (there is no update flag for it), with image streaming on:
#
#   gcloud container node-pools create <pool> --cluster=<c> --location=<l> \
#     --enable-image-streaming \
#     --secondary-boot-disk=disk-image=projects/$PROJECT/global/images/<IMAGE>,mode=CONTAINER_IMAGE_CACHE
#
# A cluster in another project needs roles/compute.imageUser on this one for BOTH
# its default compute SA and its service-<num>@container-engine-robot SA. Missing
# either fails NODE creation, not pool creation, so it surfaces far from the cause.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
command -v yq >/dev/null || { echo "yq is required to read the pinned versions" >&2; exit 1; }

# The clusters' own project: cross-project works but needs the IAM noted above.
PROJECT="${PROJECT:-tigera-cc-dev}"
ZONE="${ZONE:-us-central1-a}"
# A node pool pins this exact name. GKE caps it at 39 chars (GCE allows 63) and
# only enforces that on attach, so -m fails here instead. "cic" rather than
# "ci-cache" because the full go-build tag needs the room: a release-candidate tag
# is 34 characters on its own.
IMAGE_NAME="${IMAGE_NAME:-$("$REPO/hack/generate-image-name.sh" -p cic -m 39)}"
DISK_SIZE_GB="${DISK_SIZE_GB:-20}"
# Override where "default" is a legacy network with no subnets (unique-caldron-775
# is one): the builder demands a subnetwork and fails validation without it.
NETWORK="${NETWORK:-default}"
SUBNET="${SUBNET:-default}"
GCS_PATH="${GCS_PATH:?set GCS_PATH to a gs:// bucket/path for the builder logs}"
# Space-separated, each with a tag or digest: the cache hits only the exact ref a
# pod requests, so a floating tag caches nothing. Add anything CI pulls often; the
# default is this repo's go-build image, resolved so it cannot drift.
if [ -z "${CONTAINER_IMAGES:-}" ]; then
  go_build_tag="$("$REPO/hack/generate-version-tag-name.sh" -f "$REPO/images/calico-go-build/versions.yaml")"
  CONTAINER_IMAGES="docker.io/calico/go-build:${go_build_tag}"
fi
# Pinned in versions.yaml; a branch or tag also works, for testing upstream.
AI_ON_GKE_REF="${AI_ON_GKE_REF:-$(yq -r '.ai-on-gke.ref' "$HERE/versions.yaml")}"

log() { echo "[preload-disk] $*"; }

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# Not `clone --branch`: that cannot check out a bare commit. This accepts any ref.
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

# Release images are immutable; branch images are replaced. A pool stores the image
# PATH, not an id, so a same-name recreate leaves its config valid.
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
