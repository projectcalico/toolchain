// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package runonvm runs a script on a GCE VM over SSH: the generic "runOn: vm"
// primitive. It ships files and secrets, runs a script (a FILE, never a command
// string, so nothing has to survive three levels of shell quoting), pulls
// artifacts back on ANY exit, and exits with the script's own status.
//
// It is meant to be the `command` of an Argo `script` template. Argo writes the
// template's `source:` to a temp file and appends its path as the last argument,
// so that `source:` block just executes on the VM. (--script is the CLI
// equivalent.)
//
// The VM comes from env, as in createvm/deletevm. Everything job-specific --
// which files, secrets and artifacts -- is a flag, so this stays generic.
//
// The image ENTRYPOINT is the scratch-utils binary, so the subcommand is an
// ARGUMENT, not the command -- a bare `command: [runonvm]` overrides the entrypoint
// and fails with "executable file not found in $PATH".
//
//	command: [scratch-utils, runonvm,
//	          --put-env, ENV_VAR:remote/path,     # repeatable; env value -> 0600 file
//	          --env,     ENV_VAR,                 # repeatable; forwarded into a sourced env file
//	          --get,     remote/dir:local/dir]    # repeatable; best-effort, on exit
//	source: |                                     # Argo appends this file; it runs on the VM
//	  ...
//
// A plain container template can instead leave the entrypoint alone and pass
// `args: [runonvm, ...]`.
package runonvm

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/projectcalico/go-build/scratch-utils/gce"
	"github.com/projectcalico/go-build/scratch-utils/util"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// setupTimeout bounds the compute-API phase (find zone, inject key, dial SSH). It
// deliberately excludes running the script: that is the CI job, and it takes as
// long as it takes.
const setupTimeout = 10 * time.Minute

// Run executes the runonvm subcommand and returns its exit code.
func Run() int {
	return run(context.Background())
}

