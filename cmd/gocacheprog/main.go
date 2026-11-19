// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// gocacheprog is a GOCACHEPROG helper that reads a Go build cache in place from
// a read-only directory, such as a disk CI shares between pods, and keeps what
// the build adds in a writable local directory. Both are in the go command's own
// on-disk layout, so the read-only cache is filled by an ordinary build with
// GOCACHE pointed at it: nothing is packed, unpacked or copied.
//
//	GOCACHEPROG="gocacheprog -ro /cache/go/build"
//
// -dir defaults to a directory under GOCACHE, and should stay under it: the go
// command moves, rather than copies, a cached file it installs from anywhere
// else, which would empty the local cache. The read-only directory is safe only
// mounted read-only, for the same reason, and only while nothing writes to it.
// -v prints one line of hit and miss counts when the go command closes it.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// hashSize is the length of an action or output ID: a SHA-256.
const hashSize = sha256.Size

// request and response mirror cmd/go/internal/cacheprog, which is internal to
// the go command and so cannot be imported.
type request struct {
	ID       int64
	Command  string
	ActionID []byte `json:",omitempty"`
	OutputID []byte `json:",omitempty"`
	BodySize int64  `json:",omitempty"`
}

type response struct {
	ID            int64      `json:"ID"`
	Err           string     `json:",omitempty"`
	KnownCommands []string   `json:",omitempty"`
	Miss          bool       `json:",omitempty"`
	OutputID      []byte     `json:",omitempty"`
	Size          int64      `json:",omitempty"`
	Time          *time.Time `json:",omitempty"`
	DiskPath      string     `json:",omitempty"`
}

func main() {
	readOnly := flag.String("ro", "", "a Go build cache directory to read and never write")
	local := flag.String("dir", "", "the writable Go build cache directory for what the build adds (default $GOCACHE/gocacheprog)")
	verbose := flag.Bool("v", false, "print hit and miss counts on close")
	flag.Parse()
	log.SetPrefix("gocacheprog: ")
	log.SetFlags(0)
	if *local == "" {
		goCache, err := defaultGoCache()
		if err != nil {
			log.Fatalf("-dir is unset and GOCACHE cannot be found: %v", err)
		}
		*local = filepath.Join(goCache, "gocacheprog")
	}
	c, err := newCache(*readOnly, *local)
	if err != nil {
		log.Fatal(err)
	}
	if err := c.serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
	if *verbose {
		log.Printf("%d hits from %s, %d local hits, %d misses, %d puts",
			c.readOnlyHits.Load(), c.readOnly, c.localHits.Load(), c.misses.Load(), c.puts.Load())
	}
}

// defaultGoCache is where the go command keeps its own build cache, which it
// passes to this program in GOCACHE when the user set one.
func defaultGoCache() (string, error) {
	if dir := os.Getenv("GOCACHE"); dir != "" && dir != "off" {
		return dir, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "go-build"), nil
}

type cache struct {
	readOnly, local string

	readOnlyHits, localHits, misses, puts atomic.Int64
}

// newCache makes both directories absolute: the go command hands the paths this
// program returns to tools that run in other directories.
func newCache(readOnly, local string) (*cache, error) {
	c := &cache{}
	var err error
	if c.local, err = filepath.Abs(local); err != nil {
		return nil, err
	}
	if readOnly != "" {
		if c.readOnly, err = filepath.Abs(readOnly); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(c.local, 0o777); err != nil {
		return nil, err
	}
	return c, nil
}

// serve answers requests until "close" or the end of input. The go command has
// many requests outstanding at once and takes answers in any order, so gets are
// answered concurrently rather than queued behind a put's write. A put's body
// follows it on the input, so puts are read, and written, in order.
func (c *cache) serve(in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	enc := json.NewEncoder(w)
	var sendMu sync.Mutex
	var sendErr error
	send := func(res response) {
		sendMu.Lock()
		defer sendMu.Unlock()
		if sendErr != nil {
			return
		}
		if err := enc.Encode(res); err != nil {
			sendErr = err
			return
		}
		sendErr = w.Flush()
	}
	sent := func() error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return sendErr
	}
	var gets sync.WaitGroup
	defer gets.Wait()

	send(response{KnownCommands: []string{"get", "put", "close"}})
	for {
		if err := sent(); err != nil {
			return err
		}
		line, err := nextLine(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading request: %w", err)
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			return fmt.Errorf("decoding request %q: %w", line, err)
		}
		res := response{ID: req.ID}
		switch req.Command {
		case "get":
			gets.Add(1)
			go func() {
				defer gets.Done()
				c.get(req, &res)
				send(res)
			}()
			continue
		case "put":
			// The body has to be consumed even when the put is refused, or the next
			// request would be read from the middle of it.
			path, err := c.put(r, req)
			if err != nil {
				res.Err = err.Error()
			}
			res.DiskPath = path
		case "close":
			// Every outstanding answer goes out before the reply to close.
			gets.Wait()
			send(res)
			return sent()
		default:
			res.Err = fmt.Sprintf("unknown command %q", req.Command)
		}
		send(res)
	}
}

