// Copyright (c) 2026 Tigera, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// fixture is a TinyGo-vs-go-re2-v1.6 compat check. wasilibs/go-re2 v1.8.0
// removed TinyGo support upstream (see wasilibs/go-re2#161 — libre2 now
// requires Abseil with thread synchronization, which TinyGo's libc++ shim
// does not provide), so coraza-proxy-wasm and gateway/coraza-wasm are
// permanently frozen on go-re2 v1.6.0. The TinyGo version we ship in this
// image must continue to link cleanly against v1.6.0's prebuilt wasm
// static archives (`internal/wasm/{libcre2,libc++,libmimalloc}.a`) for
// downstream WAF builds to keep working.
//
// CI assertion (in .semaphore/semaphore.yml): build this fixture against
// the just-built calico/tinygo image and count `<- env.cre2_*` imports
// in the produced wasm. Expect zero — cre2_* must resolve internally to
// the bundled libcre2.a. Two failure modes are caught:
//
//  1. TinyGo bumped to a release whose wasi-libc no longer matches what
//     v1.6.0's archives expect → wasm-ld undefined-symbol error at build
//     time, fixture fails to compile, CI block red.
//
//  2. Build-flag drift causes cre2_* to be imported from `env` instead of
//     resolved internally → fixture compiles, but assertion finds non-zero
//     env.cre2_* count and fails the CI block red. (This is the same
//     missing-import-at-instantiate failure mode `gateway/coraza-wasm`
//     would have hit during a downstream WAF rebuild.)
//
// Both assertions concern go-re2/cre2 only; every ctype symbol the shim
// resolves comes from go-re2's archives. go-libinjection is imported to prove
// it also compiles and links under the shipped TinyGo, but it contributes
// nothing to the cre2 assertions above.
//
// pattern + input are read from os.Args to keep the inputs non-constant, but
// the wasilibs calls are not dead-code-eliminated regardless: they cross the
// wasm-import boundary and have observable effects (re2.MustCompile panics on
// a bad pattern), so `-opt=2` cannot prove them dead even with constant inputs.
// The static archive is pulled in and the ctype shim is exercised either way.
package main

import (
	"os"

	"github.com/wasilibs/go-libinjection"
	"github.com/wasilibs/go-re2"
)

func main() {
	pattern := `^[a-z]+$`
	input := "hello"
	if len(os.Args) > 1 {
		pattern = os.Args[1]
	}
	if len(os.Args) > 2 {
		input = os.Args[2]
	}

	re := re2.MustCompile(pattern)
	if !re.MatchString(input) {
		println("re2: regex match regression")
		return
	}

	if sqli, _ := libinjection.IsSQLi(input); sqli {
		println("libinjection: unexpected sqli on benign input")
		return
	}

	println("calico/tinygo fixture OK")
}
