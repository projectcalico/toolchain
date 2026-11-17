// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// SSH access to a GCE VM without gcloud: an ephemeral keypair goes in as the
// instance's `ssh-keys` metadata (the guest agent installs it), the external IP
// comes from the instance, and the connection is a plain x/crypto/ssh dial. This
// is what lets the run step drive the VM from the same distroless image.
package gce

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/ssh"
	compute "google.golang.org/api/compute/v1"

	"github.com/projectcalico/go-build/scratch-utils/util"
)

// SSH is a live connection to a VM. Close it when done.
type SSH struct {
	client *ssh.Client
}

// DialSSH injects an ephemeral keypair, reads the external IP and dials, retrying
// until reachable -- a fresh VM accepts SSH only once sshd, the guest agent and
// the key have caught up. This retry is the readiness check createvm skips. The
// host key is not verified: we just made the VM, it lives for minutes, and there
// is no prior key to pin.
func (c *Client) DialSSH(ctx context.Context, zone, name, user string) (*SSH, error) {
	signer, authorized, err := ephemeralKey()
	if err != nil {
		return nil, err
	}
	ip, err := c.injectKeyAndGetIP(ctx, zone, name, user, authorized)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // ephemeral CI VM, no key to pin
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(ip, "22")

	deadline := time.Now().Add(3 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		client, err := dial(addr, cfg)
		if err == nil {
			go keepalive(client)
			return &SSH{client: client}, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return nil, fmt.Errorf("ssh to %s (%s) not ready after 3m: %w", name, addr, lastErr)
}

// dial bounds the handshake, not just the connect. ssh.Dial leaves NewClientConn
// with no deadline, so a VM whose sshd has bound the socket but is not answering
// stalls forever -- the exact state the retry loop above exists for, and it would
// never get back there.
func dial(addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := net.DialTimeout("tcp", addr, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(cfg.Timeout)); err != nil {
		conn.Close()
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// Clear it again: the deadline covered the handshake, and the session that
	// follows carries a CI job of unbounded length.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		c.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// keepalive stops GCP reaping the connection: a VPC drops an idle established flow
// after 10 minutes, untunable, and x/crypto/ssh sends nothing itself. A quiet
// stretch in the job would otherwise kill the session and the artifact pull with it.
func keepalive(client *ssh.Client) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for range t.C {
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			return // connection is gone; Run/GetDir will report it
		}
	}
}

// injectKeyAndGetIP sets the instance's ssh-keys metadata to authorize user with
// the given key (preserving any other metadata) and returns its external IP.
func (c *Client) injectKeyAndGetIP(ctx context.Context, zone, name, user, authorized string) (string, error) {
	var inst *compute.Instance
	if err := retry(ctx, "get instance "+name, func() (err error) {
		inst, err = c.svc.Instances.Get(c.project, zone, name).Context(ctx).Do()
		return err
	}); err != nil {
		return "", err
	}

	md := inst.Metadata
	if md == nil {
		md = &compute.Metadata{}
	}
	sshKeys := fmt.Sprintf("%s:%s", user, strings.TrimSpace(authorized))
	replaced := false
	for _, it := range md.Items {
		if it.Key == "ssh-keys" {
			it.Value = new(sshKeys)
			replaced = true
			break
		}
	}
	if !replaced {
		md.Items = append(md.Items, &compute.MetadataItems{Key: "ssh-keys", Value: new(sshKeys)})
	}
	var op *compute.Operation
	if err := retry(ctx, "set ssh-keys metadata on "+name, func() (err error) {
		op, err = c.svc.Instances.SetMetadata(c.project, zone, name, md).Context(ctx).Do()
		return err
	}); err != nil {
		return "", err
	}
	if err := c.waitZoneOp(ctx, zone, op.Name); err != nil {
		return "", fmt.Errorf("set-metadata op on %s: %w", name, err)
	}

	// Re-read rather than reuse the snapshot from before the metadata op: createvm
	// does not wait for readiness, so natIP can still be unset when the insert
	// reaches DONE, and it appears seconds later. Retried for the same reason.
	var ip string
	if err := retry(ctx, "external IP of "+name, func() error {
		fresh, err := c.svc.Instances.Get(c.project, zone, name).Context(ctx).Do()
		if err != nil {
			return err
		}
		if ip = externalIP(fresh); ip == "" {
			return fmt.Errorf("external IP of %s: %w", name, errNotReady)
		}
		return nil
	}); err != nil {
		return "", err
	}
	return ip, nil
}

func externalIP(inst *compute.Instance) string {
	for _, ni := range inst.NetworkInterfaces {
		for _, ac := range ni.AccessConfigs {
			if ac.NatIP != "" {
				return ac.NatIP
			}
		}
	}
	return ""
}

// ephemeralKey returns an ssh signer and its authorized_keys line.
func ephemeralKey() (ssh.Signer, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, "", fmt.Errorf("signer: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", fmt.Errorf("public key: %w", err)
	}
	return signer, string(ssh.MarshalAuthorizedKey(sshPub)), nil
}

// Close closes the underlying connection.
func (s *SSH) Close() error { return s.client.Close() }

// Run executes cmd on the VM, streaming output to the given writers. A non-zero
// exit comes back as exitCode with a nil error so the caller can propagate it;
// err is non-nil only for a connection or protocol failure.
func (s *SSH) Run(cmd string, stdout, stderr io.Writer) (exitCode int, err error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return -1, err
	}
	defer sess.Close()
	sess.Stdout = stdout
	sess.Stderr = stderr
	if err := sess.Run(cmd); err != nil {
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			return ee.ExitStatus(), nil
		}
		return -1, err
	}
	return 0, nil
}

