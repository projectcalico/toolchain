// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSecretWritesModeAndContent(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "nested", "dir", "secret")
	t.Setenv("TEST_SECRET_VAR", "s3cr3t")

	found, err := LocalSecret("TEST_SECRET_VAR", dest)
	if err != nil || !found {
		t.Fatalf("LocalSecret: found=%v err=%v", found, err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "s3cr3t" {
		t.Errorf("content = %q, want %q", got, "s3cr3t")
	}
	// Readable by anyone but the owner would defeat the point.
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600", perm)
	}
}

// An unset var is a no-op, not an error: the caller decides whether that is fatal,
// which is what MustLocalSecret is for.
func TestLocalSecretMissingVarIsNoOp(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "secret")
	found, err := LocalSecret("TEST_SECRET_VAR_DEFINITELY_UNSET", dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("found = true for an unset env var")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("a file was created for an unset env var")
	}
}

func TestLocalSecretEmptyValueStillWrites(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "secret")
	t.Setenv("TEST_SECRET_EMPTY", "")
	found, err := LocalSecret("TEST_SECRET_EMPTY", dest)
	if err != nil || !found {
		t.Fatalf("a set-but-empty var should still be written: found=%v err=%v", found, err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMustLocalSecretErrorsWhenUnset(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "secret")
	if err := MustLocalSecret("TEST_SECRET_VAR_DEFINITELY_UNSET", dest); err == nil {
		t.Fatal("want an error for a required-but-unset secret")
	}
}

// The key must land on a private, per-run path: O_EXCL at 0600 with a random
// name, so it cannot follow a symlink or collide with a concurrent run.
func TestSetupComputeADCUsesAPrivateUniqueFile(t *testing.T) {
	t.Setenv("COMPUTE_SA_KEY", "")
	t.Setenv("COMPUTE_SA_ENV", "TEST_SA_JSON")
	t.Setenv("TEST_SA_JSON", `{"type":"service_account"}`)

	var paths []string
	for range 2 {
		if err := SetupComputeADC(); err != nil {
			t.Fatalf("SetupComputeADC: %v", err)
		}
		p := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		paths = append(paths, p)
		t.Cleanup(func() { os.Remove(p) })

		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != `{"type":"service_account"}` {
			t.Errorf("content = %q", got)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("mode = %#o, want 0600", perm)
		}
	}
	// Two runs in one namespace must not fight over the same file.
	if paths[0] == paths[1] {
		t.Errorf("both runs used the same path %q", paths[0])
	}
}

func TestSetupComputeADCErrorsWhenTheEnvVarIsMissing(t *testing.T) {
	t.Setenv("COMPUTE_SA_KEY", "")
	t.Setenv("COMPUTE_SA_ENV", "TEST_SA_DEFINITELY_UNSET")
	if err := SetupComputeADC(); err == nil {
		t.Fatal("want an error when the named env var is unset")
	}
}

// os.WriteFile applies its mode only when it creates the file, so writing a secret
// over an existing world-readable path left it world-readable -- the helper's whole
// contract is that it does not.
func TestLocalSecretTightensAnExistingPermissiveFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(dest, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET_OVERWRITE", "fresh")

	found, err := LocalSecret("TEST_SECRET_OVERWRITE", dest)
	if err != nil {
		t.Fatalf("LocalSecret: %v", err)
	}
	if !found {
		t.Fatal("expected the secret to be found")
	}

	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %04o, want 0600", got)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fresh" {
		t.Errorf("content = %q, want %q", got, "fresh")
	}

	// The staging file must not survive next to the secret.
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("leftover files beside the secret: %v", names)
	}
}
