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

# The VM's toolchain tracks the go-build image, not this repo's go.mod: a job that
# runs on the VM is the same job that would have run in calico/go-build, so it must
# see the same Go. images/calico-go-build/versions.yaml is the single source of
# truth for both, and hack/generate-version-tag-name.sh composes the image tag from
# it exactly as the release tagging does -- so bumping versions.yaml and re-running
# this script is all it takes to keep the two in step.
VERSIONS="$REPO/images/calico-go-build/versions.yaml"
command -v yq >/dev/null || { echo "yq is required to read $VERSIONS" >&2; exit 1; }
GO_VERSION="${GO_VERSION:-$("$REPO/hack/generate-version-tag-name.sh" -f "$VERSIONS" -g)}"
GO_SHA256="${GO_SHA256:-$(yq -r '.golang.checksum.sha256.amd64' "$VERSIONS")}"
GO_BUILD_IMAGE="${GO_BUILD_IMAGE:-calico/go-build:$("$REPO/hack/generate-version-tag-name.sh" -f "$VERSIONS")}"
# kubectl tracks the same k8s release the go-build image is cut against.
KUBECTL_VERSION="${KUBECTL_VERSION:-v$(yq -r '.kubernetes.version' "$VERSIONS")}"
log "go $GO_VERSION, kubectl $KUBECTL_VERSION, prepulling $GO_BUILD_IMAGE (from images/calico-go-build/versions.yaml)"

# Named off the go-build release tag, exactly as the go-build images are, so a VM
# image and the toolchain it was built against are matchable by eye (dots become
# hyphens: GCE names are RFC1035). Deterministic, not timestamped -- one release
# builds one image, and a re-release carries its own -N suffix in the tag.
IMAGE="${IMAGE:-$("$REPO/hack/generate-image-name.sh" -p "$FAMILY" -f "$VERSIONS")}"
log "image name: $IMAGE (family $FAMILY)"

# Fail before spending 4 minutes on a builder VM, not after: GCE image names are
# unique per project, and a deterministic name means a rebuild of the same release
# collides. Re-releases get their own -N tag, so a collision means this release's
# image already exists.
if gcloud compute images describe "$IMAGE" --project="$PROJECT" >/dev/null 2>&1; then
  log "image $IMAGE already exists in $PROJECT -- this release is already built."
  log "to rebuild it: gcloud compute images delete $IMAGE --project=$PROJECT"
  log "or set IMAGE=<name> to build under a different name."
  exit 1
fi

# kind and gh have no entry in the go-build versions file, so they are pinned in
# ours. Everything is pinned deliberately: an image build must be reproducible from
# a commit, which "latest"/stable.txt is not.
VM_VERSIONS="$HERE/versions.yaml"
KIND_VERSION="${KIND_VERSION:-$(yq -r '.kind.version' "$VM_VERSIONS")}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-$(yq -r '.kind.node_image' "$VM_VERSIONS")}"
GH_VERSION="${GH_VERSION:-$(yq -r '.gh.version' "$VM_VERSIONS")}"
log "kind $KIND_VERSION (node $KIND_NODE_IMAGE), gh $GH_VERSION (from vm-image/versions.yaml)"

# provision.sh runs on the builder as its startup-script, where it cannot read this
# repo -- so bake the versions in as a preamble rather than hardcoding them there.
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

# The go-build tag also goes on as a label: the image NAME has dots rewritten to
# hyphens for RFC1035, so the label is where the exact tag survives for lookup
# (gcloud compute images list --filter="labels.go-build-tag=<tag>").
log "creating image $IMAGE in family $FAMILY"
gcloud compute images create "$IMAGE" --project="$PROJECT" \
  --source-disk="$BUILDER" --source-disk-zone="$ZONE" --family="$FAMILY" \
  --labels="go-build-tag=$(echo "$GO_BUILD_IMAGE" | sed 's|.*:||; s|\.|_|g')"

log "done: image $IMAGE (family $FAMILY, project $PROJECT). createvm: GOOGLE_VM_IMAGE_PROJECT=$PROJECT GOOGLE_VM_IMAGE_FAMILY=$FAMILY"
