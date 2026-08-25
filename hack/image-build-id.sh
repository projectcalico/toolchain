#!/bin/bash

# Print a stable content hash ("build ID") for one toolchain image.
#
# The hash covers every committed file that can change the image, so the same
# source tree always maps to the same ID. CI addresses its staged per-arch
# builds by this ID, which lets a pipeline skip a rebuild when the registry
# already holds an image for the tree it was handed.
#
# The hash reads the committed tree, not the working directory, so it ignores
# generated files under bin/ but also ignores uncommitted edits.

set -eu

usage() {
    echo "usage: $0 <calico-base|calico-binfmt|calico-go-build|calico-rust-build|calico-tinygo>" >&2
    exit 1
}

[ $# -eq 1 ] || usage

case "$1" in
calico-base)
    deps="images/calico-base"
    ;;
calico-binfmt)
    # The binfmt binary is built from cmd/ and copied into the image.
    deps="images/calico-binfmt cmd"
    ;;
calico-go-build)
    # The semvalidator binary is built from cmd/ and copied into the image.
    deps="images/calico-go-build cmd"
    ;;
calico-rust-build)
    deps="images/calico-rust-build"
    ;;
calico-tinygo)
    deps="images/calico-tinygo"
    ;;
*)
    usage
    ;;
esac

# These carry the build args and docker invocation shared by every image.
deps="$deps images/Makefile lib.Makefile"

for dep in $deps; do
    git rev-parse "HEAD:$dep"
done | sha256sum | cut -c1-12
