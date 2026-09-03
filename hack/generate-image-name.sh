#!/bin/bash
# Generate a GCE image name for a build artifact that corresponds to a go-build
# release, so a VM/disk image and a calico/go-build tag are matchable by eye:
#
#   1.27.0-llvm21.1.8-k8s1.37.0   ->   ci-base-1-27-0-llvm21-1-8-k8s1-37-0
#
# The version half comes from the release tag when one triggered the build
# ($SEMAPHORE_GIT_TAG_NAME), because that is the only place the re-release suffix
# lives -- a CVE fix that leaves every compiler version untouched reuses the
# version tag with -1, -2 appended (see .github/workflows/create-tag-on-version-
# change.yml), and generate-version-tag-name.sh does not emit that. Outside a tag
# build it falls back to the versions file, which is right for a manual run.
#
# GCE resource names are RFC1035: lowercase, digits and hyphens only, first
# character a letter, 63 max. Version strings are full of dots, so they are
# rewritten to hyphens -- lossless here, since the tag has no hyphen/dot ambiguity
# a reader would trip on.
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

# A tag build knows its exact release tag, suffix and all; anything else asks the
# versions file.
version="${SEMAPHORE_GIT_TAG_NAME:-}"
if [[ -z $version ]]; then
    version="$("$here/generate-version-tag-name.sh" -f "$ver_file")"
fi

name="${prefix}-${version//./-}"
name="$(echo "$name" | tr '[:upper:]' '[:lower:]')"

if [[ ${#name} -gt 63 ]]; then
    echo "image name is ${#name} characters, over the GCE limit of 63: $name" >&2
    exit 1
fi
if ! [[ $name =~ ^[a-z]([-a-z0-9]*[a-z0-9])?$ ]]; then
    echo "not a valid GCE resource name: $name" >&2
    exit 1
fi

echo "$name"
