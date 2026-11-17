// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The transfers end to end, over SSH to a bash standing in for the VM.

func TestSSHPutFileAndPutDir(t *testing.T) {
	needZstd(t)
	conn := testSSH(t)
	src := t.TempDir()
	plain := bytes.Repeat([]byte("calico "), 50000)
	if err := os.WriteFile(filepath.Join(src, "plain.bin"), plain, 0o755); err != nil {
		t.Fatal(err)
	}
	zst := append([]byte{0x28, 0xb5, 0x2f, 0xfd}, bytes.Repeat([]byte{7}, 1000)...)
	if err := os.WriteFile(filepath.Join(src, "image.tar.zst"), zst, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()

	// Compressed on the wire, and the already-compressed file sent as is: both
	// must land byte for byte, with their modes.
	for name, mode := range map[string]os.FileMode{"plain.bin": 0o755, "image.tar.zst": 0o644} {
		if err := conn.PutFile(filepath.Join(src, name), filepath.Join(dst, "files", name), mode); err != nil {
			t.Fatalf("PutFile %s: %v", name, err)
		}
		checkFile(t, filepath.Join(dst, "files", name), filepath.Join(src, name), mode)
	}

	if err := conn.PutDir(src, filepath.Join(dst, "dir")); err != nil {
		t.Fatalf("PutDir: %v", err)
	}
	checkFile(t, filepath.Join(dst, "dir", "plain.bin"), filepath.Join(src, "plain.bin"), 0o755)
	checkFile(t, filepath.Join(dst, "dir", "image.tar.zst"), filepath.Join(src, "image.tar.zst"), 0o644)
}

func TestSSHGetDir(t *testing.T) {
	needZstd(t)
	conn := testSSH(t)
	remote := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "report.xml"), []byte("<ok/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	if err := conn.GetDir(remote, local); err != nil {
		t.Fatalf("GetDir: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(local, "report.xml")); err != nil || string(got) != "<ok/>" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := conn.GetDir(filepath.Join(remote, "absent"), t.TempDir()); err != nil {
		t.Fatalf("GetDir of a missing directory: %v", err)
	}
}

// A local failure mid-stream leaves the remote decompressor failing on input
// cut short; the error must name the local cause, not only that.
func TestSSHRunWithInputReportsTheLocalFailure(t *testing.T) {
	needZstd(t)
	conn := testSSH(t)
	dest := filepath.Join(t.TempDir(), "payload")
	errDisk := errors.New("disk read failed")
	err := conn.runWithInput(putCommand(dest, 0o644), func(w io.Writer) error {
		if _, err := w.Write(bytes.Repeat([]byte("x"), 1<<20)); err != nil {
			return err
		}
		return errDisk
	})
	if !errors.Is(err, errDisk) {
		t.Fatalf("error = %v, want one wrapping %v", err, errDisk)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("destination exists after a failed put: %v", statErr)
	}
}

// An extract that fails part way must still return, not hang draining a pipe the
// decoder is also reading.
func TestSSHGetDirFailsCleanly(t *testing.T) {
	needZstd(t)
	conn := testSSH(t)
	remote := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "f.txt"), []byte(strings.Repeat("y", 1<<20)), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file where untar needs a directory makes the extract fail.
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "f.txt"), nil, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(local, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(local, 0o755) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only destination")
	}
	if err := conn.GetDir(remote, local); err == nil {
		t.Fatal("GetDir into a read-only destination succeeded")
	}
}

// A remote tar that fails says why, not only its exit status.
func TestSSHGetDirSaysWhyTheRemoteFailed(t *testing.T) {
	needZstd(t)
	if os.Geteuid() == 0 {
		t.Skip("root reads the unreadable file")
	}
	conn := testSSH(t)
	remote := t.TempDir()
	secret := filepath.Join(remote, "unreadable.txt")
	if err := os.WriteFile(secret, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	err := conn.GetDir(remote, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unreadable.txt") {
		t.Fatalf("error = %v, want the remote tar's own message naming the file", err)
	}
}

func checkFile(t *testing.T, got, want string, mode os.FileMode) {
	t.Helper()
	g, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g, w) {
		t.Errorf("%s: %d bytes differ from %s", got, len(g), want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Errorf("%s: mode %o, want %o", got, info.Mode().Perm(), mode)
	}
}
