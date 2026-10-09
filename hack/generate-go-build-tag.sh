#!/bin/bash
# The calico/go-build tag this build should pre-pull and label itself with: the tag
# calico-go-build-cd actually published, which is not always the one versions.yaml
# names.
#
# They diverge on a re-release. When a change moves nothing in versions.yaml,
# create-tag-on-version-change appends a release number to the git tag (-1, -2), and
# calico-go-build-cd publishes under that suffixed tag because its BRANCH_NAME is the
# git tag. versions.yaml never carries the suffix. Reading it here would pre-pull the
# build being replaced -- and that pull succeeds, since the unsuffixed tag still
# exists from the earlier build, so the cache silently warms the superseded image and
# the go-build-tag label names the wrong release.
#
#   tag 1.27.1-llvm21.1.8-k8s1.37.1-1  ->  1.27.1-llvm21.1.8-k8s1.37.1-1
#   branch go1.27                      ->  1.27.1-llvm21.1.8-k8s1.37.1  (versions.yaml)
#
# Same tag precedence as generate-image-name.sh. A branch build has no published
# version tag to aim at -- cd publishes :master or :<branch> there -- so it keeps
# reading versions.yaml, i.e. the tag that branch will cut next.
#
# This is deliberately not a flag on generate-version-tag-name.sh: that script names
# the tag create-tag-on-version-change is about to create, and must stay a pure
# reader of versions.yaml.
#
#   generate-go-build-tag.sh [-f versions.yaml]

set -eu

ver_file=""

while getopts ":f:" opt; do
    case $opt in
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

here="$(cd "$(dirname "$0")" && pwd)"
: "${ver_file:=$here/../images/calico-go-build/versions.yaml}"

if [[ ${SEMAPHORE_GIT_REF_TYPE:-} == "tag" && -n ${SEMAPHORE_GIT_TAG_NAME:-} ]]; then
    echo "${SEMAPHORE_GIT_TAG_NAME}"
else
    "$here/generate-version-tag-name.sh" -f "$ver_file"
fi