// PutData writes data to remote at the given mode, creating parent dirs. Sent as
// is: it carries secrets and env files, too small for compression to pay.
func (s *SSH) PutData(data []byte, remote string, mode os.FileMode) error {
	err := s.runWithInput(putCommand(remote, mode, false), true, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
	if err != nil {
		return fmt.Errorf("put %s: %w", remote, err)
	}
	return nil
}

// putCommand writes stdin to remote at mode, staged through a 0600 mktemp file in the
// same directory. Writing remote directly creates it at the shell's umask and narrows
// it only afterwards, exposing a secret for the length of the upload. With compressed
// set, stdin is a zstd stream that lands decompressed.
//
// -T is load-bearing: plain `mv -f tmp dir` moves the payload *into* an existing
// directory and exits 0. It also replaces a destination symlink rather than writing
// through it. GNU coreutils, which is what the VM runs.
func putCommand(remote string, mode os.FileMode, compressed bool) string {
	// Single-quoted, not %q: %q is double-quoted, and bash expands $(...) inside
	// double quotes, so a crafted remote path would execute on the VM.
	q := util.ShellQuote(remote)
	qd := util.ShellQuote(filepath.Dir(remote))
	write := "cat"
	if compressed {
		write = "zstd -dcq"
	}
	return fmt.Sprintf(
		`mkdir -p %s && t="$(mktemp %s/.put.XXXXXX)" && trap 'rm -f "$t"' EXIT && `+
			`%s > "$t" && chmod %o "$t" && mv -fT -- "$t" %s && trap - EXIT`,
		qd, qd, write, mode.Perm(), q)
}

// PutFile streams a single local file to remote at the given mode, zstd-compressed
// on the wire unless it is compressed already.
func (s *SSH) PutFile(local, remote string, mode os.FileMode) error {
	raw, err := isCompressed(local)
	if err != nil {
		return err
	}
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	err = s.runWithInput(putCommand(remote, mode, !raw), raw, func(w io.Writer) error {
		_, err := io.Copy(w, f)
		return err
	})
	if err != nil {
		return fmt.Errorf("put %s: %w", remote, err)
	}
	return nil
}

// PutDir uploads a local directory tree to a remote directory (created if absent)
// by streaming a tar over the connection and untarring on the VM. The tar is
// zstd-compressed unless every file in it is compressed already.
func (s *SSH) PutDir(localDir, remoteDir string) error {
	raw, err := allCompressed(localDir)
	if err != nil {
		return err
	}
	q := util.ShellQuote(remoteDir)
	// tar runs the decompressor itself, so a corrupt stream fails the command
	// rather than vanishing into a pipeline's exit status.
	untar := "tar --zstd -xf -"
	if raw {
		untar = "tar -xf -"
	}
	cmd := fmt.Sprintf("mkdir -p %s && %s -C %s", q, untar, q)
	if err := s.runWithInput(cmd, raw, func(w io.Writer) error { return tarDir(localDir, w) }); err != nil {
		return fmt.Errorf("put dir %s: %w", remoteDir, err)
	}
	return nil
}

// runWithInput runs cmd on the VM with what write produces as its stdin,
// zstd-compressed unless raw.
func (s *SSH) runWithInput(cmd string, raw bool, write func(io.Writer) error) error {
	sess, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	pr, pw := io.Pipe()
	sess.Stdin = pr
	// Unblocks the writer if the session dies before draining the pipe.
	defer pr.Close()
	// Buffered so the goroutine finishes even on the early return below.
	writeErr := make(chan error, 1)
	go func() {
		err := writeStream(pw, raw, write)
		// Recorded before the close that ends the remote's input, so it is there
		// by the time Run returns.
		writeErr <- err
		pw.CloseWithError(err)
	}()
	// Captured so a failing remote command says why.
	var errBuf bytes.Buffer
	sess.Stderr = &errBuf
	if err := sess.Run(cmd); err != nil {
		remote := fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
		// A local failure cuts the stream short, so the remote end then fails too,
		// on input that says nothing of why. Report the local one when there is one.
		select {
		case werr := <-writeErr:
			if werr != nil {
				return fmt.Errorf("%w (remote: %v)", werr, remote)
			}
		default:
		}
		return remote
	}
	// Run returned, so the input is done. An error here means we shipped a
	// truncated stream the remote accepted without complaint.
	return <-writeErr
}

// writeStream runs write against w, through a zstd encoder unless raw.
func writeStream(w io.Writer, raw bool, write func(io.Writer) error) error {
	if raw {
		return write(w)
	}
	// The fastest level: the sending pod is often given a fraction of a core, and
	// compressing slower than the network turns a saving into a delay.
	cw := &cutWriter{w: w}
	zw, err := zstd.NewWriter(cw, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return err
	}
	if err := write(zw); err != nil {
		// Closing writes the frame's end and checksum, which would let the remote
		// decompress what was cut short as if it were whole.
		cw.cut()
		zw.Close()
		return err
	}
	return zw.Close()
}

// cutWriter passes writes through to w until cut, then discards them.
type cutWriter struct {
	mu    sync.Mutex
	w     io.Writer
	isCut bool
}

func (c *cutWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isCut {
		return len(p), nil
	}
	return c.w.Write(p)
}

func (c *cutWriter) cut() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isCut = true
}

