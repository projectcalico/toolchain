// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Command cctools bundles the CI VM helpers into a single binary, dispatched by
// subcommand -- one image, different args, not four binaries:
//
//	cctools createvm                       create the CI GCE VM (config from env)
//	cctools deletevm                       delete it by name (best-effort cleanup)
//	cctools secret <ENV_VAR> <DEST_PATH>   materialize a mounted-secret env var to a file
//	cctools runonvm [flags] <script>       run a script on the VM over SSH
//
// It has no gcloud/bash dependency, so it runs from a scratch/distroless image.
package main

import (
	"fmt"
	"os"

	"github.com/projectcalico/go-build/cctools/subcmd/createvm"
	"github.com/projectcalico/go-build/cctools/subcmd/deletevm"
	"github.com/projectcalico/go-build/cctools/subcmd/runonvm"
	"github.com/projectcalico/go-build/cctools/subcmd/secret"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	// Re-slice so each subcommand sees its own args as os.Args[1:] (its flag
	// parsing and positional args work unchanged).
	os.Args = append([]string{os.Args[0] + " " + sub}, os.Args[2:]...)

	switch sub {
	case "createvm":
		os.Exit(createvm.Run())
	case "deletevm":
		os.Exit(deletevm.Run())
	case "secret":
		os.Exit(secret.Run())
	case "runonvm":
		os.Exit(runonvm.Run())
	default:
		fmt.Fprintf(os.Stderr, "cctools: unknown subcommand %q\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cctools <createvm|deletevm|secret|runonvm> [args]")
}
