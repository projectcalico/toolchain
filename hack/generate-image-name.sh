#!/bin/bash
# Generate a GCE image name matching the go-build release it corresponds to, so an
# image and a calico/go-build tag are matchable by eye:
#
#   1.27.0-llvm21.1.8-k8s1.37.0   ->   ci-base-1-27-0-llvm21-1-8-k8s1-37-0
#
# The version half follows calico-go-build-cd (see promotions/calico-go-build.yml):
# a tag build uses the git tag, anything else the branch. The tag matters because
# it is the only place the re-release suffix lives -- a CVE fix leaving every
# compiler version untouched reuses the tag with -1, -2 appended (see
# create-tag-on-version-change.yml), which generate-version-tag-name.sh cannot know.
#
#   tag build      1.27.0-llvm21.1.8-k8s1.37.0-1  ->  ci-base-1-27-0-llvm21-1-8-k8s1-37-0-1
#   branch build   go1.27                         ->  ci-base-go1-27
#   branch build   master                         ->  ci-base-master
#
# GCE names are RFC1035: lowercase, digits, hyphens, leading letter, 63 max. Dots
# and anything else illegal (a / in a branch name) become hyphens.
#
# -m caps the length below GCE's own 63. GKE allows a secondary boot disk image
# name of at most 39 characters, and enforces it when a node pool ATTACHES the
# image -- long after the image built cleanly. Pass the real cap so an over-long
# name fails here instead.
#
#   generate-image-name.sh -p ci-base [-f versions.yaml] [-m 39]

set -eu

prefix=""
ver_file=""
max_len=63

while getopts ":p:f:m:" opt; do
    case $opt in
    p) prefix="$OPTARG" ;;
    f) ver_file="$OPTARG" ;;
    m) max_len="$OPTARG" ;;
    :)
        echo "option: -$OPTARG requires an argument" >&2
        exit 1
        ;;
    *)
        echo "invalid option: -$OPTARG" >&2
        exit 1
        ;;
    esac
done

if [[ -z $prefix ]]; then
    echo "-p <prefix> is required" >&2
    exit 1
fi

here="$(cd "$(dirname "$0")" && pwd)"
: "${ver_file:=$here/../images/calico-go-build/versions.yaml}"

# Same precedence as calico-go-build-cd's BRANCH_NAME.
if [[ ${SEMAPHORE_GIT_REF_TYPE:-} == "tag" && -n ${SEMAPHORE_GIT_TAG_NAME:-} ]]; then
    version="${SEMAPHORE_GIT_TAG_NAME}"
elif [[ -n ${SEMAPHORE_GIT_WORKING_BRANCH:-} ]]; then
    version="${SEMAPHORE_GIT_WORKING_BRANCH}"
elif branch="$(git -C "$here" rev-parse --abbrev-ref HEAD 2>/dev/null)" && [[ -n $branch && $branch != "HEAD" ]]; then
    version="$branch" # local run
else
    version="$("$here/generate-version-tag-name.sh" -f "$ver_file")" # detached HEAD
fi

# Fold to a legal RFC1035 tail: no runs of hyphens, none leading or trailing.
version="$(echo "$version" |
    tr '[:upper:]' '[:lower:]' |
    sed -e 's/[^a-z0-9-]/-/g' -e 's/--*/-/g' -e 's/^-//' -e 's/-$//')"

if [[ -z $version ]]; then
    echo "version/branch reduced to an empty string" >&2
    exit 1
fi

name="${prefix}-${version}"

if [[ ${#name} -gt $max_len ]]; then
    echo "image name is ${#name} characters, over the limit of ${max_len}: $name" >&2
    exit 1
fi
if ! [[ $name =~ ^[a-z]([-a-z0-9]*[a-z0-9])?$ ]]; then
    echo "not a valid GCE resource name: $name" >&2
    exit 1
fi

echo "$name"
