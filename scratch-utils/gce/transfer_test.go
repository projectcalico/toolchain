// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/projectcalico/go-build/scratch-utils/util"
)

// The remote half of each transfer is a shell command run on the VM, so these run
// it locally against the Go half. They need the zstd and GNU tar the VM has.
func needZstd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("zstd CLI not installed")
	}
}

// runLocal runs cmd under bash with what write produces, through writeStream, as
// its stdin.
func runLocal(t *testing.T, cmd string, raw bool, write func(io.Writer) error) {
	t.Helper()
	var in bytes.Buffer
	if err := writeStream(&in, raw, write); err != nil {
		t.Fatalf("writeStream: %v", err)
	}
	c := exec.Command("bash", "-c", cmd)
	c.Stdin = &in
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", cmd, err, out)
	}
}

func TestPutFileCompressedRoundTrip(t *testing.T) {
	needZstd(t)
	dest := filepath.Join(t.TempDir(), "sub", "payload.bin")
	want := []byte(strings.Repeat("calico ", 10000))
	runLocal(t, putCommand(dest, 0o640, true), false, func(w io.Writer) error {
		_, err := w.Write(want)
		return err
	})
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content differs: got %d bytes, want %d", len(got), len(want))
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 640", info.Mode().Perm())
	}
}

// A truncated zstd stream must fail the put, not land as a short file.
func TestPutFileRejectsATruncatedStream(t *testing.T) {
	needZstd(t)
	dest := filepath.Join(t.TempDir(), "payload.bin")
	var in bytes.Buffer
	if err := writeStream(&in, false, func(w io.Writer) error {
		_, err := w.Write(bytes.Repeat([]byte("x"), 1<<20))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("bash", "-c", putCommand(dest, 0o644, true))
	c.Stdin = bytes.NewReader(in.Bytes()[:in.Len()/2])
	if err := c.Run(); err == nil {
		t.Fatal("a truncated stream was accepted")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination exists after a failed put: %v", err)
	}
}

func TestPutDirCompressedRoundTrip(t *testing.T) {
	needZstd(t)
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a", "b", "f.txt"), []byte("hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	q := util.ShellQuote(dst)
	runLocal(t, "mkdir -p "+q+" && tar --zstd -xf - -C "+q, false, func(w io.Writer) error {
		return tarDir(src, w)
	})
	got, err := os.ReadFile(filepath.Join(dst, "a", "b", "f.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestGetDirStreamRoundTrip(t *testing.T) {
	needZstd(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "report.xml"), []byte("<ok/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-c", getCommand(src)).Output()
	if err != nil {
		t.Fatalf("getCommand: %v", err)
	}
	zr, err := zstd.NewReader(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	dst := t.TempDir()
	if err := untar(zr, dst); err != nil {
		t.Fatalf("untar: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "report.xml"))
	if err != nil || string(got) != "<ok/>" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// A missing remote directory sends nothing; GetDir's decoder must read that as an
// empty archive, not an error.
func TestGetDirMissingRemoteIsEmpty(t *testing.T) {
	out, err := exec.Command("bash", "-c", getCommand(filepath.Join(t.TempDir(), "absent"))).Output()
	if err != nil {
		t.Fatalf("getCommand: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("missing dir sent %d bytes", len(out))
	}
	zr, err := zstd.NewReader(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if err := untar(zr, t.TempDir()); err != nil {
		t.Fatalf("untar of an empty stream: %v", err)
	}
}

func TestIsCompressed(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		content []byte
		want    bool
	}{
		"zstd":  {[]byte{0x28, 0xb5, 0x2f, 0xfd, 0, 0}, true},
		"gzip":  {[]byte{0x1f, 0x8b, 8, 0}, true},
		"xz":    {[]byte{0xfd, '7', 'z', 'X', 'Z', 0, 1}, true},
		"tar":   {[]byte("ustar\x00 plain archive"), false},
		"text":  {[]byte("#!/bin/bash\n"), false},
		"empty": {nil, false},
		"short": {[]byte{0x1f}, false},
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, tc.content, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := isCompressed(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: isCompressed = %v, want %v", name, got, tc.want)
		}
	}
}

func TestAllCompressed(t *testing.T) {
	zst := []byte{0x28, 0xb5, 0x2f, 0xfd, 0, 0}
	write := func(t *testing.T, dir, name string, b []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	only := t.TempDir()
	write(t, only, "calico-image.tar.zst", zst)
	if got, err := allCompressed(only); err != nil || !got {
		t.Errorf("all compressed: got %v, %v", got, err)
	}

	mixed := t.TempDir()
	write(t, mixed, "image.tar.zst", zst)
	write(t, mixed, "win-fv.exe", []byte("MZ"))
	if got, err := allCompressed(mixed); err != nil || got {
		t.Errorf("mixed: got %v, %v", got, err)
	}

	// Nothing to skip compressing, and compressing an empty tar is free.
	if got, err := allCompressed(t.TempDir()); err != nil || got {
		t.Errorf("empty: got %v, %v", got, err)
	}
}
