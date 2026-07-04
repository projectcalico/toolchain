# ctypecompat — musl ctype shim for TinyGo ≥ 0.38

`libctypecompat.a` supplies the C `ctype`/`wctype` functions that TinyGo ≥ 0.38
no longer emits into its bundled wasi-libc. It is baked into the `calico/tinygo`
image at `/usr/local/lib/ctypecompat/libctypecompat.a` (exported as
`$CALICO_TINYGO_CTYPE_COMPAT`).

## Why this exists

WAF builds (`gateway/coraza-wasm` in projectcalico/calico) link prebuilt static
wasm archives that ship inside their Go modules:

- `github.com/wasilibs/go-re2` v1.6.0 → `libc++.a`, `libc++abi.a`, `libre2.a`, `libcre2.a`
- `github.com/wasilibs/nottinygc` v0.7.1 → `libmimalloc.a`, `libgc.a`

Those archives were compiled against a full wasi-sdk sysroot, so they reference
the standard C `ctype` functions — `toupper`, `tolower`, `isalpha`, `isspace`,
`isxdigit`, the `is*_l` locale variants, and the wide-char `isw*_l` / `tow*_l`
family — as **external** symbols, expecting libc to provide them. Both modules
are frozen: nottinygc is archived/EOL, and go-re2 v1.8.0+ dropped TinyGo support
(re2 now needs Abseil threads TinyGo lacks), so WAF stays on these versions.

TinyGo **0.34** built its bundled wasi-libc with upstream's Makefile, which
compiled `libc-top-half/musl/src/ctype/*.c`, so those symbols were present in
`libc.a` and the archives linked cleanly.

TinyGo **0.38** replaced that with an in-tree Go builder
(`builder/wasilibc.go`, upstream PR
[tinygo-org/tinygo#4820](https://github.com/tinygo-org/tinygo/pull/4820)) that
re-lists the wasi-libc source globs by hand — and **omits the `ctype/` glob**.
TinyGo ≥ 0.38 therefore no longer emits those symbols, and the WAF archives fail
to link:

```
wasm-ld: error: libc++.a(locale.cpp.o): undefined symbol: toupper
wasm-ld: error: libmimalloc.a(options.c.obj): undefined symbol: toupper
```

This is a **toolchain build-config regression, not a wasi-libc change**: TinyGo
0.34 and 0.39 pin the *same* wasi-libc commit. Only the set of sources TinyGo
compiles into its libc changed.

## What `libctypecompat.a` is

The exact ctype/wctype object files lifted **verbatim** from TinyGo 0.34's
prebuilt wasi-libc (`calico/tinygo:0.34.0`,
`/usr/local/tinygo/lib/wasi-libc/sysroot/lib/wasm32-wasi/libc.a`). Because the
wasi-libc commit is identical between TinyGo 0.34 and the version this image
ships, these objects are ABI-compatible; we reuse the already-compiled
artifacts rather than re-deriving musl's arch headers by hand.

## How consumers use it

Append it to the TinyGo wasm link:

```
tinygo build -target=wasip1 -gc=custom ... \
  -ldflags="-extldflags=$CALICO_TINYGO_CTYPE_COMPAT" ...
```

`wasm-ld` then resolves the go-re2 / nottinygc archive references against these
real musl implementations. The image's own test fixture
(`../test/fixture`) is built this way in CI as a regression guard.

## Regenerating

Run `./regen.sh` (needs `docker` + host `llvm-ar`). It pulls the source TinyGo
image, extracts its `libc.a`, and re-archives the ctype object members. See the
script for the exact object list. Source image digest at time of generation:
`calico/tinygo@sha256:9ff0bed2e3598f695a2fc6222be901eb0f3a0153549b82d28aa640c8487e1854`.

## When this can go away

Drop this shim once TinyGo restores the `ctype/` glob in `builder/wasilibc.go`
(worth filing upstream — the curated subset also dropped `regex/`, `prng/`,
`search/`, etc.), or once WAF builds move off the prebuilt go-re2 / nottinygc
archives (e.g. a Rust rewrite).
