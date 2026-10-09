#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Build (or refresh) the ci-base CI VM image: a throwaway builder VM runs
# provision.sh, its disk is snapshotted into FAMILY, the builder is deleted.
# createvm boots from FAMILY, so it always gets the newest.
#
#   PROJECT=unique-caldron-775 ./vm-images/ci-base/build-image.sh
#
# Needs gcloud (authed, compute instance + image create/delete) and yq. ~3-4 min.
set -euo pipefail

PROJECT="${PROJECT:-unique-caldron-775}"
ZONE="${ZONE:-us-central1-a}"
# Left empty so generate-image-name.sh decides: a release goes to its own
# per-version family, a branch to ci-base-<branch>. Pinning one here puts every
# build in the same family, and since a family resolves to its newest member, a
# master build would become what every CI VM boots from.
FAMILY="${FAMILY:-}"
BUILDER="${BUILDER:-ci-img-builder-$$}"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

log() { echo "[build-image] $*"; }

# The toolchain tracks the go-build image, not this repo's go.mod: a job here would
# otherwise have run inside calico/go-build, so it must see the same Go.
VERSIONS="$REPO/images/calico-go-build/versions.yaml"
command -v yq >/dev/null || { echo "yq is required to read $VERSIONS" >&2; exit 1; }
GO_VERSION="${GO_VERSION:-$("$REPO/hack/generate-version-tag-name.sh" -f "$VERSIONS" -g)}"
GO_SHA256="${GO_SHA256:-$(yq -r '.golang.checksum.sha256.amd64' "$VERSIONS")}"
GO_BUILD_IMAGE="${GO_BUILD_IMAGE:-calico/go-build:$("$REPO/hack/generate-version-tag-name.sh" -f "$VERSIONS")}"
# kubectl tracks the same k8s release the go-build image is cut against.
KUBECTL_VERSION="${KUBECTL_VERSION:-v$(yq -r '.kubernetes.version' "$VERSIONS")}"
log "go $GO_VERSION, kubectl $KUBECTL_VERSION, prepulling $GO_BUILD_IMAGE (from images/calico-go-build/versions.yaml)"

# Named off the go-build release tag, so image and toolchain match by eye; a branch
# build carries its commit instead, which is what keeps it unique.
IMAGE="${IMAGE:-$("$REPO/hack/generate-image-name.sh" -p ci-base -f "$VERSIONS")}"
FAMILY="${FAMILY:-$("$REPO/hack/generate-image-name.sh" -p ci-base -f "$VERSIONS" -F)}"
log "image name: $IMAGE (family $FAMILY)"

# Names are unique per build -- a release by its tag, a branch by its commit -- so
# a collision means this exact thing is already built. Nothing is deleted to make
# room; the family moves to whatever is newest.
if gcloud compute images describe "$IMAGE" --project="$PROJECT" >/dev/null 2>&1; then
  log "image $IMAGE already exists in $PROJECT -- nothing to rebuild."
  log "to force it: gcloud compute images delete $IMAGE --project=$PROJECT"
  log "or set IMAGE=<name> to build under a different name."
  exit 1
fi

# kind and gh have no entry in the go-build versions file. All pinned, so an image
# build is reproducible from a commit.
VM_VERSIONS="$HERE/versions.yaml"
KIND_VERSION="${KIND_VERSION:-$(yq -r '.kind.version' "$VM_VERSIONS")}"
# Space-separated: env vars cannot hold arrays, and image refs contain no spaces.
KIND_NODE_IMAGES="${KIND_NODE_IMAGES:-$(yq -r '.kind.node_images | join(" ")' "$VM_VERSIONS")}"
[ -n "$KIND_NODE_IMAGES" ] || { echo "kind.node_images is empty in $VM_VERSIONS" >&2; exit 1; }
GH_VERSION="${GH_VERSION:-$(yq -r '.gh.version' "$VM_VERSIONS")}"
log "kind $KIND_VERSION, gh $GH_VERSION (from vm-images/ci-base/versions.yaml)"
for img in $KIND_NODE_IMAGES; do log "  node image: $img"; done

