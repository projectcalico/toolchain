// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package util

import (
	"fmt"
	"regexp"
	"strings"
)

// ShellQuote single-quotes s for a POSIX shell: an embedded apostrophe is closed,
// backslash-escaped and reopened, so any value survives verbatim. The literal
// escape is only in the code below -- gofmt rewrites a doubled apostrophe in a
// comment into a curly quote, so it cannot be spelled here.
//
// Use this, never fmt %q, for anything interpolated into a remote command. %q
// emits a Go double-quoted string, and bash still expands $(...) and backticks
// inside double quotes, so a path like /tmp/x$(id -u) would execute.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellName is the portable environment-variable name: a leading letter or
// underscore, then letters, digits or underscores.
var shellName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CheckShellName rejects a name that cannot be used as a shell variable. Values
// can be quoted, but a NAME is interpolated bare into `export NAME=...`, so
// something like "FOO; rm -rf /" would run when the file is sourced.
func CheckShellName(name string) error {
	if !shellName.MatchString(name) {
		return fmt.Errorf("%q is not a valid environment variable name", name)
	}
	return nil
}
