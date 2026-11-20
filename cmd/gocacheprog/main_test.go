// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// put is a request with its body, written the way the go command writes it: the
// JSON line, a blank line, then the body as a quoted base64 line.
type put struct {
	req  request
	body []byte
}

func write(t *testing.T, w io.Writer, msgs ...any) {
	t.Helper()
	enc := json.NewEncoder(w)
	for _, m := range msgs {
		switch m := m.(type) {
		case request:
			if err := enc.Encode(m); err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(w, "\n")
		case put:
			if err := enc.Encode(m.req); err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(w, "\n")
			if m.req.BodySize > 0 {
				fmt.Fprintf(w, "\"%s\"\n", base64.StdEncoding.EncodeToString(m.body))
			}
		}
	}
}

// putOf is a well-formed put of body under the action ID derived from seed.
func putOf(id int64, seed string, body []byte) put {
	action := sha256.Sum256([]byte(seed))
	out := sha256.Sum256(body)
	return put{req: request{ID: id, Command: "put", ActionID: action[:], OutputID: out[:], BodySize: int64(len(body))}, body: body}
}

func getOf(id int64, seed string) request {
	action := sha256.Sum256([]byte(seed))
	return request{ID: id, Command: "get", ActionID: action[:]}
}

// session runs serve over msgs and returns its responses after the capabilities.
func session(t *testing.T, c *cache, msgs ...any) []response {
	t.Helper()
	var in bytes.Buffer
	write(t, &in, msgs...)
	var out bytes.Buffer
	if err := c.serve(&in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	dec := json.NewDecoder(&out)
	var hello response
	if err := dec.Decode(&hello); err != nil || len(hello.KnownCommands) != 3 {
		t.Fatalf("first message = %+v, %v; want the capabilities", hello, err)
	}
	var got []response
	for {
		var r response
		if err := dec.Decode(&r); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	// Answers come in any order, as the protocol allows; tests read them by ID.
	slices.SortFunc(got, func(a, b response) int { return int(a.ID - b.ID) })
	return got
}

func TestPutThenGet(t *testing.T) {
	c := &cache{local: t.TempDir()}
	body := []byte("compiled")
	p := putOf(2, "a", body)
	if before := session(t, c, getOf(1, "a")); !before[0].Miss {
		t.Errorf("get before put = %+v, want a miss", before[0])
	}
	got := session(t, c, p, getOf(3, "a"), request{ID: 4, Command: "close"})
	if len(got) != 3 {
		t.Fatalf("responses = %+v, want 3", got)
	}
	if got[0].Err != "" || got[0].DiskPath == "" {
		t.Errorf("put = %+v, want a disk path", got[0])
	}
	hit := got[1]
	if hit.Miss || !bytes.Equal(hit.OutputID, p.req.OutputID) || hit.Size != int64(len(body)) || hit.Time == nil {
		t.Fatalf("get after put = %+v", hit)
	}
	if data, err := os.ReadFile(hit.DiskPath); err != nil || string(data) != "compiled" {
		t.Errorf("body at %s = %q, %v", hit.DiskPath, data, err)
	}
	if got[2].ID != 4 || got[2].Err != "" {
		t.Errorf("close = %+v", got[2])
	}
}

// The go command caches empty outputs too, and sends no body line for them.
func TestPutOfAnEmptyOutput(t *testing.T) {
	c := &cache{local: t.TempDir()}
	got := session(t, c, putOf(1, "empty", nil), getOf(2, "empty"))
	if got[0].Err != "" || got[1].Miss || got[1].Size != 0 {
		t.Fatalf("responses = %+v, want an empty entry served", got)
	}
}

// The read-only cache is read in place: a hit points into it, and nothing is
// written there or locally.
func TestGetFromTheReadOnlyCache(t *testing.T) {
	ro := t.TempDir()
	session(t, &cache{local: ro}, putOf(1, "shared", []byte("abc")))

	c := &cache{readOnly: ro, local: t.TempDir()}
	got := session(t, c, getOf(1, "shared"), getOf(2, "unknown"))
	if got[0].Miss || !strings.HasPrefix(got[0].DiskPath, ro+string(filepath.Separator)) {
		t.Errorf("get = %+v, want a hit in %s", got[0], ro)
	}
	if !got[1].Miss {
		t.Errorf("get of an unknown action = %+v, want a miss", got[1])
	}
	if c.readOnlyHits.Load() != 1 || c.misses.Load() != 1 {
		t.Errorf("counted %d read-only hits and %d misses, want 1 and 1", c.readOnlyHits.Load(), c.misses.Load())
	}
	if entries, _ := os.ReadDir(c.local); len(entries) != 0 {
		t.Errorf("local cache has %d entries after only gets", len(entries))
	}
}

// What the build itself puts wins over the shared cache.
func TestLocalPutShadowsTheReadOnlyCache(t *testing.T) {
	ro := t.TempDir()
	session(t, &cache{local: ro}, putOf(1, "x", []byte("shared")))
	c := &cache{readOnly: ro, local: t.TempDir()}
	got := session(t, c, putOf(1, "x", []byte("mine")), getOf(2, "x"))
	if data, _ := os.ReadFile(got[1].DiskPath); string(data) != "mine" {
		t.Errorf("served %q, want the local put", data)
	}
}

// An entry whose output is gone, as after a trim, must not be served.
func TestEntryWithoutItsOutputIsAMiss(t *testing.T) {
	dir := t.TempDir()
	p := putOf(1, "trimmed", []byte("x"))
	session(t, &cache{local: dir}, p)
	out := filepath.Join(dir, fmt.Sprintf("%02x", p.req.OutputID[0]), fmt.Sprintf("%x-d", p.req.OutputID))
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	if _, ok := lookup(dir, p.req.ActionID); ok {
		t.Error("served an entry whose output was removed")
	}
}

// A body that does not match its output ID is refused, nothing is stored, and the
// stream stays in step for the next request.
func TestCorruptBodyIsRefused(t *testing.T) {
	c := &cache{local: t.TempDir()}
	bad := putOf(1, "bad", []byte("right"))
	bad.body = []byte("wrong")
	short := putOf(2, "short", []byte("twelve bytes"))
	short.req.BodySize = 20
	got := session(t, c, bad, short, getOf(3, "bad"), getOf(4, "short"), putOf(5, "good", []byte("ok")))
	if len(got) != 5 {
		t.Fatalf("responses = %+v, want 5: the stream lost its place", got)
	}
	for i, want := range []string{"hashes to", "bytes, want 20"} {
		if !strings.Contains(got[i].Err, want) {
			t.Errorf("put %d error = %q, want it to say %q", i+1, got[i].Err, want)
		}
	}
	if !got[2].Miss || !got[3].Miss {
		t.Errorf("gets after refused puts = %+v, %+v; want misses", got[2], got[3])
	}
	if got[4].Err != "" {
		t.Errorf("put after the refused ones = %+v", got[4])
	}
}

func TestMalformedIDsAreErrors(t *testing.T) {
	c := &cache{local: t.TempDir()}
	got := session(t, c,
		request{ID: 1, Command: "get", ActionID: []byte{1}},
		put{req: request{ID: 2, Command: "put", ActionID: []byte{1}, OutputID: []byte{2}, BodySize: 1}, body: []byte("x")},
		request{ID: 3, Command: "nope"},
		getOf(4, "still in step"))
	for i := 0; i < 3; i++ {
		if got[i].Err == "" {
			t.Errorf("response %d = %+v, want an error", i+1, got[i])
		}
	}
	if !got[3].Miss {
		t.Errorf("get after the errors = %+v, want a plain miss", got[3])
	}
}

// Bodies are streamed, so one larger than the reader's buffer many times over
// round-trips intact.
func TestLargeBodyRoundTrips(t *testing.T) {
	c := &cache{local: t.TempDir()}
	body := bytes.Repeat([]byte("0123456789abcdef"), 1<<20)
	got := session(t, c, putOf(1, "big", body), getOf(2, "big"))
	data, err := os.ReadFile(got[1].DiskPath)
	if err != nil || !bytes.Equal(data, body) {
		t.Errorf("stored %d bytes, %v; want the %d put", len(data), err, len(body))
	}
}

// The go command itself: a build whose cache was filled by a plain GOCACHE build
// takes its packages from that cache, read in place.
func TestGoBuildReadsACacheFilledByAPlainBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with the go command")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	// The caller's own cache settings would turn the plain builds below into
	// helper-backed ones; every go command here sets its own.
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOCACHEPROG=") && !strings.HasPrefix(kv, "GOCACHE=") {
			env = append(env, kv)
		}
	}
	work := t.TempDir()
	helper := filepath.Join(work, "gocacheprog")
	// A test cannot mount its shared directory read-only.
	helperBuild := exec.Command(goBin, "build", "-buildvcs=false", "-ldflags", "-X main.allowWritableReadOnly=true", "-o", helper, ".")
	helperBuild.Env = append(env, "GOCACHE="+filepath.Join(work, "helper-cache"))
	if out, err := helperBuild.CombinedOutput(); err != nil {
		t.Fatalf("building the helper: %v\n%s", err, out)
	}
	mod := filepath.Join(work, "mod")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"go.mod":  "module example.com/m\n\ngo 1.24\n",
		"main.go": "package main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n)\n\nfunc main() { b, _ := json.Marshal(1); fmt.Println(string(b)) }\n",
	} {
		if err := os.WriteFile(filepath.Join(mod, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A fresh output each time: an up-to-date one makes go skip the build, and so
	// skip caching the main package.
	builds := 0
	build := func(extra ...string) string {
		t.Helper()
		builds++
		cmd := exec.Command(goBin, "build", "-o", filepath.Join(work, fmt.Sprint("bin", builds)), ".")
		cmd.Dir = mod
		cmd.Env = append(slices.Clone(env), extra...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("go build %v: %v\n%s", extra, err, stderr.String())
		}
		return stderr.String()
	}

	shared := filepath.Join(work, "shared")
	build("GOCACHE=" + shared)
	goCache := filepath.Join(work, "gocache")
	// Relative flags, as a user might give them, from the module directory.
	rel, err := filepath.Rel(mod, shared)
	if err != nil {
		t.Fatal(err)
	}
	prog := fmt.Sprintf("GOCACHEPROG=%s -v -ro %s", helper, rel)
	stderr := build("GOCACHE="+goCache, prog)
	m := regexp.MustCompile(`gocacheprog: (\d+) hits from .*, (\d+) local hits, (\d+) misses`).FindStringSubmatch(stderr)
	if m == nil {
		t.Fatalf("no summary from the helper in:\n%s", stderr)
	}
	if hits, _ := strconv.Atoi(m[1]); hits == 0 {
		t.Errorf("no hits from the shared cache:\n%s", stderr)
	}
	if out, err := exec.Command(filepath.Join(work, fmt.Sprint("bin", builds))).Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Errorf("built binary printed %q, %v; want 1", out, err)
	}

	// vet and test run their tools from other directories than compile does.
	for _, args := range [][]string{{"vet", "."}, {"test", "-count=1", "."}} {
		cmd := exec.Command(goBin, args...)
		cmd.Dir = mod
		cmd.Env = append(slices.Clone(env), "GOCACHE="+goCache, prog)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("go %v through the helper: %v\n%s", args, err, out)
		}
	}

	// What the helper writes is in the go command's own layout: a cache filled by
	// the helper alone serves a plain GOCACHE build without a compile.
	filled := filepath.Join(work, "filled")
	build("GOCACHE="+filepath.Join(work, "gocache2"), fmt.Sprintf("GOCACHEPROG=%s -dir %s", helper, filled))
	plain := exec.Command(goBin, "build", "-x", "-o", filepath.Join(work, "plain"), ".")
	plain.Dir = mod
	plain.Env = append(slices.Clone(env), "GOCACHE="+filled)
	out, err := plain.CombinedOutput()
	if err != nil {
		t.Fatalf("plain build over the helper's directory: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "/compile ") {
		t.Errorf("a plain build over the helper's directory recompiled:\n%s", out)
	}
}

// A stream that ends inside a body stores nothing and leaves no temporary file.
func TestTruncatedBodyStoresNothing(t *testing.T) {
	dir := t.TempDir()
	c := &cache{local: dir}
	p := putOf(1, "cut", []byte("a body that never finishes"))
	var in bytes.Buffer
	write(t, &in, p)
	cut := in.Bytes()[:in.Len()-10]
	if err := c.serve(bytes.NewReader(cut), io.Discard); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if _, ok := lookup(dir, p.req.ActionID); ok {
		t.Error("stored a truncated body")
	}
	assertNoTempFiles(t, dir)
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), ".tmp-") {
			t.Errorf("temporary file left behind: %s", path)
		}
		return nil
	})
}

