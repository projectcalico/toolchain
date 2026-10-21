// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package runonvm

import "testing"

func TestSplitPair(t *testing.T) {
	for _, tc := range []struct {
		in   string
		a, b string
		ok   bool
	}{
		{"local:remote", "local", "remote", true},
		{"/a/b:/c/d", "/a/b", "/c/d", true},
		// Splits on the FIRST colon, so a remote path may contain one.
		{"VAR:/tmp/x:y", "VAR", "/tmp/x:y", true},
		{"", "", "", false},
		{"nocolon", "", "", false},
	} {
		a, b, ok := splitPair(tc.in)
		if a != tc.a || b != tc.b || ok != tc.ok {
			t.Errorf("splitPair(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, a, b, ok, tc.a, tc.b, tc.ok)
		}
	}
}
