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
	// A secret readable by anyone but the owner would defeat the point.
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600", perm)
	}
}

// An unset var is a no-op, not an error -- the caller decides whether a missing
// secret is fatal (that is what MustLocalSecret is for).
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