// Entries are readable by a build running as another user, as the go command's
// own are.
func TestStoredFilesAreWorldReadable(t *testing.T) {
	dir := t.TempDir()
	got := session(t, &cache{local: dir}, putOf(1, "mode", []byte("x")))
	info, err := os.Stat(got[0].DiskPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o044 != 0o044 {
		t.Errorf("output mode = %v, want it readable by group and others", mode)
	}
}

// Several builds share one local directory; each has its own helper process.
func TestConcurrentHelpersShareADirectory(t *testing.T) {
	dir := t.TempDir()
	done := make(chan []response)
	for i := 0; i < 4; i++ {
		go func() {
			var msgs []any
			for j := 0; j < 50; j++ {
				seed := fmt.Sprint("shared-", j)
				msgs = append(msgs, putOf(int64(2*j+1), seed, []byte(seed)), getOf(int64(2*j+2), seed))
			}
			var in, out bytes.Buffer
			write(t, &in, msgs...)
			c := &cache{local: dir}
			if err := c.serve(&in, &out); err != nil {
				t.Error(err)
			}
			dec := json.NewDecoder(&out)
			var got []response
			for {
				var r response
				if dec.Decode(&r) != nil {
					break
				}
				got = append(got, r)
			}
			done <- got
		}()
	}
	for i := 0; i < 4; i++ {
		for _, r := range <-done {
			if r.Err != "" || (r.ID%2 == 0 && r.Miss) {
				t.Errorf("response %+v, want every put stored and every get a hit", r)
			}
		}
	}
	assertNoTempFiles(t, dir)
}

// A missing read-only directory is only ever misses.
func TestMissingReadOnlyDirectoryMisses(t *testing.T) {
	c := &cache{readOnly: filepath.Join(t.TempDir(), "absent"), local: t.TempDir()}
	if got := session(t, c, getOf(1, "x")); !got[0].Miss || got[0].Err != "" {
		t.Errorf("get = %+v, want a plain miss", got[0])
	}
}

// The go command hands the paths this returns to tools that run in other
// directories, so relative flags must come back absolute.
func TestNewCacheMakesPathsAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	c, err := newCache("ro", "local")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(c.local) || !filepath.IsAbs(c.readOnly) {
		t.Errorf("paths = %q, %q; want both absolute", c.readOnly, c.local)
	}
}

