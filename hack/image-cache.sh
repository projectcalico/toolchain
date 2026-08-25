#!/bin/bash

# Restore or store a built toolchain image as a compressed tarball in GCS.
#
# The cache key is the image's build ID (see image-build-id.sh), so an entry is
# reused only by a tree that produces the same image. This lets the branch build
# that follows a merge load what the pull request already built instead of
# repeating it, which matters most for arm64, ppc64le and s390x -- those run
# under QEMU emulation.
#
# Entries expire quickly, and that is the point. The cache exists to carry one
# change from its pull request to the merge that follows -- the two share a tree,
# so they share a build ID. It is not meant to let an unrelated run skip a
# rebuild: these images install packages from floating bases (almalinux:9,
# fedora:44, ubi-minimal:latest) and run `dnf upgrade`, so rebuilding the same
# tree later legitimately produces a newer, more patched image and that is
# usually what we want.
#
# A tree hash alone cannot tell those two cases apart, because change_in can fire
# without the tree changing -- editing semaphore.yml does exactly that. MAX_AGE_HOURS
# bounds the difference: long enough for a pull request to reach master, short
# enough that anything else rebuilds. It is also the freshness bound on published
# images, which before this cache were always built from scratch.
#
# Usage:
#   image-cache.sh restore <image> <arch>   # exit 0 on hit, 1 on miss
#   image-cache.sh store   <image> <arch>

set -eu

cd "$(git rev-parse --show-toplevel)"

MAX_AGE_HOURS=${MAX_AGE_HOURS:-24}

usage() {
    echo "usage: $0 <restore|store> <image> <arch>" >&2
    exit 2
}

[ $# -eq 3 ] || usage
action=$1
image=$2
arch=$3

if [ -z "${GCS_IMAGE_CACHE_BUCKET:-}" ]; then
    echo "GCS_IMAGE_CACHE_BUCKET is not set, skipping image cache" >&2
    exit 1
fi

# Local tags produced by the images/Makefile build targets. calico-base builds
# one image per UBI version, so it carries two tags per architecture.
case "$image" in
calico-base)
    tags="base:ubi9-latest-${arch} base:ubi10-latest-${arch}"
    ;;
calico-binfmt)
    qemu_version=$(yq -r '.qemu.version' images/calico-binfmt/versions.yaml)
    tags="binfmt:qemu-v${qemu_version}-amd64"
    ;;
calico-go-build)
    tags="go-build:latest-${arch}"
    ;;
calico-rust-build)
    tags="rust-build:latest-${arch}"
    ;;
calico-tinygo)
    tags="tinygo:latest-${arch}"
    ;;
*)
    usage
    ;;
esac

build_id=$(hack/image-build-id.sh "$image")
object="gs://${GCS_IMAGE_CACHE_BUCKET}/images/${image}-${build_id}-${arch}.tar.zst"
tarball="/tmp/${image}-${arch}.tar"

case "$action" in
restore)
    # Read timeCreated from the object metadata rather than parsing the
    # human-formatted `ls` output. A missing or unparseable timestamp is a miss
    # too: rebuilding is always safe.
    created=$(gcloud storage objects describe "$object" --format="value(timeCreated)" 2>/dev/null || true)
    if [ -z "$created" ]; then
        echo "image cache miss: $object"
        exit 1
    fi
    created_epoch=$(date -u -d "$created" +%s 2>/dev/null || echo 0)
    if [ "$created_epoch" -eq 0 ]; then
        echo "image cache timestamp unreadable, treating as miss: $object"
        exit 1
    fi
    age_hours=$(( ($(date -u +%s) - created_epoch) / 3600 ))
    if [ "$age_hours" -ge "$MAX_AGE_HOURS" ]; then
        echo "image cache stale (${age_hours}h >= ${MAX_AGE_HOURS}h), rebuilding: $object"
        exit 1
    fi
    echo "image cache hit (${age_hours}h old): $object"
    gcloud storage cp "$object" "${tarball}.zst"
    zstd -d --rm -o "$tarball" "${tarball}.zst"
    docker load -i "$tarball"
    rm -f "$tarball"
    ;;
store)
    # Never let a fork populate the cache. A fork could otherwise upload an
    # image that does not match the tree its build ID names, and a later merge
    # of that innocuous-looking tree would publish it.
    if [ -n "${SEMAPHORE_GIT_PR_SLUG:-}" ] &&
       [ "${SEMAPHORE_GIT_PR_SLUG}" != "${SEMAPHORE_GIT_REPO_SLUG:-}" ]; then
        echo "forked pull request, not storing to the image cache"
        exit 0
    fi
    if gcloud storage ls "$object" >/dev/null 2>&1; then
        echo "image cache already populated: $object"
        exit 0
    fi
    # shellcheck disable=SC2086
    docker save $tags -o "$tarball"
    zstd -3 --rm "$tarball"
    gcloud storage cp "${tarball}.zst" "$object"
    rm -f "${tarball}.zst"
    echo "image cache stored: $object"
    ;;
*)
    usage
    ;;
esac
