// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
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
func runLocal(t *testing.T, cmd string, write func(io.Writer) error) {
	t.Helper()
	var in bytes.Buffer
	if err := writeStream(&in, write); err != nil {
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
	runLocal(t, putCommand(dest, 0o640), func(w io.Writer) error {
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
	if err := writeStream(&in, func(w io.Writer) error {
		_, err := w.Write(bytes.Repeat([]byte("x"), 1<<20))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("bash", "-c", putCommand(dest, 0o644))
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
	runLocal(t, putDirCommand(dst), func(w io.Writer) error {
		return tarDir(src, w)
	})
	got, err := os.ReadFile(filepath.Join(dst, "a", "b", "f.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
	// A new directory, not mktemp's private one.
	if info, err := os.Stat(dst); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("destination mode = %v, %v; want 0755", info.Mode().Perm(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dst), ".putdir.*")); len(left) != 0 {
		t.Errorf("staging left behind: %v", left)
	}
}

// Into an existing directory, the archive's files join what is there.
func TestPutDirMergesIntoAnExistingDirectory(t *testing.T) {
	needZstd(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "kept.txt"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	runLocal(t, putDirCommand(dst), func(w io.Writer) error { return tarDir(src, w) })
	for name, want := range map[string]string{"new.txt": "new", "kept.txt": "kept"} {
		if got, err := os.ReadFile(filepath.Join(dst, name)); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
}

// A stream cut short must leave the destination as it was: extracting in place kept
// every entry before the cut, the last one truncated.
func TestPutDirFailedStreamLeavesNothing(t *testing.T) {
	needZstd(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "big.bin"), bytes.Repeat([]byte("calico"), 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	var in bytes.Buffer
	if err := writeStream(&in, func(w io.Writer) error { return tarDir(src, w) }); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	for _, existing := range []bool{false, true} {
		dst := filepath.Join(parent, fmt.Sprintf("out-%v", existing))
		if existing {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		c := exec.Command("bash", "-c", putDirCommand(dst))
		c.Stdin = bytes.NewReader(in.Bytes()[:in.Len()/2])
		if err := c.Run(); err == nil {
			t.Fatalf("existing=%v: a truncated stream was accepted", existing)
		}
		entries, err := os.ReadDir(dst)
		switch {
		case !existing && !os.IsNotExist(err):
			t.Errorf("a new destination exists after a failed put: %v", err)
		case existing && len(entries) != 0:
			t.Errorf("an existing destination gained %d entries from a failed put", len(entries))
		}
	}
	if left, _ := filepath.Glob(filepath.Join(parent, ".putdir.*")); len(left) != 0 {
		t.Errorf("staging left behind: %v", left)
	}
}

// A stream that breaks mid-file leaves neither a truncated file under its name nor a
// clobbered earlier copy, and nothing half-written beside it.
func TestUntarKeepsAFileWholeOrAbsent(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	body := bytes.Repeat([]byte("x"), 4096)
	if err := tw.WriteHeader(&tar.Header{Name: "report.xml", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "report.xml"), []byte("earlier"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := untar(bytes.NewReader(archive.Bytes()[:1024]), dst); err == nil {
		t.Fatal("a stream cut mid-file was accepted")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "report.xml")); err != nil || string(got) != "earlier" {
		t.Errorf("report.xml = %q, %v; want the earlier copy untouched", got, err)
	}
	if entries, _ := os.ReadDir(dst); len(entries) != 1 {
		t.Errorf("%d entries after a failed untar, want just the earlier file", len(entries))
	}
}

// zstdIn is s as the zstd stream a put reads.
func zstdIn(t *testing.T, s string) io.Reader {
	t.Helper()
	// Only the remote side, the zstd CLI, reads it.
	needZstd(t)
	var in bytes.Buffer
	if err := writeStream(&in, func(w io.Writer) error {
		_, err := io.WriteString(w, s)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return &in
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
