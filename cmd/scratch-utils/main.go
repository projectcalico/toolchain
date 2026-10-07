// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Command scratch-utils bundles the CI VM helpers into one binary dispatched by
// subcommand -- one image, different args, not four binaries:
//
//	scratch-utils createvm                       create the CI GCE VM (config from env)
//	scratch-utils deletevm                       delete it by name, best-effort
//	scratch-utils secret <ENV_VAR> <DEST_PATH>   env var -> file
//	scratch-utils runonvm [flags] <script>       run a script on the VM over SSH
//
// No gcloud or bash dependency, so it runs from a distroless image. The ENTRYPOINT
// is this binary, so a pod passes the subcommand as an ARGUMENT -- `args:
// [createvm]`; a bare `command: [createvm]` replaces the entrypoint and fails.
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
	// Each subcommand then sees its own args as os.Args[1:].
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
