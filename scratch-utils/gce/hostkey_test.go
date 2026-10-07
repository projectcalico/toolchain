// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// newHostKey returns a host key and the guest-attribute pair that publishes it:
// Key the algorithm, Value the bare base64 blob.
func newHostKey(t *testing.T) (ssh.PublicKey, string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	_ = pub
	key := signer.PublicKey()
	// MarshalAuthorizedKey gives "<algo> <base64>\n"; guest attributes split those
	// into the entry's key and value, so split it the same way.
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	algo, blob, ok := strings.Cut(line, " ")
	if !ok {
		t.Fatalf("unexpected authorized key line %q", line)
	}
	return key, algo, blob
}

// The callback is the whole security control: anything it lets through is a host
// we will ship secrets to.
func TestAcceptOneOf(t *testing.T) {
	a, _, _ := newHostKey(t)
	b, _, _ := newHostKey(t)
	stranger, _, _ := newHostKey(t)

	addr := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 22}

	cb := acceptOneOf([]ssh.PublicKey{a, b})
	for i, k := range []ssh.PublicKey{a, b} {
		if err := cb("vm:22", addr, k); err != nil {
			t.Errorf("published key %d rejected: %v", i, err)
		}
	}
	if err := cb("vm:22", addr, stranger); err == nil {
		t.Fatal("a key the instance never published was accepted")
	}

	// An empty set must reject everything rather than default to allowing it --
	// a fetch that returned nothing must not read as "no restriction".
	if err := acceptOneOf(nil)("vm:22", addr, a); err == nil {
		t.Fatal("empty key set accepted a host key")
	}
}

// The rejection has to name the key, or a genuine mismatch is undiagnosable.
func TestAcceptOneOfErrorNamesTheFingerprint(t *testing.T) {
	a, _, _ := newHostKey(t)
	stranger, _, _ := newHostKey(t)

	err := acceptOneOf([]ssh.PublicKey{a})("vm:22", &net.TCPAddr{}, stranger)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if fp := ssh.FingerprintSHA256(stranger); !strings.Contains(err.Error(), fp) {
		t.Errorf("error %q does not name the offered key %s", err, fp)
	}
}

// Guest attributes hold the algorithm and the blob separately. Rejoining them has
// to produce exactly the key that was published, or pinning compares the wrong
// bytes and every connection fails.
func TestGuestAttributeEntryParsesBackToTheSameKey(t *testing.T) {
	want, algo, blob := newHostKey(t)

	got, _, _, _, err := ssh.ParseAuthorizedKey([]byte(algo + " " + blob))
	if err != nil {
		t.Fatalf("parse %s: %v", algo, err)
	}
	if string(got.Marshal()) != string(want.Marshal()) {
		t.Fatal("rejoined guest attribute entry is not the published key")
	}
	if err := acceptOneOf([]ssh.PublicKey{got})("vm:22", &net.TCPAddr{}, want); err != nil {
		t.Errorf("key parsed from guest attributes does not match itself: %v", err)
	}
}
