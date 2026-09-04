// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package gce creates and deletes a GCE VM via the compute API and drives it over
// SSH (ssh.go) — no gcloud, so callers can run from a distroless image. Create
// does not wait for the VM to be usable; DialSSH's retry is the readiness check.
package gce

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"
)

// Config describes the VM to create. Zones are tried in order (capacity), and the
// first that succeeds is returned. The project is the Client's, not a field here.
type Config struct {
	Name          string
	Zones         []string
	MachineType   string // e.g. "n2-standard-16"
	DiskType      string // e.g. "pd-ssd"
	DiskSizeGB    int64
	Image         string // exact image name; wins over ImageFamily when set
	ImageFamily   string // e.g. "ubuntu-2404-lts-amd64"
	ImageProject  string // e.g. "ubuntu-os-cloud"
	MaxRun        time.Duration
	Labels        map[string]string
	StartupScript string // bash run as the GCE startup-script (on the VM)
}

// Client is a compute API client scoped to one project.
type Client struct {
	svc     *compute.Service
	project string
}

// New builds a client using Application Default Credentials. Point
// GOOGLE_APPLICATION_CREDENTIALS at the service-account key (a mounted secret)
// before calling -- the cmd wrappers do this from COMPUTE_SA_KEY.
func New(ctx context.Context, project string) (*Client, error) {
	svc, err := compute.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("compute client: %w", err)
	}
	return &Client{svc: svc, project: project}, nil
}

// PerZoneTimeout bounds ONE zone's attempt. The zone list exists so a zone short
// on capacity can be skipped, which only works if each gets its own budget: with a
// single deadline spanning the loop, one slow zone consumed all of it and every
// later zone failed its insert instantly with "context deadline exceeded",
// reporting a zone that was never really tried and hiding the actual failure.
const PerZoneTimeout = 3 * time.Minute

// Create inserts the instance in the first zone that accepts it, waits for the
// insert to finish, and returns that zone. The VM gets an external IP,
// cloud-platform scope, and a max-run-duration GCP reclaims it at — a leaked-VM
// backstop independent of any cleanup step.
//
// Every zone's error is reported, not just the last: which zones were out of
// capacity and which were never reached is exactly what you need from a CI log.
func (c *Client) Create(ctx context.Context, cfg Config) (zone string, err error) {
	if len(cfg.Zones) == 0 {
		return "", fmt.Errorf("no zones configured")
	}
	var errs []error
	for _, z := range cfg.Zones {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("%s: not attempted: %w", z, err))
			continue
		}
		fmt.Fprintf(os.Stderr, "[gce] trying %s in %s\n", cfg.Name, z)
		if err := c.createInZone(ctx, z, cfg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", z, err))
			continue
		}
		return z, nil
	}
	return "", fmt.Errorf("could not create %s in any zone: %w", cfg.Name, errors.Join(errs...))
}

// createInZone attempts one zone under its own deadline.
func (c *Client) createInZone(ctx context.Context, zone string, cfg Config) error {
	zctx, cancel := context.WithTimeout(ctx, PerZoneTimeout)
	defer cancel()

	op, err := c.svc.Instances.Insert(c.project, zone, c.instanceSpec(zone, cfg)).Context(zctx).Do()
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	if err := c.waitZoneOp(zctx, zone, op.Name); err != nil {
		// The insert was accepted, so an instance may exist or be mid-create even
		// though we are moving on. Left behind it would run to max-run-duration while
		// we boot another elsewhere -- two VMs for one job.
		c.deleteBestEffort(zone, cfg.Name)
		return fmt.Errorf("wait for insert: %w", err)
	}
	return nil
}

// deleteBestEffort removes an instance we are abandoning. It builds its own
// context: the caller's is typically already expired, which is why we are here.
func (c *Client) deleteBestEffort(zone, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fmt.Fprintf(os.Stderr, "[gce] cleaning up abandoned %s in %s\n", name, zone)
	if err := c.Delete(ctx, zone, name); err != nil {
		fmt.Fprintf(os.Stderr, "[gce] cleanup of %s in %s failed: %v (max-run-duration is the backstop)\n", name, zone, err)
	}
}

