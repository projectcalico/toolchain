package main

import (
	"testing"
)

func TestContainerImageRegex(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{"ubuntu:latest", true},
		{"gcr.io/my-project/my-image:v1.2.3", true},
		{"docker.io/library/nginx@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"my-registry.local:5000/my-image:latest", true},
		{"gcr.io/test-project/test-image:v1-alpha_beta", true},

		// Invalid images (should fail validation)
		{"ubuntu:latest;id>/tmp/PWNED;#", false},
		{"ubuntu:latest\n/bin/sh", false},
		{"ubuntu:latest $(whoami)", false},
		{"ubuntu:latest' or 1=1", false},
		{"ubuntu:latest`id`", false},
		{"ubuntu:latest ;", false},
	}

	for _, tt := range tests {
		got := containerImageRegex.MatchString(tt.image)
		if got != tt.want {
			t.Errorf("containerImageRegex.MatchString(%q) = %t; want %t", tt.image, got, tt.want)
		}
	}
}
