// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package secret materializes a mounted-secret env var to a file — the Go form of
// cc-utils' createLocalSecret, for the scratch image. Usage: secret NAME PATH.
package secret

import (
	"fmt"
	"os"

	"github.com/projectcalico/go-build/scratch-utils/util"
)

// Run executes the secret subcommand and returns its exit code.
func Run() int {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: secret <ENV_VAR_NAME> <DEST_PATH>")
		return 2
	}
	found, err := util.LocalSecret(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "secret: %v\n", err)
		return 1
	}
	if !found {
		fmt.Printf("secret %s not created (env var not set)\n", os.Args[1])
		return 0
	}
	fmt.Printf("created %s\n", os.Args[2])
	return 0
}
