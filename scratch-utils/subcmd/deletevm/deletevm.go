// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package deletevm deletes the kind-rig CI GCE VM by name (finding its zone if
// ZONE isn't given), over the compute API — no gcloud. Best-effort: the VM's
// max-run-duration is the ultimate backstop, so a failure here is logged, not
// fatal. Runs from the same scratch image as createvm, in the workflow's onExit
// cleanup step.
package deletevm

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/projectcalico/go-build/scratch-utils/gce"
	"github.com/projectcalico/go-build/scratch-utils/util"
)

// Run executes the deletevm subcommand and returns its exit code. Best-effort:
// any failure returns 0 (the VM's max-run-duration is the backstop).
func Run() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := os.Getenv("VM_NAME")
	if name == "" {
		fmt.Fprintln(os.Stderr, "deletevm: VM_NAME must be set")
		return 1
	}
	project := envOr("GCP_VM_PROJECT", "unique-caldron-775")
	// Materialize the compute SA from its env var (COMPUTE_SA_ENV) -> ADC, exactly
	// like createvm. The cleanup step injects the SA as an env var (banzai-secrets),
	// NOT a mounted file -- pointing ADC at a nonexistent /secrets file made every
	// delete fail auth, leaking VMs to the max-run-duration backstop.
	if err := util.SetupComputeADC(); err != nil {
		fmt.Fprintf(os.Stderr, "deletevm: %v (leaving to max-run-duration)\n", err)
		return 0
	}

	client, err := gce.New(ctx, project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deletevm: %v (leaving to max-run-duration)\n", err)
		return 0
	}

	zone := os.Getenv("ZONE")
	if zone == "" {
		if zone, err = client.FindZone(ctx, name); err != nil {
			fmt.Fprintf(os.Stderr, "deletevm: find zone for %s: %v (leaving to max-run-duration)\n", name, err)
			return 0
		}
	}
	if zone == "" {
		fmt.Printf("[deletevm] no VM %s found; nothing to delete\n", name)
		return 0
	}

	fmt.Printf("[deletevm] deleting %s in %s\n", name, zone)
	if err := client.Delete(ctx, zone, name); err != nil {
		fmt.Fprintf(os.Stderr, "deletevm: %v (leaving to max-run-duration)\n", err)
		return 0
	}
	fmt.Printf("[deletevm] deleted %s\n", name)
	return 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
