// Copyright (c) 2026 Tigera, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// fixture exercises the calico/tinygo image against the failure class
// where TinyGo's bundled wasi-libc and the prebuilt static archives shipped
// inside wasilibs Go modules disagree on the libc/libc++ ABI. A trivial
// smoke (`package main; func main(){}`) does NOT catch this — TinyGo's
// stdlib provides everything the smoke needs.
//
// The regression only fires when wasm-ld has to resolve symbols out of
// vendored `wasilibs/<lib>/wasm/*.a` archives. This fixture forces those
// archives in by importing and calling go-re2 (libc++.a) and go-libinjection
// (libinjection wasm static archive) — the same dependencies the
// gateway/coraza-wasm consumer pulls in. If a future TinyGo bump breaks
// either prebuilt archive's libc/libc++ ABI, this fixture fails to compile
// in the calico/tinygo Semaphore block and surfaces the regression at
// toolchain-image build time rather than in a downstream WAF rebuild.
//
// Build flags must match the gateway/coraza-wasm production set; see
// .semaphore/semaphore.yml block "calico/tinygo image" for invocation.
package main

import (
	"github.com/wasilibs/go-libinjection"
	"github.com/wasilibs/go-re2"
)

func main() {
	re := re2.MustCompile(`^[a-z]+$`)
	if !re.MatchString("hello") {
		println("re2: regex match regression")
		return
	}

	if sqli, _ := libinjection.IsSQLi("' OR 1=1--"); !sqli {
		println("libinjection: sqli detection regression")
		return
	}

	println("calico/tinygo fixture OK")
}
