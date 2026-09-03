// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package createvm

import "testing"

func TestParseDiskGB(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"200GB", 200},
		{"200G", 200},
		{"200", 200},
		{" 200gb ", 200},
		{"1500GB", 1500},
	} {
		got, err := parseDiskGB(tc.in)
		if err != nil {
			t.Errorf("parseDiskGB(%q) errored: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDiskGB(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDiskGBRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "GB", "200GBx", "two hundred", "200TB"} {
		if _, err := parseDiskGB(in); err == nil {
			t.Errorf("parseDiskGB(%q) accepted garbage", in)
		}
	}
}

// The error has to name the value the operator actually set, not the parser's
// trimmed-and-uppercased leftovers -- that is what makes a misconfigured env var
// findable from a CI log.
func TestParseDiskGBErrorQuotesOriginalInput(t *testing.T) {
	_, err := parseDiskGB(" 200GBx ")
	if err == nil {
		t.Fatal("want an error")
	}
	if want := `" 200GBx "`; !containsStr(err.Error(), want) {
		t.Errorf("error %q does not contain the original input %s", err, want)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
