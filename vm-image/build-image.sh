#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Build (or refresh) the kind-rig CI VM base image: create a throwaway builder VM
# from stock Ubuntu, run provision.sh on it, snapshot its disk into the image
# FAMILY, and delete the builder. createvm then boots VMs from FAMILY (fully
# tooled, no per-run installs). Re-run any time to pick up newer tools -- the
# family means createvm automatically gets the newest image.
#
#   PROJECT=unique-caldron-775 ZONE=us-central1-a FAMILY=ci-base ./build-image.sh
#
# Needs: gcloud authed as an identity with compute instance + image create/delete
# in PROJECT. Takes ~3-4 min.
set -euo pipefail

PROJECT="${PROJECT:-unique-caldron-775}"
ZONE="${ZONE:-us-central1-a}"
FAMILY="${FAMILY:-ci-base}"
BUILDER="${BUILDER:-ci-img-builder-$$}"
IMAGE="${IMAGE:-${FAMILY}-$(date +%Y%m%d-%H%M%S)}"
HERE="$(cd "$(dirname "$0")" && pwd)"

log() { echo "[build-image] $*"; }
cleanup() { gcloud --quiet compute instances delete "$BUILDER" --project="$PROJECT" --zone="$ZONE" 2>/dev/null || true; }
trap cleanup EXIT

log "creating builder $BUILDER in $ZONE"
gcloud compute instances create "$BUILDER" --project="$PROJECT" --zone="$ZONE" \
  --machine-type=e2-standard-8 \
  --image-family=ubuntu-2404-lts-amd64 --image-project=ubuntu-os-cloud \
  --boot-disk-size=50GB --boot-disk-type=pd-ssd \
  --metadata-from-file startup-script="$HERE/provision.sh"

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

log "creating image $IMAGE in family $FAMILY"
gcloud compute images create "$IMAGE" --project="$PROJECT" \
  --source-disk="$BUILDER" --source-disk-zone="$ZONE" --family="$FAMILY"

log "done: image $IMAGE (family $FAMILY, project $PROJECT). createvm: GOOGLE_VM_IMAGE_PROJECT=$PROJECT GOOGLE_VM_IMAGE_FAMILY=$FAMILY"
