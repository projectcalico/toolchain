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
// pattern + input are read from os.Args so TinyGo's `-opt=2` optimizer
// cannot prove the calls dead and constant-fold them — without this the
// optimizer eliminates the wasilibs calls entirely, the static archive
// never gets pulled in, and the assertion silently passes on a wasm that
// exercises nothing.
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
