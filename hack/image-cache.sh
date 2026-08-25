#!/bin/bash

# Restore or store a built toolchain image as a compressed tarball in GCS.
#
# The cache carries one change from its pull request to the merge that follows:
# the two share a source tree, so they share a cache entry, and the merge does
# not repeat a build the pull request already did. That matters most for arm64,
# ppc64le and s390x, which build under QEMU emulation.
#
# An entry is keyed by two things, because the source tree alone does not
# determine the image:
#
#   1. The build ID: a hash of the committed files that can change this image.
#   2. The digests of the base images it is built FROM. Those are floating tags,
#      and a new release means a rebuild would produce a different image. UBI is
#      rebuilt roughly daily; almalinux:9 and fedora:44 roughly quarterly.
#
# That covers new base releases but not everything, because dnf and microdnf
# pull from repositories that ship updates between base image retags --
# almalinux:9 can sit unchanged for months while its repositories do not.
# MAX_AGE_HOURS is the backstop for that drift, and it is also the staleness
# bound on anything published, which before this cache was always built fresh.
#
# Usage:
#   image-cache.sh restore <image> <arch>   # exit 0 on hit, 1 on miss
#   image-cache.sh store   <image> <arch>

set -eu

usage() {
    echo "usage: $0 <restore|store> <image> <arch>" >&2
    exit 2
}

[ $# -eq 3 ] || usage
action=$1
image=$2
arch=$3

case "$action" in
restore | store) ;;
*) usage ;;
esac

cd "$(git rev-parse --show-toplevel)"

MAX_AGE_HOURS=${MAX_AGE_HOURS:-72}

# A cache that cannot be reached must never fail a build. A restore reports a
# miss so the caller builds instead; a store quietly does nothing.
unavailable() {
    echo "$1, skipping image cache" >&2
    if [ "$action" = restore ]; then
        exit 1
    fi
    exit 0
}

[ -n "${GCS_IMAGE_CACHE_BUCKET:-}" ] || unavailable "GCS_IMAGE_CACHE_BUCKET is not set"

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

# calico-base substitutes UBI_VERSION at build time; expand it to the versions
# the Makefile actually builds.
bases=$(grep '^FROM' "images/${image}/Dockerfile" | awk '{print $2}' | grep -v '^scratch$' | sort -u)
if [ "$image" = calico-base ]; then
    bases="${bases//\$\{UBI_VERSION\}/ubi9} ${bases//\$\{UBI_VERSION\}/ubi10}"
fi

digests=""
for base in $bases; do
    digest=$(docker buildx imagetools inspect "$base" --format '{{.Manifest.Digest}}' 2>/dev/null || true)
    [ -n "$digest" ] || unavailable "cannot resolve the digest of $base"
    digests="${digests}${digest}"
done
base_id=$(printf '%s' "$digests" | sha256sum | cut -c1-8)

object="gs://${GCS_IMAGE_CACHE_BUCKET}/images/${image}-${build_id}-${base_id}-${arch}.tar.zst"
tarball="/tmp/${image}-${arch}.tar"

case "$action" in
restore)
    # Read timeCreated from the object metadata rather than parsing the
    # human-formatted `ls` output. A missing or unreadable timestamp is a miss
    # too: rebuilding is always safe.
    created=$(gcloud storage objects describe "$object" --format="value(timeCreated)" 2>/dev/null || true)
    if [ -z "$created" ]; then
        echo "image cache miss: $object"
        exit 1
    fi
    created_epoch=$(date -u -d "$created" +%s 2>/dev/null || echo 0)
    if [ "$created_epoch" -eq 0 ]; then
        echo "image cache timestamp unreadable, rebuilding: $object"
        exit 1
    fi
    age_hours=$((($(date -u +%s) - created_epoch) / 3600))
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
esac