// The go command's own disk cache keeps an executable as a directory holding just
// that file, which a build filling the read-only cache leaves for go run and go
// tool; it is served as the file inside.
func TestExecutableStoredAsADirectoryIsServed(t *testing.T) {
	dir := t.TempDir()
	p := putOf(1, "exe", []byte("binary"))
	session(t, &cache{local: dir}, p)
	out := filepath.Join(dir, fmt.Sprintf("%02x", p.req.OutputID[0]), fmt.Sprintf("%x-d", p.req.OutputID))
	if err := os.Rename(out, out+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(out, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(out+".tmp", filepath.Join(out, "tool")); err != nil {
		t.Fatal(err)
	}
	e, ok := lookup(dir, p.req.ActionID)
	if !ok || e.path != filepath.Join(out, "tool") {
		t.Fatalf("lookup = %+v, %v; want the file inside %s", e, ok, out)
	}
	// One holding anything but that single file is not an output.
	if err := os.WriteFile(filepath.Join(out, "extra"), nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, ok := lookup(dir, p.req.ActionID); ok {
		t.Error("served a directory holding more than one file")
	}
}

// A hit is a path into the read-only directory that the go command may rename on
// install, so a writable one is refused rather than drained.
func TestWritableReadOnlyDirectoryIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := requireReadOnly(dir); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Errorf("requireReadOnly(%s) = %v, want a refusal", dir, err)
	}
	if err := requireReadOnly(""); err != nil {
		t.Errorf("requireReadOnly with no -ro = %v, want nil", err)
	}
}
