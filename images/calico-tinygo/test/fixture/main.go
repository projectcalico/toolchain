// Copyright (c) 2026 Tigera, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// fixture exercises the calico/tinygo image against two failure classes:
//
//  1. wasm-ld undefined symbols. TinyGo's bundled wasi-libc and the
//     prebuilt static archives shipped inside wasilibs Go modules must
//     agree on the libc/libc++ ABI. A trivial smoke (`package main; func
//     main(){}`) does NOT exercise this — TinyGo's stdlib provides every-
//     thing the smoke needs. This fixture forces the wasilibs static
//     archives in by importing and using github.com/wasilibs/go-re2 and
//     github.com/wasilibs/go-libinjection.
//
//  2. Runtime missing-import on Envoy. wasilibs's TinyGo path resolves
//     cre2_* internally to a bundled wasm static archive (libcre2 baked
//     into internal/wasm/). If a future go-re2 release stops shipping
//     that archive (as v1.8.0+ did when wasilibs removed TinyGo support),
//     wasm-ld emits the cre2_* calls as `env.cre2_*` imports and the
//     wasm fails to instantiate on Envoy with `missing import: env.cre2_new`.
//     The CI assertion in .semaphore/semaphore.yml catches this at
//     toolchain-image build time by counting `<- env.cre2_*` imports
//     in the produced wasm; non-zero is a fail.
//
// pattern + input are read from os.Args so TinyGo's `-opt=2` optimizer
// cannot prove the calls dead and constant-fold them — without this the
// optimizer eliminates the wasilibs calls entirely, the static archive
// never gets pulled in, and both assertions silently pass on a wasm
// that exercises nothing.
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
