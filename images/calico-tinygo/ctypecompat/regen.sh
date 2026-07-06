#!/usr/bin/env bash
# Copyright (c) 2026 Tigera, Inc. All rights reserved.
#
# Regenerates libctypecompat.a — the musl ctype/wctype objects that TinyGo
# >= 0.38 stopped compiling into its bundled wasi-libc, but that the prebuilt
# go-re2 / nottinygc wasm archives still reference. See README.md for the full
# rationale.
#
# The objects are lifted VERBATIM from TinyGo 0.34's prebuilt wasi-libc
# (calico/tinygo:0.34.0). TinyGo 0.34 and the current build's TinyGo both pin
# the identical wasi-libc commit, so these objects are ABI-compatible with the
# 0.39 build; we only reuse the compiled artifacts because 0.38 dropped the
# source glob from its own libc build, not because the source changed.
#
# libctypecompat.a is the repo's only committed binary. To keep it trustworthy,
# EXPECTED_SHA256 below records the exact bytes we ship; llvm-ar's default
# deterministic mode (zeroed timestamps/uids/gids) makes regeneration from the
# same source image byte-for-byte reproducible, so this hash is a real integrity
# assertion, not just a convenience.
#
# Usage:
#   ./regen.sh          Regenerate libctypecompat.a in place, then verify its
#                       sha256 matches EXPECTED_SHA256 (fails on drift).
#   ./regen.sh --check  Verify the committed libctypecompat.a matches
#                       EXPECTED_SHA256 without touching it. No docker/llvm-ar
#                       needed. Suitable for CI.
#
# Requires (regenerate mode): docker, llvm-ar, llvm-nm (host).
set -euo pipefail

cd "$(dirname "$0")"

OUT="libctypecompat.a"

# sha256 of the committed libctypecompat.a. Bump this deliberately (and explain
# why in the commit) whenever the archive is legitimately regenerated.
EXPECTED_SHA256="163ca777f4788097febe70862ee204e0464492781e5c67bfba83dc1e376cf39b"

verify() {
  # $1: path to archive to check.
  local got
  got="$(sha256sum "$1" | awk '{print $1}')"
  if [ "${got}" != "${EXPECTED_SHA256}" ]; then
    echo >&2 "FAIL: ${1} sha256 mismatch"
    echo >&2 "  expected: ${EXPECTED_SHA256}"
    echo >&2 "  got:      ${got}"
    echo >&2 "If this change is intentional, update EXPECTED_SHA256 in regen.sh."
    return 1
  fi
  echo "OK: ${1} matches EXPECTED_SHA256 (${EXPECTED_SHA256})"
}

if [ "${1:-}" = "--check" ]; then
  verify "${OUT}"
  exit 0
fi

# The TinyGo release whose wasi-libc still shipped the ctype objects.
SRC_IMAGE="${SRC_IMAGE:-calico/tinygo:0.34.0}"
# Path to the prebuilt wasi-libc archive inside that image.
LIBC_IN_IMAGE="/usr/local/tinygo/lib/wasi-libc/sysroot/lib/wasm32-wasi/libc.a"

# The ctype/wctype/__ctype object members to extract. This set defines every
# ctype symbol (toupper/tolower/is*/isw*/tow* and their _l locale variants —
# the _l/wide variants are weak aliases living in the same source files) that
# the go-re2 libc++.a and nottinygc libmimalloc.a reference as externs.
OBJS=(
  __ctype_b_loc.o __ctype_get_mb_cur_max.o __ctype_tolower_loc.o __ctype_toupper_loc.o
  isalnum.o isalpha.o isascii.o isblank.o iscntrl.o isdigit.o isgraph.o islower.o
  isprint.o ispunct.o isspace.o isupper.o isxdigit.o
  iswalnum.o iswalpha.o iswblank.o iswcntrl.o iswctype.o iswdigit.o iswgraph.o
  iswlower.o iswprint.o iswpunct.o iswspace.o iswupper.o iswxdigit.o
  toascii.o tolower.o toupper.o towctrans.o wcswidth.o wctrans.o
)

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Extracting wasi-libc from ${SRC_IMAGE} ..."
cid="$(docker create "${SRC_IMAGE}")"
docker cp "${cid}:${LIBC_IN_IMAGE}" "${tmp}/libc.a"
docker rm "${cid}" >/dev/null

echo "Unpacking objects ..."
( cd "${tmp}" && llvm-ar x libc.a )

echo "Archiving ${#OBJS[@]} ctype objects into ${OUT} ..."
# llvm-ar 'rcs' uses deterministic 'D' mode by default (zeroed metadata), so the
# output is reproducible byte-for-byte from the same source image.
( cd "${tmp}" && llvm-ar rcs "${OUT}" "${OBJS[@]}" )
cp "${tmp}/${OUT}" "${OUT}"

echo "Done. $(sha256sum "${OUT}")"
echo "Symbols provided:"
llvm-nm "${OUT}" | grep -E ' [TtWw] ' | awk '{print $3}' | sort -u | tr '\n' ' '
echo

# Guard: the freshly regenerated archive must match what we ship. If the source
# image or object list drifted, this fails loudly rather than silently shipping
# different bytes.
verify "${OUT}"
