// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package util is the Go reimplementation of the cc-utils argoci common-scripts
// (tigera/cc-utils/argoci-images/common-scripts) that a distroless/scratch image
// can't run because it has no bash/git/ssh binaries. LocalSecret is createLocalSecret;
// the SSH/clone equivalents live alongside so the VM-lifecycle binaries are
// self-sufficient. Slated to move to a shared cc-util folder.
package util

import (
	"fmt"
	"os"
	"path/filepath"
)

// LocalSecret writes the value of environment variable name to destPath (mode
// 0600, creating parent dirs) — the Go form of cc-utils' createLocalSecret, which
// materializes a mounted-secret env var to a file. Missing env var is a no-op (as
// the script is), returning found=false so callers can decide whether that's fatal.
func LocalSecret(name, destPath string) (found bool, err error) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir for %s: %w", destPath, err)
	}
	if err := os.WriteFile(destPath, []byte(v), 0o600); err != nil {
		return false, fmt.Errorf("write secret %s: %w", destPath, err)
	}
	return true, nil
}

// MustLocalSecret is LocalSecret but errors when the env var is absent — for a
// secret the caller can't proceed without (e.g. the compute SA key).
func MustLocalSecret(name, destPath string) error {
	found, err := LocalSecret(name, destPath)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("required secret env var %q not set", name)
	}
	return nil
}

// SetupComputeADC points Application Default Credentials at the compute
// service-account key so the GCP API clients authenticate. It prefers the file at
// COMPUTE_SA_KEY when that exists (a mounted secret volume); otherwise it
// materializes the key from the env var named by COMPUTE_SA_ENV (default the
// banzai SA key, an envFrom'd mounted secret) to a temp file. Either way it sets
// GOOGLE_APPLICATION_CREDENTIALS. This keeps the binary self-sufficient on a
// scratch image — no createLocalSecret step needed.
func SetupComputeADC() error {
	if p := os.Getenv("COMPUTE_SA_KEY"); p != "" {
		if _, err := os.Stat(p); err == nil {
			os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", p)
			return nil
		}
	}
	name := os.Getenv("COMPUTE_SA_ENV")
	if name == "" {
		name = "banzai-google-service-account.json"
	}
	dest := filepath.Join(os.TempDir(), "compute-sa.json")
	if err := MustLocalSecret(name, dest); err != nil {
		return fmt.Errorf("compute SA: %w (set COMPUTE_SA_KEY to a mounted key file, or COMPUTE_SA_ENV to the key's env var name)", err)
	}
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", dest)
	return nil
}
