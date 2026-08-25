#!/bin/bash

# Assemble .semaphore/semaphore.yml from the fragments in
# .semaphore/semaphore.yml.d, the way projectcalico/calico does.
#
# Top-level fragments are concatenated in filename order, so the numeric
# prefixes set the order. When 09-blocks.yml is reached (it contains just the
# `blocks:` key) every file under semaphore.yml.d/blocks/ follows it, also in
# filename order, indented by two spaces. Block fragments are written at column
# zero so each one is valid YAML on its own and an editor can lint it.

set -eu

# --check reports whether the committed file matches its fragments instead of
# rewriting it. It compares generated output against the file on disk, so it is
# independent of what git happens to have staged.
check=false
if [ "${1:-}" = "--check" ]; then
    check=true
fi

cd "$(git rev-parse --show-toplevel)"

d=.semaphore/semaphore.yml.d
out=.semaphore/semaphore.yml
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

cat >"$tmp" <<'HEADER'
# !! WARNING, DO NOT EDIT !! This file is generated from the fragments
# in /.semaphore/semaphore.yml.d. To update, modify the relevant
# fragment and then run 'make gen-semaphore-yaml'.
HEADER

for f in "$d"/*.yml; do
    cat "$f" >>"$tmp"
    if [ "$(basename "$f")" = "09-blocks.yml" ]; then
        for b in "$d"/blocks/*.yml; do
            # Indent to sit under `blocks:`, leaving blank lines empty rather
            # than filling them with trailing spaces.
            sed 's/^\(.\)/  \1/' "$b" >>"$tmp"
        done
    fi
done

# A fragment with a typo must not land in the tree.
if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import sys,yaml; yaml.safe_load(open(sys.argv[1]))' "$tmp"
fi

if [ "$check" = true ]; then
    if ! diff -u "$out" "$tmp"; then
        echo >&2 "ERROR: $out does not match its fragments. Run 'make gen-semaphore-yaml'."
        exit 1
    fi
    echo "$out is up to date"
    exit 0
fi

mv "$tmp" "$out"
trap - EXIT
echo "generated $out"