func (c *Client) instanceSpec(zone string, cfg Config) *compute.Instance {
	inst := &compute.Instance{
		Name:        cfg.Name,
		MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", zone, cfg.MachineType),
		Labels:      cfg.Labels,
		Disks: []*compute.AttachedDisk{{
			Boot:       true,
			AutoDelete: true,
			InitializeParams: &compute.AttachedDiskInitializeParams{
				SourceImage: sourceImage(cfg),
				DiskSizeGb:  cfg.DiskSizeGB,
				DiskType:    fmt.Sprintf("zones/%s/diskTypes/%s", zone, cfg.DiskType),
			},
		}},
		// One NAT access config = an ephemeral external IP (SSH from the run step).
		NetworkInterfaces: []*compute.NetworkInterface{{
			AccessConfigs: []*compute.AccessConfig{{Type: "ONE_TO_ONE_NAT", Name: "External NAT"}},
		}},
		ServiceAccounts: []*compute.ServiceAccount{{
			Email:  "default",
			Scopes: []string{compute.CloudPlatformScope},
		}},
	}
	// Optional: the ci-base image is already provisioned, so createvm passes none.
	// Only set it for a stock image.
	if cfg.StartupScript != "" {
		inst.Metadata = &compute.Metadata{Items: []*compute.MetadataItems{
			{Key: "startup-script", Value: strPtr(cfg.StartupScript)},
		}}
	}
	if cfg.MaxRun > 0 {
		inst.Scheduling = &compute.Scheduling{
			ProvisioningModel:         "STANDARD",
			InstanceTerminationAction: "DELETE",
			MaxRunDuration:            &compute.Duration{Seconds: int64(cfg.MaxRun.Seconds())},
		}
	}
	return inst
}

// Delete removes the instance and waits for the delete operation to finish. A
// not-found instance is treated as already deleted.
func (c *Client) Delete(ctx context.Context, zone, name string) error {
	op, err := c.svc.Instances.Delete(c.project, zone, name).Context(ctx).Do()
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete %s in %s: %w", name, zone, err)
	}
	return c.waitZoneOp(ctx, zone, op.Name)
}

// FindZone returns the zone an instance of this name lives in, or "" if none.
// Lets a zone-agnostic cleanup delete a VM without carrying its zone.
func (c *Client) FindZone(ctx context.Context, name string) (string, error) {
	// The filter value must be quoted: an unquoted name containing - or . is a
	// syntax error, not a non-match. AggregatedList spans every zone, so it pages
	// even when the filter matches one instance.
	call := c.svc.Instances.AggregatedList(c.project).Filter(fmt.Sprintf("name=%q", name))
	for {
		agg, err := call.Context(ctx).Do()
		if err != nil {
			return "", err
		}
		for scope, list := range agg.Items {
			if len(list.Instances) == 0 {
				continue
			}
			// scope is "zones/<zone>".
			return strings.TrimPrefix(scope, "zones/"), nil
		}
		if agg.NextPageToken == "" {
			return "", nil
		}
		call = call.PageToken(agg.NextPageToken)
	}
}

// waitZoneOp blocks until a zone operation reaches DONE, surfacing its error.
func (c *Client) waitZoneOp(ctx context.Context, zone, op string) error {
	for {
		got, err := c.svc.ZoneOperations.Wait(c.project, zone, op).Context(ctx).Do()
		if err != nil {
			return err
		}
		if got.Status == "DONE" {
			if got.Error != nil && len(got.Error.Errors) > 0 {
				return fmt.Errorf("%s: %s", got.Error.Errors[0].Code, got.Error.Errors[0].Message)
			}
			return nil
		}
	}
}

// sourceImage resolves the boot image: an exact name when Image is set, else the
// family, which always yields its newest member. Pinning matters now that images
// are named per go-build release -- a job can hold an image steady while newer
// ones land in the family.
func sourceImage(cfg Config) string {
	if cfg.Image != "" {
		return fmt.Sprintf("projects/%s/global/images/%s", cfg.ImageProject, cfg.Image)
	}
	return fmt.Sprintf("projects/%s/global/images/family/%s", cfg.ImageProject, cfg.ImageFamily)
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "notFound")
}

func strPtr(s string) *string { return &s }
