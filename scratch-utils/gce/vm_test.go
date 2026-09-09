// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
)

// The family path always yields the newest member, so pinning an exact image is
// the only way a job can hold one steady while newer releases land in the family.
func TestSourceImage(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "family when no exact image is given",
			cfg:  Config{ImageProject: "unique-caldron-775", ImageFamily: "ci-base"},
			want: "projects/unique-caldron-775/global/images/family/ci-base",
		},
		{
			name: "exact image when given",
			cfg: Config{
				ImageProject: "unique-caldron-775",
				ImageFamily:  "ci-base",
				Image:        "ci-base-1-27-0-llvm21-1-8-k8s1-37-0",
			},
			want: "projects/unique-caldron-775/global/images/ci-base-1-27-0-llvm21-1-8-k8s1-37-0",
		},
		{
			name: "exact image wins over the family",
			cfg: Config{
				ImageProject: "ubuntu-os-cloud",
				ImageFamily:  "ubuntu-2404-lts-amd64",
				Image:        "ubuntu-2404-noble-amd64-v20260101",
			},
			want: "projects/ubuntu-os-cloud/global/images/ubuntu-2404-noble-amd64-v20260101",
		},
	} {
		if got := sourceImage(tc.cfg); got != tc.want {
			t.Errorf("%s: sourceImage() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// instanceSpec is what actually reaches the API, so make sure the pin survives it.
func TestInstanceSpecUsesPinnedImage(t *testing.T) {
	c := &Client{project: "unique-caldron-775"}
	inst := c.instanceSpec("us-central1-a", Config{
		Name:         "vm",
		MachineType:  "n2-standard-16",
		DiskType:     "pd-ssd",
		DiskSizeGB:   200,
		ImageProject: "unique-caldron-775",
		ImageFamily:  "ci-base",
		Image:        "ci-base-1-27-0-llvm21-1-8-k8s1-37-0",
	})
	got := inst.Disks[0].InitializeParams.SourceImage
	want := "projects/unique-caldron-775/global/images/ci-base-1-27-0-llvm21-1-8-k8s1-37-0"
	if got != want {
		t.Errorf("SourceImage = %q, want %q", got, want)
	}
}

func TestCreateRejectsEmptyZoneList(t *testing.T) {
	c := &Client{project: "p"}
	if _, err := c.Create(context.Background(), Config{Name: "vm"}); err == nil {
		t.Fatal("want an error when no zones are configured")
	}
}

// A dead context must not be reported as a per-zone failure: that is what made the
// original bug unreadable, with the last zone blamed for a deadline an earlier zone
// had already consumed. Every zone should say it was not attempted, and none should
// reach the API.
func TestCreateReportsUnattemptedZonesSeparately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// svc is nil: if any zone reached the API this would panic, which is the point.
	c := &Client{project: "p"}
	_, err := c.Create(ctx, Config{Name: "vm", Zones: []string{"z-a", "z-b", "z-c"}})
	if err == nil {
		t.Fatal("want an error")
	}
	got := err.Error()
	for _, z := range []string{"z-a", "z-b", "z-c"} {
		if !strings.Contains(got, z+": not attempted") {
			t.Errorf("error should name %s as not attempted; got: %s", z, got)
		}
	}
}

// Delete treats a not-found instance as already deleted, so this decides whether
// a cleanup step is idempotent or spuriously fails.
func TestIsNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"googleapi 404", &googleapi.Error{Code: 404, Message: "not found"}, true},
		{"googleapi 404 wrapped", fmt.Errorf("delete vm: %w", &googleapi.Error{Code: 404}), true},
		{"googleapi 403", &googleapi.Error{Code: 403, Message: "forbidden"}, false},
		{"googleapi 500", &googleapi.Error{Code: 500}, false},
		// A permission error whose prose happens to contain the old sentinel must
		// not be read as "already gone", or cleanup would silently skip a live VM.
		{"prose mentioning notFound", errors.New("caller lacks permission; resource notFound checks disabled"), false},
		{"unrelated", errors.New("connection reset"), false},
	} {
		if got := isNotFound(tc.err); got != tc.want {
			t.Errorf("%s: isNotFound = %v, want %v", tc.name, got, tc.want)
		}
	}
}