// compressedMagics are the leading bytes of formats that a second pass of zstd
// only spends CPU on.
var compressedMagics = [][]byte{
	{0x28, 0xb5, 0x2f, 0xfd},         // zstd
	{0x1f, 0x8b},                     // gzip
	{0xfd, '7', 'z', 'X', 'Z', 0x00}, // xz
	{'B', 'Z', 'h'},                  // bzip2
	{0x04, 0x22, 0x4d, 0x18},         // lz4
	{'P', 'K', 0x03, 0x04},           // zip
}

// isCompressed reports whether the file at path starts like a compressed format.
func isCompressed(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	head := make([]byte, 6)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	for _, m := range compressedMagics {
		if bytes.HasPrefix(head[:n], m) {
			return true, nil
		}
	}
	return false, nil
}

// allCompressed reports whether dir holds at least one regular file and every
// one is compressed already. It follows a symlinked root as tarDir does.
func allCompressed(dir string) (bool, error) {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	found := false
	errMixed := errors.New("mixed")
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		c, err := isCompressed(p)
		if err != nil {
			return err
		}
		if !c {
			return errMixed
		}
		found = true
		return nil
	})
	if errors.Is(err, errMixed) {
		return false, nil
	}
	return found && err == nil, err
}

// getCommand tars the contents of remoteDir (not the directory itself) to stdout as
// a zstd stream, so they land directly under the local destination.
//
// A missing directory is forgiven, tar's own failures are not. `cd && tar || true`
// forgave both: a tar that exited non-zero -- an unreadable file, or one that changed
// while being read -- still emitted a valid archive of whatever it managed to read,
// untar accepted it, and GetDir returned nil. A partial download was reported as a
// complete one. tar runs the compressor itself for the same reason: in a pipeline
// its exit status would be lost.
func getCommand(remoteDir string) string {
	q := util.ShellQuote(remoteDir)
	return fmt.Sprintf("if [ -d %s ]; then cd %s && tar --use-compress-program='zstd -1 -T0' -cf - .; fi", q, q)
}

// GetDir streams a remote directory's contents into localDir. Best-effort: a
// missing remote path is not an error, since an epilogue runs precisely when the
// files it wants may have never been produced.
func (s *SSH) GetDir(remoteDir, localDir string) error {
	sess, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	if err := sess.Start(getCommand(remoteDir)); err != nil {
		return err
	}
	// One goroutine: the default decodes ahead in the background, which would read
	// stdout concurrently with the drain below.
	zr, err := zstd.NewReader(stdout, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := untar(zr, localDir); err != nil {
		// Drain first: abandoning the pipe while the remote tar writes blocks it
		// forever, and Wait with it.
		_, _ = io.Copy(io.Discard, stdout)
		_ = sess.Wait()
		return err
	}
	return sess.Wait()
}

// tarDir writes a tar of dir's contents (paths relative to dir) to w.
func tarDir(dir string, w io.Writer) error {
	// The root only. runonvm picks PutDir from os.Stat, which follows a symlink, but
	// Walk does not follow the root it is given: its single visit is the link itself,
	// skipped as ".", so `--put link-to-dir` shipped an empty archive and reported
	// success. Links inside the tree are still shipped as links.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	tw := tar.NewWriter(w)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Walk lstats, so the link target must reach the header: an empty link name
		// writes a size-0 TypeSymlink that the body copy below then overflows.
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		// Only regular files have a body; a socket or device has nothing to ship.
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		// Not deferred: that would hold every descriptor until the walk finished.
		_, err = io.Copy(tw, f)
		f.Close()
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// untar extracts a tar stream into destDir, guarding against paths that would
// escape it.
func untar(r io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	// Rooted at destDir. The lexical check this replaces compared against destDir as
	// spelled, so it rejected valid destinations ("./artifacts" never equals the
	// cleaned "artifacts") and could not see through a symlink already in destDir. A
	// root refuses to traverse out, with no window between checking and opening.
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer root.Close()

	// An empty stream (missing remote dir) reads as an empty archive.
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		// Relative to the root, so destDir's spelling stops mattering.
		name := strings.TrimPrefix(path.Clean("/"+hdr.Name), "/")
		if name == "" {
			// The archive's own root entry, which tar writes as "." or "./".
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if dir := path.Dir(name); dir != "." {
				if err := root.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			f, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // CI artifacts we produced
				f.Close()
				return err
			}
			f.Close()
		}
	}
}
