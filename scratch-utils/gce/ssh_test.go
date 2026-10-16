// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tarDir and untar are the two halves of PutDir/GetDir: a tree that survives both
// unchanged is the contract.
func TestTarDirUntarRoundTrip(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{
		"top.txt":            "top",
		"sub/nested.txt":     "nested",
		"sub/deep/leaf.txt":  "leaf",
		"sub/deep/empty.txt": "",
	}
	for rel, content := range files {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := tarDir(src, &buf); err != nil {
		t.Fatalf("tarDir: %v", err)
	}
	dst := t.TempDir()
	if err := untar(&buf, dst); err != nil {
		t.Fatalf("untar: %v", err)
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

// GetDir untars whatever the remote sends; ../ entries must not escape destDir.
func TestUntarRejectsPathEscape(t *testing.T) {
	for _, name := range []string{
		"../escaped.txt",
		"../../escaped.txt",
		"sub/../../escaped.txt",
		"/absolute.txt",
	} {
		dst := t.TempDir()
		// Where a successful escape would land.
		outside := filepath.Join(filepath.Dir(dst), "escaped.txt")
		_ = os.Remove(outside)

		err := untar(bytes.NewReader(tarWith(t, name, "pwned")), dst)
		if _, statErr := os.Stat(outside); statErr == nil {
			os.Remove(outside)
			t.Fatalf("%q escaped the destination directory", name)
		}
		// An absolute path is cleaned into destDir rather than rejected; both are
		// safe, so only require that nothing escaped.
		if err != nil && !strings.Contains(err.Error(), "escapes dest") {
			t.Errorf("%q: unexpected error %v", name, err)
		}
	}
}

// A missing remote dir makes GetDir's `cd ... || true` emit nothing -- the normal
// "no artifacts this run" case, not a failure.
func TestUntarEmptyStreamIsNotAnError(t *testing.T) {
	dst := t.TempDir()
	if err := untar(bytes.NewReader(nil), dst); err != nil {
		t.Fatalf("empty stream: %v", err)
	}
}

func TestUntarCreatesDestination(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "does", "not", "exist")
	if err := untar(bytes.NewReader(tarWith(t, "f.txt", "x")), dst); err != nil {
		t.Fatalf("untar: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "f.txt"))
	if err != nil || string(got) != "x" {
		t.Fatalf("f.txt = %q, %v", got, err)
	}
}

// tarWith writes one file at the given (possibly hostile) name; archive/tar emits
// it verbatim, which is the point.
func tarWith(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A symlink under the uploaded tree used to abort the whole upload: the header went
// out as a zero-length TypeSymlink and the body copy then hit "write too long", so
// one link in a vendor or node_modules directory failed the job before it started.
func TestTarDirShipsSymlinksAsLinks(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}
	// A link with no target at all: open would fail, so only the header may be read.
	if err := os.Symlink("nowhere.txt", filepath.Join(src, "dangling.txt")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := tarDir(src, &buf); err != nil {
		t.Fatalf("tarDir with symlinks: %v", err)
	}

	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*tar.Header{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = hdr
	}

	for name, wantTarget := range map[string]string{
		"link.txt":     "real.txt",
		"dangling.txt": "nowhere.txt",
	} {
		hdr, ok := got[name]
		if !ok {
			t.Errorf("%s missing from the archive", name)
			continue
		}
		if hdr.Typeflag != tar.TypeSymlink {
			t.Errorf("%s typeflag = %q, want TypeSymlink", name, hdr.Typeflag)
		}
		if hdr.Linkname != wantTarget {
			t.Errorf("%s linkname = %q, want %q", name, hdr.Linkname, wantTarget)
		}
	}
	if hdr := got["real.txt"]; hdr == nil || hdr.Typeflag != tar.TypeReg {
		t.Errorf("real.txt should still be a regular file, got %+v", hdr)
	}
}

// A symlink already sitting in the destination is not something the lexical check
// could see: "logs/file" is inside destDir as a string, but writing it followed the
// link out of the tree. The root refuses to traverse out, so the file outside stays
// as it was.
func TestUntarWillNotWriteThroughADestinationSymlink(t *testing.T) {
	base := t.TempDir()
	dst := filepath.Join(base, "dest")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{dst, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "logs")); err != nil {
		t.Fatal(err)
	}

	err := untar(bytes.NewReader(tarWith(t, "logs/victim.txt", "PWNED")), dst)
	if err == nil {
		t.Error("expected an error writing through the symlink, got nil")
	}
	got, readErr := os.ReadFile(victim)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "ORIGINAL" {
		t.Fatalf("file outside the destination was overwritten: %q", got)
	}
}

// The old containment check compared against destDir exactly as spelled, so any
// spelling that was not already clean rejected its own contents.
func TestUntarAcceptsEquivalentDestinationSpellings(t *testing.T) {
	for _, dest := range []string{"artifacts", "./artifacts", "nested/./artifacts", "."} {
		base := t.TempDir()
		t.Chdir(base)

		// "." is what `tar czf - -C dir .` puts first; it must not be mistaken for an
		// entry trying to escape.
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: "report.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 2,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte("hi")); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}

		if err := untar(bytes.NewReader(buf.Bytes()), dest); err != nil {
			t.Errorf("dest %q: %v", dest, err)
			continue
		}
		got, err := os.ReadFile(filepath.Join(dest, "report.txt"))
		if err != nil {
			t.Errorf("dest %q: %v", dest, err)
			continue
		}
		if string(got) != "hi" {
			t.Errorf("dest %q: content = %q", dest, got)
		}
	}
}

// putCommand is a shell string run on the VM, so the only real check is to run it.
// bash here stands in for the remote shell; what matters is that the destination
// never exists at a wider mode than asked for, including over an existing 0644 file.
func TestPutCommandNeverWidensTheDestination(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	for _, tc := range []struct {
		name     string
		mode     os.FileMode
		existing os.FileMode // 0 means the destination does not exist yet
	}{
		{"new secret", 0o600, 0},
		{"new script", 0o755, 0},
		{"over an existing world-readable file", 0o600, 0o644},
		{"over an existing private file", 0o600, 0o600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "sub", "payload")
			if tc.existing != 0 {
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, []byte("stale"), tc.existing); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command("bash", "-c", putCommand(dest, tc.mode))
			cmd.Stdin = strings.NewReader("s3cret")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("running putCommand: %v: %s", err, out)
			}

			fi, err := os.Stat(dest)
			if err != nil {
				t.Fatal(err)
			}
			if got := fi.Mode().Perm(); got != tc.mode.Perm() {
				t.Errorf("mode = %04o, want %04o", got, tc.mode.Perm())
			}
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "s3cret" {
				t.Errorf("content = %q", got)
			}
			// The staging file must not be left behind next to the destination.
			entries, err := os.ReadDir(filepath.Dir(dest))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("staging file left behind: %d entries in %s", len(entries), filepath.Dir(dest))
			}
		})
	}
}

// A path with a space or a $(...) in it must reach the shell as data.
func TestPutCommandQuotesHostilePaths(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	for _, base := range []string{"a file", "$(touch pwned)", "semi;colon"} {
		dest := filepath.Join(dir, base)
		cmd := exec.Command("bash", "-c", putCommand(dest, 0o600))
		cmd.Stdin = strings.NewReader("ok")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%q: %v: %s", base, err, out)
		}
		if got, err := os.ReadFile(dest); err != nil || string(got) != "ok" {
			t.Errorf("%q: content=%q err=%v", base, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("command substitution in the path executed")
	}
}
