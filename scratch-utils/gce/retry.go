// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/api/googleapi"
)

// retryBackoff is the first inter-attempt delay (doubled each retry). A package
// var so tests can shrink it.
var retryBackoff = time.Second

// retry runs fn, retrying on transient errors with an exponential backoff. GCE
// control-plane calls (get/set-metadata, operation waits) occasionally drop the
// HTTP/2 connection mid-request ("http2: client connection lost") or return a
// 429/5xx; a couple of retries turns that flake into a non-event. Permanent
// errors (a 4xx, a missing instance) return immediately.
//
// Every error is wrapped with what, including the permanent one -- the callers
// pass their whole message there ("get instance vm-1"), so returning fn's error
// bare would leave a 404 with nothing saying which call or which instance
// produced it.
func retry(ctx context.Context, what string, fn func() error) error {
	const attempts = 4
	backoff := retryBackoff
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if !isTransient(err) {
			return fmt.Errorf("%s: %w", what, err)
		}
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			// Name the last failure too: a bare deadline says nothing about what
			// we were failing at while the clock ran out.
			return fmt.Errorf("%s: %w (last attempt: %v)", what, ctx.Err(), err)
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return fmt.Errorf("%s: giving up after %d attempts: %w", what, attempts, err)
}

// isTransient reports whether err is worth retrying: a rate-limit/server error
// from the API, a network timeout, or one of the transport-level drops that
// surface as a plain error string (the HTTP/2 connection-lost flake among them).
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch gerr.Code {
		case 429, 500, 502, 503, 504:
			return true
		}
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	msg := err.Error()
	for _, m := range []string{
		"http2: client connection lost",
		"connection reset",
		"connection refused",
		"unexpected EOF",
		"i/o timeout",
		"TLS handshake",
		"server closed idle connection",
		"broken pipe",
	} {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