// nextLine returns the next non-blank line: the go command ends each request with
// a newline of its own after the encoder's.
func nextLine(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			return trimmed, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// get answers from the local cache first, which holds this build's own puts,
// then the read-only one. A miss is not an error: the go command rebuilds.
func (c *cache) get(req request, res *response) {
	if len(req.ActionID) != hashSize {
		res.Err = fmt.Sprintf("action ID is %d bytes, want %d", len(req.ActionID), hashSize)
		return
	}
	if e, ok := lookup(c.local, req.ActionID); ok {
		c.localHits.Add(1)
		e.fill(res)
		return
	}
	if c.readOnly != "" {
		if e, ok := lookup(c.readOnly, req.ActionID); ok {
			c.readOnlyHits.Add(1)
			e.fill(res)
			return
		}
	}
	c.misses.Add(1)
	res.Miss = true
}

// put streams the request's body, a base64 JSON string on the line after it,
// into the local cache and returns where it landed. Streamed, not decoded whole:
// a linked binary can be hundreds of megabytes.
func (c *cache) put(r *bufio.Reader, req request) (string, error) {
	var body io.Reader = bytes.NewReader(nil)
	if req.BodySize > 0 {
		quoted, err := openQuoted(r)
		if err != nil {
			return "", fmt.Errorf("reading the body: %w", err)
		}
		defer quoted.drain()
		body = base64.NewDecoder(base64.StdEncoding, quoted)
	}
	if len(req.ActionID) != hashSize || len(req.OutputID) != hashSize {
		return "", fmt.Errorf("action and output IDs are %d and %d bytes, want %d", len(req.ActionID), len(req.OutputID), hashSize)
	}
	c.puts.Add(1)
	out := filepath.Join(c.local, shard(req.OutputID), hex.EncodeToString(req.OutputID)+"-d")
	// The output ID is the body's SHA-256, so a short or corrupt stream cannot
	// become an entry that a later build trusts.
	if err := writeVerified(out, body, req.BodySize, req.OutputID); err != nil {
		return "", err
	}
	entry := fmt.Sprintf("v1 %x %x %20d %20d\n", req.ActionID, req.OutputID, req.BodySize, time.Now().UnixNano())
	if err := writeAtomic(filepath.Join(c.local, shard(req.ActionID), hex.EncodeToString(req.ActionID)+"-a"), strings.NewReader(entry)); err != nil {
		return "", err
	}
	return out, nil
}

// quotedReader reads a JSON string's contents up to its closing quote. A base64
// body has no escapes, so the bytes between the quotes are the base64 itself.
type quotedReader struct {
	r    *bufio.Reader
	done bool
}

func openQuoted(r *bufio.Reader) (*quotedReader, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		switch b {
		case '"':
			return &quotedReader{r: r}, nil
		case ' ', '\t', '\r', '\n':
		default:
			return nil, fmt.Errorf("body starts with %q, want a JSON string", b)
		}
	}
}

func (q *quotedReader) Read(p []byte) (int, error) {
	if q.done {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	if q.r.Buffered() == 0 {
		if _, err := q.r.Peek(1); err != nil {
			return 0, err
		}
	}
	buf, _ := q.r.Peek(min(len(p), q.r.Buffered()))
	if i := bytes.IndexByte(buf, '"'); i >= 0 {
		n := copy(p, buf[:i])
		_, _ = q.r.Discard(i + 1)
		q.done = true
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
	n := copy(p, buf)
	_, _ = q.r.Discard(n)
	return n, nil
}

// drain consumes the rest of the string, so a body abandoned part way leaves the
// reader at the next request.
func (q *quotedReader) drain() {
	_, _ = io.Copy(io.Discard, q)
}

// entry is an action entry in the go command's on-disk layout: dir/<first byte
// in hex>/<action ID>-a holds "v1 <action> <output> <size> <unixnano>", with the
// output itself in dir/<first byte>/<output ID>-d.
type entry struct {
	outputID []byte
	size     int64
	time     time.Time
	path     string
}

func (e entry) fill(res *response) {
	res.OutputID = e.outputID
	res.Size = e.size
	res.Time = &e.time
	res.DiskPath = e.path
}

func lookup(dir string, actionID []byte) (entry, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, shard(actionID), hex.EncodeToString(actionID)+"-a"))
	if err != nil {
		return entry{}, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 5 || fields[0] != "v1" || fields[1] != hex.EncodeToString(actionID) {
		return entry{}, false
	}
	outputID, err := hex.DecodeString(fields[2])
	if err != nil || len(outputID) != hashSize {
		return entry{}, false
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || size < 0 {
		return entry{}, false
	}
	nanos, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil || nanos < 0 {
		return entry{}, false
	}
	path := filepath.Join(dir, shard(outputID), fields[2]+"-d")
	// An entry whose output was trimmed, or is not the size it claims, is a miss.
	if info, err := os.Stat(path); err != nil || info.Size() != size {
		return entry{}, false
	}
	return entry{outputID: outputID, size: size, time: time.Unix(0, nanos), path: path}, true
}

func shard(id []byte) string {
	return fmt.Sprintf("%02x", id[0])
}

// writeVerified writes body to path only if it is size bytes with the given
// SHA-256.
func writeVerified(path string, body io.Reader, size int64, sum []byte) error {
	h := sha256.New()
	counted := &countingReader{r: io.TeeReader(body, h)}
	return writeAtomicCheck(path, counted, func() error {
		if counted.n != size {
			return fmt.Errorf("body is %d bytes, want %d", counted.n, size)
		}
		if got := h.Sum(nil); !bytes.Equal(got, sum) {
			return fmt.Errorf("body hashes to %x, want %x", got, sum)
		}
		return nil
	})
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func writeAtomic(path string, data io.Reader) error {
	return writeAtomicCheck(path, data, func() error { return nil })
}

// writeAtomicCheck writes through a temporary file that is renamed into place
// only if check passes, so neither a concurrent build reading the same local
// cache nor a later one ever sees half a file.
func writeAtomicCheck(path string, data io.Reader, check func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, data)
	// CreateTemp's 0600 would hide the entry from a build running as anyone else.
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = check()
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
