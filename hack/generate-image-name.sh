#!/bin/bash
# Generate a GCE image name for a build artifact that corresponds to a go-build
# release, so a VM/disk image and a calico/go-build tag are matchable by eye:
#
#   1.27.0-llvm21.1.8-k8s1.37.0   ->   ci-base-1-27-0-llvm21-1-8-k8s1-37-0
#
# The version half follows exactly what calico-go-build-cd does for its image tag
# (see .semaphore/promotions/calico-go-build.yml): a tag build uses the git tag,
# anything else uses the branch. The tag matters because it is the only place the
# re-release suffix lives -- a CVE fix that leaves every compiler version
# untouched reuses the version tag with -1, -2 appended (see
# .github/workflows/create-tag-on-version-change.yml), and
# generate-version-tag-name.sh does not emit that.
#
#   tag build      1.27.0-llvm21.1.8-k8s1.37.0-1  ->  ci-base-1-27-0-llvm21-1-8-k8s1-37-0-1
#   branch build   go1.27                         ->  ci-base-go1-27
#   branch build   master                         ->  ci-base-master
#
# GCE resource names are RFC1035: lowercase, digits and hyphens only, first
# character a letter, 63 max. Dots (and anything else illegal, such as the / in a
# branch name) become hyphens.
#
#   generate-image-name.sh -p ci-base [-f images/calico-go-build/versions.yaml]

set -eu

prefix=""
ver_file=""

while getopts ":p:f:" opt; do
    case $opt in
    p) prefix="$OPTARG" ;;
    f) ver_file="$OPTARG" ;;
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
    # Local run: the checked-out branch is the closest thing to CI's branch build.
    version="$branch"
else
    # Detached HEAD with no CI hints: fall back to what the versions file says.
    version="$("$here/generate-version-tag-name.sh" -f "$ver_file")"
fi

# Fold to a legal RFC1035 tail: lowercase, anything illegal (dots, slashes,
# underscores) to a hyphen, no runs of hyphens, no leading or trailing hyphen.
version="$(echo "$version" |
    tr '[:upper:]' '[:lower:]' |
    sed -e 's/[^a-z0-9-]/-/g' -e 's/--*/-/g' -e 's/^-//' -e 's/-$//')"

if [[ -z $version ]]; then
    echo "version/branch reduced to an empty string" >&2
    exit 1
fi

name="${prefix}-${version}"

if [[ ${#name} -gt 63 ]]; then
    echo "image name is ${#name} characters, over the GCE limit of 63: $name" >&2
    exit 1
fi
if ! [[ $name =~ ^[a-z]([-a-z0-9]*[a-z0-9])?$ ]]; then
    echo "not a valid GCE resource name: $name" >&2
    exit 1
fi

echo "$name"
