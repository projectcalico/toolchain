// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Command scratch-utils bundles the CI VM helpers into a single binary, dispatched by
// subcommand -- one image, different args, not four binaries:
//
//	scratch-utils createvm                       create the CI GCE VM (config from env)
//	scratch-utils deletevm                       delete it by name (best-effort cleanup)
//	scratch-utils secret <ENV_VAR> <DEST_PATH>   materialize a mounted-secret env var to a file
//	scratch-utils runonvm [flags] <script>       run a script on the VM over SSH
//
// It has no gcloud/bash dependency, so it runs from a scratch/distroless image.
package main

import (
	"fmt"
	"os"

	"github.com/projectcalico/go-build/scratch-utils/subcmd/createvm"
	"github.com/projectcalico/go-build/scratch-utils/subcmd/deletevm"
	"github.com/projectcalico/go-build/scratch-utils/subcmd/runonvm"
	"github.com/projectcalico/go-build/scratch-utils/subcmd/secret"
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
		fmt.Fprintf(os.Stderr, "scratch-utils: unknown subcommand %q\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: scratch-utils <createvm|deletevm|secret|runonvm> [args]")
}
