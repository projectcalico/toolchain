// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import "testing"

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
