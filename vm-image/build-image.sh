#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Build (or refresh) the kind-rig CI VM base image: create a throwaway builder VM
# from stock Ubuntu, run provision.sh on it, snapshot its disk into the image
# FAMILY, and delete the builder. createvm then boots VMs from FAMILY (fully
# tooled, no per-run installs). Re-run any time to pick up newer tools -- the
# family means createvm automatically gets the newest image.
#
#   PROJECT=unique-caldron-775 ZONE=us-central1-a FAMILY=ci-base ./vm-image/build-image.sh
#
# Needs: gcloud and yq, gcloud authed as an identity with compute instance + image
# create/delete in PROJECT. Takes ~3-4 min.
set -euo pipefail

PROJECT="${PROJECT:-unique-caldron-775}"
ZONE="${ZONE:-us-central1-a}"
FAMILY="${FAMILY:-ci-base}"
BUILDER="${BUILDER:-ci-img-builder-$$}"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"

log() { echo "[build-image] $*"; }

# The VM's toolchain tracks the go-build image, not this repo's go.mod: a job on
# the VM would otherwise have run inside calico/go-build, so it must see the same
# Go. Bump versions.yaml and re-run to keep the two in step.
VERSIONS="$REPO/images/calico-go-build/versions.yaml"
command -v yq >/dev/null || { echo "yq is required to read $VERSIONS" >&2; exit 1; }
GO_VERSION="${GO_VERSION:-$("$REPO/hack/generate-version-tag-name.sh" -f "$VERSIONS" -g)}"
GO_SHA256="${GO_SHA256:-$(yq -r '.golang.checksum.sha256.amd64' "$VERSIONS")}"
GO_BUILD_IMAGE="${GO_BUILD_IMAGE:-calico/go-build:$("$REPO/hack/generate-version-tag-name.sh" -f "$VERSIONS")}"
# kubectl tracks the same k8s release the go-build image is cut against.
KUBECTL_VERSION="${KUBECTL_VERSION:-v$(yq -r '.kubernetes.version' "$VERSIONS")}"
log "go $GO_VERSION, kubectl $KUBECTL_VERSION, prepulling $GO_BUILD_IMAGE (from images/calico-go-build/versions.yaml)"

# Named off the go-build release tag, as the go-build images are, so a VM image and
# its toolchain match by eye. See hack/generate-image-name.sh.
IMAGE="${IMAGE:-$("$REPO/hack/generate-image-name.sh" -p "$FAMILY" -f "$VERSIONS")}"
log "image name: $IMAGE (family $FAMILY)"

# GCE image names are unique per project, so a deterministic name collides on a
# rebuild. Check now, not after four minutes of builder VM. What a collision means
# depends on the trigger, the same split calico/go-build makes:
#   release tag -- immutable; it is already built.
#   branch      -- the moving "latest build of this branch", like the
#                  calico/go-build:<branch> tag. Replace, which means delete first.
if gcloud compute images describe "$IMAGE" --project="$PROJECT" >/dev/null 2>&1; then
  if [ "${SEMAPHORE_GIT_REF_TYPE:-}" = "tag" ]; then
    log "image $IMAGE already exists in $PROJECT -- this release is already built."
    log "to rebuild it: gcloud compute images delete $IMAGE --project=$PROJECT"
    log "or set IMAGE=<name> to build under a different name."
    exit 1
  fi
  log "replacing existing branch image $IMAGE"
  gcloud --quiet compute images delete "$IMAGE" --project="$PROJECT"
fi

# kind and gh have no entry in the go-build versions file, so they live in ours.
# All pinned: an image build must be reproducible from a commit.
VM_VERSIONS="$HERE/versions.yaml"
KIND_VERSION="${KIND_VERSION:-$(yq -r '.kind.version' "$VM_VERSIONS")}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-$(yq -r '.kind.node_image' "$VM_VERSIONS")}"
GH_VERSION="${GH_VERSION:-$(yq -r '.gh.version' "$VM_VERSIONS")}"
log "kind $KIND_VERSION (node $KIND_NODE_IMAGE), gh $GH_VERSION (from vm-image/versions.yaml)"

# provision.sh runs as the builder's startup-script, where it cannot read this
# repo, so bake the versions in as a preamble.
STARTUP="$(mktemp)"
{
  echo '#!/usr/bin/env bash'
  echo '# Preamble injected by build-image.sh from the repo versions.yaml files.'
  printf 'export GO_VERSION=%q\n' "$GO_VERSION"
  printf 'export GO_SHA256=%q\n' "$GO_SHA256"
  printf 'export GO_BUILD_IMAGE=%q\n' "$GO_BUILD_IMAGE"
  printf 'export KUBECTL_VERSION=%q\n' "$KUBECTL_VERSION"
  printf 'export KIND_VERSION=%q\n' "$KIND_VERSION"
  printf 'export KIND_NODE_IMAGE=%q\n' "$KIND_NODE_IMAGE"
  printf 'export GH_VERSION=%q\n' "$GH_VERSION"
  tail -n +2 "$HERE/provision.sh" # its shebang is replaced by the one above
} >"$STARTUP"

cleanup() {
  rm -f "$STARTUP"
  gcloud --quiet compute instances delete "$BUILDER" --project="$PROJECT" --zone="$ZONE" 2>/dev/null || true
}
trap cleanup EXIT

log "creating builder $BUILDER in $ZONE"
gcloud compute instances create "$BUILDER" --project="$PROJECT" --zone="$ZONE" \
  --machine-type=e2-standard-8 \
  --image-family=ubuntu-2404-lts-amd64 --image-project=ubuntu-os-cloud \
  --boot-disk-size=50GB --boot-disk-type=pd-ssd \
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

log "installed tool versions + pre-pulled images:"
gcloud --quiet compute ssh "ubuntu@$BUILDER" --project="$PROJECT" --zone="$ZONE" \
  --command='docker --version; /usr/local/go/bin/go version; kind version; kubectl version --client 2>/dev/null | head -1; gh --version | head -1; echo "--- baked images ---"; sudo docker images --format "{{.Repository}}:{{.Tag}} ({{.Size}})"' || true

log "stopping builder for a consistent disk"
gcloud --quiet compute instances stop "$BUILDER" --project="$PROJECT" --zone="$ZONE"

# The name has dots rewritten for RFC1035, so the label is where the exact tag
# survives: images list --filter="labels.go-build-tag=<tag>".
log "creating image $IMAGE in family $FAMILY"
gcloud compute images create "$IMAGE" --project="$PROJECT" \
  --source-disk="$BUILDER" --source-disk-zone="$ZONE" --family="$FAMILY" \
  --labels="go-build-tag=$(echo "$GO_BUILD_IMAGE" | sed 's|.*:||; s|\.|_|g')"

log "done: image $IMAGE (family $FAMILY, project $PROJECT). createvm: GOOGLE_VM_IMAGE_PROJECT=$PROJECT GOOGLE_VM_IMAGE_FAMILY=$FAMILY"
