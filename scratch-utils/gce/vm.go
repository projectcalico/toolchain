// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package gce creates and deletes a GCE VM via the compute API, and drives it over
// SSH (see ssh.go) — no gcloud CLI, so the caller can run from a scratch/distroless
// image. Create does not wait for the VM to be usable: DialSSH's retry is the
// readiness check.
package gce

import (
	"context"
	"fmt"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"
)

// Config describes the VM to create. Zones are tried in order (capacity), and the
// first that succeeds is returned.
type Config struct {
	Project       string
	Name          string
	Zones         []string
	MachineType   string // e.g. "n2-standard-16"
	DiskType      string // e.g. "pd-ssd"
	DiskSizeGB    int64
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

// Create inserts the instance in the first zone that accepts it and waits for the
// insert operation to finish, returning that zone. The VM gets an external IP,
// cloud-platform scope, and a max-run-duration whose
// deadline GCP reclaims the VM at (DELETE) — a leaked-VM backstop independent of
// any cleanup step.
func (c *Client) Create(ctx context.Context, cfg Config) (zone string, err error) {
	var lastErr error
	for _, z := range cfg.Zones {
		inst := c.instanceSpec(z, cfg)
		op, err := c.svc.Instances.Insert(cfg.Project, z, inst).Context(ctx).Do()
		if err != nil {
			lastErr = fmt.Errorf("insert in %s: %w", z, err)
			continue
		}
		if err := c.waitZoneOp(ctx, z, op.Name); err != nil {
			lastErr = fmt.Errorf("insert op in %s: %w", z, err)
			continue
		}
		return z, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no zones configured")
	}
	return "", fmt.Errorf("could not create %s in any zone: %w", cfg.Name, lastErr)
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
				SourceImage: fmt.Sprintf("projects/%s/global/images/family/%s", cfg.ImageProject, cfg.ImageFamily),
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
	// A startup script is optional: the ci-base image already has docker etc., so
	// createvm passes none. Only set it for a stock image that needs provisioning.
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
	agg, err := c.svc.Instances.AggregatedList(c.project).Filter("name=" + name).Context(ctx).Do()
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
	return "", nil
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

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "notFound")
}

func strPtr(s string) *string { return &s }