# provision.sh runs as the builder's startup-script and cannot read this repo.
STARTUP="$(mktemp)"
{
  echo '#!/usr/bin/env bash'
  echo '# Preamble injected by build-image.sh from the repo versions.yaml files.'
  printf 'export GO_VERSION=%q\n' "$GO_VERSION"
  printf 'export GO_SHA256=%q\n' "$GO_SHA256"
  printf 'export GO_BUILD_IMAGE=%q\n' "$GO_BUILD_IMAGE"
  printf 'export KUBECTL_VERSION=%q\n' "$KUBECTL_VERSION"
  printf 'export KIND_VERSION=%q\n' "$KIND_VERSION"
  printf 'export KIND_NODE_IMAGES=%q\n' "$KIND_NODE_IMAGES"
  printf 'export GH_VERSION=%q\n' "$GH_VERSION"
  tail -n +2 "$HERE/provision.sh" # its shebang is replaced by the one above
} >"$STARTUP"

cleanup() {
  rm -f "$STARTUP"
  gcloud --quiet compute instances delete "$BUILDER" --project="$PROJECT" --zone="$ZONE" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

log "creating builder $BUILDER in $ZONE"
# max-run-duration so the builder cannot outlive the job: the trap misses a
# SIGKILL, and the poll below can run past the pipeline's 30-minute limit.
gcloud compute instances create "$BUILDER" --project="$PROJECT" --zone="$ZONE" \
  --machine-type=e2-standard-8 \
  --image-family=ubuntu-2404-lts-amd64 --image-project=ubuntu-os-cloud \
  --boot-disk-size=50GB --boot-disk-type=pd-ssd \
  --max-run-duration=60m --instance-termination-action=DELETE \
  --metadata-from-file startup-script="$STARTUP"

log "waiting for provision.sh (~2-3 min)"
ready=""
for _ in $(seq 1 120); do
  if gcloud --quiet compute ssh "ubuntu@$BUILDER" --project="$PROJECT" --zone="$ZONE" \
       --ssh-flag="-o BatchMode=yes -o ConnectTimeout=10" --command='test -e /var/run/provision-done' 2>/dev/null; then
    ready=1; break
  fi
  sleep 10
done
if [ -z "$ready" ]; then
  log "provision timed out; serial console tail:"
  gcloud compute instances get-serial-port-output "$BUILDER" --project="$PROJECT" --zone="$ZONE" 2>/dev/null | tail -100 || true
  exit 1
fi

# Enforcing, not informational: every binary must run and every pre-pulled image
# must be in the cache, or nothing is published from this builder.
log "verifying the toolchain before snapshotting"
if ! gcloud --quiet compute ssh "ubuntu@$BUILDER" --project="$PROJECT" --zone="$ZONE" \
  --command="set -e
    docker --version
    /usr/local/go/bin/go version
    kind version
    kubectl version --client
    gh --version
    echo '--- baked images ---'
    sudo docker images --format '{{.Repository}}:{{.Tag}} ({{.Size}})'
    for img in $GO_BUILD_IMAGE $KIND_NODE_IMAGES registry:2; do
      sudo docker image inspect \"\$img\" >/dev/null || { echo \"missing from cache: \$img\"; exit 1; }
    done"; then
  log "verification FAILED -- not publishing an image from this builder"
  exit 1
fi

log "stopping builder for a consistent disk"
gcloud --quiet compute instances stop "$BUILDER" --project="$PROJECT" --zone="$ZONE"

# Dots become dashes, matching the munged form in the image name -- label values
# may not contain dots, so this cannot hold the tag verbatim.
log "creating image $IMAGE in family $FAMILY"
gcloud compute images create "$IMAGE" --project="$PROJECT" \
  --source-disk="$BUILDER" --source-disk-zone="$ZONE" --family="$FAMILY" \
  --labels="go-build-tag=$(echo "$GO_BUILD_IMAGE" | sed 's|.*:||; s|\.|-|g')"

log "done: image $IMAGE (family $FAMILY, project $PROJECT). createvm: GOOGLE_VM_IMAGE_PROJECT=$PROJECT GOOGLE_VM_IMAGE_FAMILY=$FAMILY"
