// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package util reimplements the cc-utils argoci common-scripts in Go, since a
// distroless image has no bash to run them. Slated to move to a shared folder.
package util

import (
	"fmt"
	"os"
	"path/filepath"
)

// LocalSecret writes env var name to destPath at 0600, creating parent dirs. A
// missing env var is a no-op, as in the script it replaces, returning found=false
// so callers decide whether that is fatal.
func LocalSecret(name, destPath string) (found bool, err error) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir for %s: %w", destPath, err)
	}
	if err := writePrivate(destPath, []byte(v)); err != nil {
		return false, fmt.Errorf("write secret %s: %w", destPath, err)
	}
	return true, nil
}

// writePrivate writes data to destPath at 0600 even when destPath already exists.
// os.WriteFile applies its mode only on create, so a secret written over an existing
// 0644 path stayed readable. CreateTemp is 0600 regardless of umask.
func writePrivate(destPath string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(destPath), "."+filepath.Base(destPath)+".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once the rename below succeeds
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, destPath)
}

// MustLocalSecret is LocalSecret, but absent means error — for a secret the caller
// cannot proceed without.
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

// SetupComputeADC points Application Default Credentials at the compute SA key:
// the file at COMPUTE_SA_KEY if it exists, else the env var named by
// COMPUTE_SA_ENV materialized to a temp file.
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
	v, ok := os.LookupEnv(name)
	if !ok {
		return fmt.Errorf("compute SA: env var %q not set (set COMPUTE_SA_KEY to a mounted key file, or COMPUTE_SA_ENV to the key's env var name)", name)
	}
	// CreateTemp opens O_EXCL at 0600 with a random name, so it cannot follow a
	// pre-planted symlink or collide with another run.
	f, err := os.CreateTemp("", "compute-sa-*.json")
	if err != nil {
		return fmt.Errorf("compute SA: %w", err)
	}
	if _, err := f.WriteString(v); err != nil {
		f.Close()
		return fmt.Errorf("compute SA: write %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("compute SA: close %s: %w", f.Name(), err)
	}
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", f.Name())
	return nil
}
