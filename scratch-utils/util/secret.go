// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package util reimplements the cc-utils argoci common-scripts
// (tigera/cc-utils/argoci-images/common-scripts) in Go, since a distroless image
// has no bash/git/ssh to run them. LocalSecret is createLocalSecret. Slated to
// move to a shared cc-util folder.
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
	if err := os.WriteFile(destPath, []byte(v), 0o600); err != nil {
		return false, fmt.Errorf("write secret %s: %w", destPath, err)
	}
	return true, nil
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

// SetupComputeADC points Application Default Credentials at the compute SA key.
// It prefers the file at COMPUTE_SA_KEY (a mounted secret volume), else
// materializes the key named by COMPUTE_SA_ENV to a temp file. Either way it sets
// GOOGLE_APPLICATION_CREDENTIALS, so no createLocalSecret step is needed.
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
