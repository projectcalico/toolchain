// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package createvm creates the CI GCE VM from the ci-base image — docker, go,
// kind, kubectl and gh prebaked, so no startup script — and writes its zone to
// ZONE_OUT for the next workflow step. It does not wait or SSH; the run step's
// connect is the readiness check. Config comes from env vars the workflow sets;
// the compute SA is a mounted key file (COMPUTE_SA_KEY) or its env var (see util).
package createvm

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/projectcalico/go-build/scratch-utils/gce"
	"github.com/projectcalico/go-build/scratch-utils/util"
)

// createTimeout bounds the whole create: an operation that never reaches DONE
// should fail the step, not hang it until the workflow's own timeout.
const createTimeout = 10 * time.Minute

// Run executes the createvm subcommand and returns its exit code.
func Run() int {
	ctx, cancel := context.WithTimeout(context.Background(), createTimeout)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "createvm: %v\n", err)
		return 1
	}
	return 0
}

func run(ctx context.Context) error {
	name := os.Getenv("VM_NAME")
	if name == "" {
		return fmt.Errorf("VM_NAME must be set")
	}
	project := envOr("GCP_VM_PROJECT", "unique-caldron-775")
	zoneOut := envOr("ZONE_OUT", "/tmp/vm-zone")
	// Point ADC at the compute SA (mounted key file, or materialized from its env var).
	if err := util.SetupComputeADC(); err != nil {
		return err
	}

	maxRun, err := time.ParseDuration(envOr("GOOGLE_VM_MAX_RUN_DURATION", "90m"))
	if err != nil {
		return fmt.Errorf("GOOGLE_VM_MAX_RUN_DURATION: %w", err)
	}
	diskGB, err := parseDiskGB(envOr("GOOGLE_VM_DISK_SIZE", "200GB"))
	if err != nil {
		return err
	}

	client, err := gce.New(ctx, project)
	if err != nil {
		return err
	}

	cfg := gce.Config{
		Name:        name,
		Zones:       strings.Fields(envOr("GOOGLE_VM_ZONES", "us-central1-a us-central1-b us-central1-c us-central1-f")),
		MachineType: envOr("GOOGLE_VM_MACHINE_TYPE", "n2-standard-16"),
		DiskType:    envOr("GOOGLE_VM_DISK_TYPE", "pd-ssd"),
		DiskSizeGB:  diskGB,
		// ci-base has the toolchain baked in (built by vm-image/build-image.sh), so
		// the VM boots ready and the job does no installs. Override for stock Ubuntu.
		// GOOGLE_VM_IMAGE pins one exact image (e.g.
		// ci-base-1-27-0-llvm21-1-8-k8s1-37-0) so the image can be rolled without
		// rebuilding this binary; unset, the family gives whatever is newest.
		Image:        os.Getenv("GOOGLE_VM_IMAGE"),
		ImageFamily:  envOr("GOOGLE_VM_IMAGE_FAMILY", "ci-base"),
		ImageProject: envOr("GOOGLE_VM_IMAGE_PROJECT", "unique-caldron-775"),
		MaxRun:       maxRun,
		Labels: map[string]string{
			"ci-runner":   "true",
			"ci-project":  "kindrig",
			"ci-workflow": envOr("CI_WORKFLOW_LABEL", "unknown"),
		},
	}

	from := "family " + cfg.ImageFamily
	if cfg.Image != "" {
		from = "image " + cfg.Image
	}
	fmt.Printf("[createvm] creating %s (%s) from %s in %s across %v\n", name, cfg.MachineType, from, project, cfg.Zones)
	zone, err := client.Create(ctx, cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(zoneOut, []byte(zone), 0o644); err != nil {
		return fmt.Errorf("write zone to %s: %w", zoneOut, err)
	}
	// Readiness is the run step's job: runonvm must connect to ship and run anyway,
	// so that connect is the check.
	fmt.Printf("[createvm] %s created in %s (zone -> %s)\n", name, zone, zoneOut)
	return nil
}

// parseDiskGB accepts "200GB", "200G", or "200" and returns the GB count.
func parseDiskGB(s string) (int64, error) {
	// Errors quote s, not trimmed: report what the caller actually set.
	trimmed := strings.TrimSpace(strings.ToUpper(s))
	trimmed = strings.TrimSuffix(strings.TrimSuffix(trimmed, "GB"), "G")
	n, err := strconv.ParseInt(strings.TrimSpace(trimmed), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("GOOGLE_VM_DISK_SIZE %q: want e.g. 200GB: %w", s, err)
	}
	return n, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
