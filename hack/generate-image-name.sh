#!/bin/bash
# Name a GCE image after the go-build release it corresponds to, so the two are
# matchable by eye. Tag builds use the git tag, everything else the branch --
# matching calico-go-build-cd, and the tag is the only place the re-release suffix
# (-1, -2) lives.
#
#   tag 1.27.0-llvm21.1.8-k8s1.37.0-1  ->  ci-base-1-27-0-llvm21-1-8-k8s1-37-0-1
#   branch go1.27                      ->  ci-base-go1-27
#
# Dots and other illegal characters become hyphens (GCE names are RFC1035, 63 max).
# -m caps it lower: GKE allows 39 for a secondary boot disk and only enforces that
# when a node pool attaches the image, long after it built.
#
# A tag build is one release, so its name is the tag. A branch build recurs, so its
# name carries the commit to stay unique -- nothing is ever deleted to make room.
#
# -F prints the FAMILY instead, which moves to its newest member on its own.
# Release families are per toolchain version, not one shared bucket: a family
# resolves by creation time, not by version, and release lines interleave -- a
# 1.26 patch cut after a 1.27 release would otherwise make the shared family
# resolve to the older Go. The version half drops the llvm/k8s words since
# position already says which is which.
#
#   tag    ci-base-1-27-1-llvm21-1-8-k8s1-37-0   family ci-base-1-27-1-21-1-8-1-37-0
#   master ci-base-master-9cc1214                family ci-base-master
#
# The family comes from versions.yaml rather than the git tag, so a re-release
# (-1, -2) lands in the same family and just moves the pointer -- the tag carries
# that suffix, versions.yaml never does.
#
#   generate-image-name.sh -p ci-base [-f versions.yaml] [-m 39] [-F]

set -eu

prefix=""
ver_file=""
max_len=63
want_family=false

while getopts ":p:f:m:F" opt; do
    case $opt in
    p) prefix="$OPTARG" ;;
    f) ver_file="$OPTARG" ;;
    m) max_len="$OPTARG" ;;
    F) want_family=true ;;
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

# Fold to a legal RFC1035 tail: no runs of hyphens, none leading or trailing.
fold_name() {
    echo "$1" |
        tr '[:upper:]' '[:lower:]' |
        sed -e 's/[^a-z0-9-]/-/g' -e 's/--*/-/g' -e 's/^-//' -e 's/-$//'
}

here="$(cd "$(dirname "$0")" && pwd)"
: "${ver_file:=$here/../images/calico-go-build/versions.yaml}"

# Same precedence as calico-go-build-cd's BRANCH_NAME.
is_tag=false
if [[ ${SEMAPHORE_GIT_REF_TYPE:-} == "tag" && -n ${SEMAPHORE_GIT_TAG_NAME:-} ]]; then
    is_tag=true
    version="${SEMAPHORE_GIT_TAG_NAME}"
elif [[ -n ${SEMAPHORE_GIT_WORKING_BRANCH:-} ]]; then
    version="${SEMAPHORE_GIT_WORKING_BRANCH}"
elif branch="$(git -C "$here" rev-parse --abbrev-ref HEAD 2>/dev/null)" && [[ -n $branch && $branch != "HEAD" ]]; then
    version="$branch" # local run
else
    version="$("$here/generate-version-tag-name.sh" -f "$ver_file")" # detached HEAD
fi

version="$(fold_name "$version")"

if [[ -z $version ]]; then
    echo "version/branch reduced to an empty string" >&2
    exit 1
fi

if [[ $want_family == true ]]; then
    if [[ $is_tag == true ]]; then
        # From versions.yaml, not the tag: excludes the re-release suffix.
        base="$("$here/generate-version-tag-name.sh" -f "$ver_file")"
        version="$(echo "${base//llvm/}" | sed 's/k8s//g')"
        version="$(fold_name "$version")"
    fi
    family="${prefix}-${version}"
    if [[ ${#family} -gt 63 ]]; then
        echo "family is ${#family} characters, over the RFC1035 limit of 63: $family" >&2
        exit 1
    fi
    if ! [[ $family =~ ^[a-z]([-a-z0-9]*[a-z0-9])?$ ]]; then
        echo "not a valid GCE family name: $family" >&2
        exit 1
    fi
    echo "$family"
    exit 0
fi

name="${prefix}-${version}"
if [[ $is_tag != true ]]; then
    # A branch rebuilds under the same version string, so carry the commit: the
    # name stays unique and the family pointer moves without deleting anything.
    sha="${SEMAPHORE_GIT_SHA:-$(git -C "$here" rev-parse HEAD 2>/dev/null || true)}"
    if [[ -z $sha ]]; then
        echo "cannot determine the commit for a branch build; set SEMAPHORE_GIT_SHA" >&2
        exit 1
    fi
    name="${name}-${sha:0:7}"
fi

if [[ ${#name} -gt $max_len ]]; then
    echo "image name is ${#name} characters, over the limit of ${max_len}: $name" >&2
    exit 1
fi
if ! [[ $name =~ ^[a-z]([-a-z0-9]*[a-z0-9])?$ ]]; then
    echo "not a valid GCE resource name: $name" >&2
    exit 1
fi

echo "$name"