func run(ctx context.Context) int {
	var puts, putEnvs, envs, gets stringList
	scriptFlag := flag.String("script", "", "script to run on the VM (Argo appends the source file as a trailing arg; --script is the CLI equivalent)")
	user := flag.String("user", envOr("VM_SSH_USER", "ubuntu"), "SSH user")
	flag.Var(&puts, "put", "LOCAL:REMOTE file or dir to upload before running (repeatable)")
	flag.Var(&putEnvs, "put-env", "ENVVAR:REMOTE -- write an env var's value to a 0600 remote file (repeatable)")
	flag.Var(&envs, "env", "ENVVAR to forward into an env file the script sources before running (repeatable)")
	flag.Var(&gets, "get", "REMOTE:LOCAL dir to pull back on exit, best-effort (repeatable)")
	flag.Parse()

	// Argo passes the staged source file as the trailing arg.
	script := *scriptFlag
	if flag.NArg() > 0 {
		script = flag.Arg(0)
	}
	if script == "" {
		fmt.Fprintln(os.Stderr, "runonvm: need a script (trailing arg or --script)")
		return 2
	}

	name := os.Getenv("VM_NAME")
	if name == "" {
		fmt.Fprintln(os.Stderr, "runonvm: VM_NAME must be set")
		return 2
	}
	project := envOr("GCP_VM_PROJECT", "unique-caldron-775")

	if err := util.SetupComputeADC(); err != nil {
		fmt.Fprintf(os.Stderr, "runonvm: %v\n", err)
		return 1
	}
	client, err := gce.New(ctx, project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runonvm: %v\n", err)
		return 1
	}

	// Bounds setup only; the script run below is unbounded.
	setupCtx, cancelSetup := context.WithTimeout(ctx, setupTimeout)
	defer cancelSetup()

	zone := os.Getenv("ZONE")
	if zone == "" {
		zone, err = client.FindZone(setupCtx, name)
		if err != nil || zone == "" {
			fmt.Fprintf(os.Stderr, "runonvm: could not find zone for %s (set ZONE): %v\n", name, err)
			return 1
		}
	}

	fmt.Printf("[runonvm] connecting to %s in %s\n", name, zone)
	conn, err := client.DialSSH(setupCtx, zone, name, *user)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runonvm: %v\n", err)
		return 1
	}
	defer conn.Close()
	cancelSetup()

	// On ANY exit, so a mid-run failure still returns logs.
	defer func() {
		for _, g := range gets {
			remote, local, ok := splitPair(g)
			if !ok {
				continue
			}
			if err := conn.GetDir(remote, local); err != nil {
				fmt.Fprintf(os.Stderr, "[runonvm] get %s: %v (continuing)\n", remote, err)
			} else {
				fmt.Printf("[runonvm] pulled %s -> %s\n", remote, local)
			}
		}
	}()

	// Ship files and secrets before running.
	for _, p := range puts {
		local, remote, ok := splitPair(p)
		if !ok {
			fmt.Fprintf(os.Stderr, "runonvm: bad --put %q (want LOCAL:REMOTE)\n", p)
			return 2
		}
		info, err := os.Stat(local)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runonvm: --put %s: %v\n", local, err)
			return 1
		}
		if info.IsDir() {
			err = conn.PutDir(local, remote)
		} else {
			err = conn.PutFile(local, remote, 0o644)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "runonvm: %v\n", err)
			return 1
		}
		fmt.Printf("[runonvm] put %s -> %s\n", local, remote)
	}
	for _, pe := range putEnvs {
		envVar, remote, ok := splitPair(pe)
		if !ok {
			fmt.Fprintf(os.Stderr, "runonvm: bad --put-env %q (want ENVVAR:REMOTE)\n", pe)
			return 2
		}
		val, present := os.LookupEnv(envVar)
		if !present {
			fmt.Fprintf(os.Stderr, "runonvm: --put-env %s: env var not set\n", envVar)
			return 1
		}
		if err := conn.PutData([]byte(val), remote, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "runonvm: %v\n", err)
			return 1
		}
		fmt.Printf("[runonvm] put-env %s -> %s (0600)\n", envVar, remote)
	}

	// Forward env vars through a file the script sources -- the generic path for a
	// commit SHA, tokens, etc. Values are shell-quoted, so any content is safe. An
	// unset var is skipped; the script decides whether that is fatal.
	remoteEnv := "/tmp/runonvm.env"
	haveEnv := false
	if len(envs) > 0 {
		var b strings.Builder
		for _, name := range envs {
			val, present := os.LookupEnv(name)
			if !present {
				fmt.Fprintf(os.Stderr, "[runonvm] --env %s not set, skipping\n", name)
				continue
			}
			fmt.Fprintf(&b, "export %s=%s\n", name, shellQuote(val))
			haveEnv = true
		}
		if haveEnv {
			if err := conn.PutData([]byte(b.String()), remoteEnv, 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "runonvm: write env file: %v\n", err)
				return 1
			}
		}
	}

	// A file, not a command string.
	remoteScript := path.Join("/tmp", path.Base(script))
	if err := conn.PutFile(script, remoteScript, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "runonvm: upload script: %v\n", err)
		return 1
	}
	runCmd := "bash " + remoteScript
	if haveEnv {
		runCmd = fmt.Sprintf(". %s && bash %s", remoteEnv, remoteScript)
	}
	fmt.Printf("[runonvm] running %s on %s\n", remoteScript, name)
	code, err := conn.Run(runCmd, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runonvm: run: %v\n", err)
		return 1
	}
	fmt.Printf("[runonvm] script finished (rc=%d)\n", code)
	return code
}

// shellQuote single-quotes s for a POSIX shell: an embedded apostrophe is closed,
// backslash-escaped and reopened, so any value survives verbatim. The literal
// escape is only in the code below -- gofmt rewrites a doubled apostrophe in a
// comment into a curly quote, so it cannot be spelled here.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// splitPair splits "A:B" on the first colon.
func splitPair(s string) (a, b string, ok bool) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
