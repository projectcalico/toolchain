// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package runonvm

import (
	"os/exec"
	"strings"
	"testing"
)

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

// shellQuote's output is sourced by bash on the VM, so the real contract is that
// the value round-trips through a shell byte for byte. Ask a shell.
func TestShellQuoteRoundTripsThroughBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	for _, val := range []string{
		"plain",
		"with space",
		"it's",
		`double"quote`,
		"$(rm -rf /)",
		"`backtick`",
		"semi; echo pwned",
		`back\slash`,
		"new\nline",
		"tab\there",
		"*glob*",
		"${VAR}",
		"",
		"''",
		`'\''`,
	} {
		script := "printf %s " + shellQuote(val)
		out, err := exec.Command("bash", "-c", script).Output()
		if err != nil {
			t.Errorf("bash rejected quoting of %q: %v", val, err)
			continue
		}
		if string(out) != val {
			t.Errorf("round trip of %q gave %q", val, out)
		}
	}
}

// The env file is `export NAME=<quoted>` lines that the script sources; make sure
// a hostile value cannot break out of the assignment.
func TestShellQuoteInExportLine(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	evil := "x'; touch /tmp/pwned-by-runonvm; echo '"
	script := "export EVIL=" + shellQuote(evil) + "\nprintf %s \"$EVIL\""
	out, err := exec.Command("bash", "-c", script).Output()
	if err != nil {
		t.Fatalf("bash rejected the export line: %v", err)
	}
	if string(out) != evil {
		t.Fatalf("value did not survive the export line: got %q, want %q", out, evil)
	}
	if strings.Contains(script, "\ntouch") {
		t.Fatal("quoting let the payload escape onto its own line")
	}
}
