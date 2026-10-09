#!/bin/bash
# Names the calico/go-build version tag from versions.yaml: the tag
# create-tag-on-version-change is about to create.
#
# -p instead names the tag calico-go-build-cd actually published for this build,
# which is what an image baking or pre-pulling go-build wants. The two differ on a
# re-release: when a change moves no version in versions.yaml, the git tag gets a
# release number appended (-1, -2) and cd publishes under that, because its
# BRANCH_NAME is the git tag. versions.yaml never carries the suffix, so without -p
# an image would cache the build being replaced -- silently, since the unsuffixed
# tag still exists from the earlier build.
#
# -p is opt-in because generate-image-name.sh calls this to build the image FAMILY
# and must keep getting the unsuffixed tag: a re-release belongs in the same family,
# moving the pointer rather than stranding it.
#
#   tag 1.27.1-llvm21.1.8-k8s1.37.1-1   -p -> 1.27.1-llvm21.1.8-k8s1.37.1-1
#                                      bare -> 1.27.1-llvm21.1.8-k8s1.37.1
#
# A branch build has no published version tag to name -- cd publishes :master or
# :<branch> there -- so -p falls back to versions.yaml, i.e. the tag it will cut next.
#
#   generate-version-tag-name.sh -f versions.yaml [-g | -p]

set -eu

ver_file=""
go_ver_only=false
published=false

while getopts ":f:gp" opt; do
    case $opt in
    f)
        ver_file="$OPTARG"
        ;;
    g)
        go_ver_only=true
        ;;
    p)
        published=true
        ;;
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

if [[ "$go_ver_only" = true ]] && [[ "$published" = true ]]; then
    echo "-g and -p are mutually exclusive: -g names a Go version, -p an image tag" >&2
    exit 1
fi

# Same tag precedence as generate-image-name.sh and calico-go-build-cd's BRANCH_NAME.
if [[ "$published" = true ]] &&
    [[ ${SEMAPHORE_GIT_REF_TYPE:-} == "tag" ]] &&
    [[ -n ${SEMAPHORE_GIT_TAG_NAME:-} ]]; then
    echo "${SEMAPHORE_GIT_TAG_NAME}"
    exit 0
fi

golang_ver=$(yq -r .golang.version "$ver_file")
k8s_ver=$(yq -r .kubernetes.version "$ver_file")
llvm_ver=$(yq -r .llvm.version "$ver_file")

if [[ -z $golang_ver ]] || [[ -z $k8s_ver ]] || [[ -z $llvm_ver ]]; then
    echo "one of the golang, llvm, or kubernetes versions is empty" >&2
    exit 1
fi

if [[ "$go_ver_only" = true ]]; then
    echo "${golang_ver}"
else
    echo "${golang_ver}-llvm${llvm_ver}-k8s${k8s_ver}"
fi
