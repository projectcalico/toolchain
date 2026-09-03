// Copyright (c) 2026 Tigera, Inc. All rights reserved.

// Package runonvm runs a script on a GCE VM over SSH, with no gcloud -- the
// generic "runOn: vm" primitive for the scratch-utils image. It ships files and
// secrets to the VM, runs a script there (a FILE, never a command string, so
// nothing has to survive three levels of shell quoting), pulls artifacts back on
// ANY exit, and exits with the script's own status.
//
// It is meant to be the `command` of an Argo `script` template: Argo writes the
// template's `source:` to a temp file and appends its path as the final argument,
// so `runonvm <flags> <source-file>` ships that source to the VM and runs it there
// -- i.e. the workflow's `source:` block executes naturally on the VM. (--script
// is the equivalent for direct/CLI use.)
//
// The VM (its name/zone/project) comes from env, same as createvm/deletevm; the
// compute SA is materialized by util. Everything job-specific -- which files,
// which secrets, which artifacts -- is a flag, so this stays generic across CI
// jobs.
//
//	command: [runonvm,
//	          --put-env, ENV_VAR:remote/path,     # repeatable; env value -> 0600 file
//	          --env,     ENV_VAR,                 # repeatable; forwarded into a sourced env file
//	          --get,     remote/dir:local/dir]    # repeatable; best-effort, on exit
//	source: |                                     # Argo appends this file; it runs on the VM
//	  ...
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

// setupTimeout bounds the compute-API phase (find the zone, inject the key, dial
// SSH) so a wedged operation fails the step instead of hanging it. It deliberately
// does NOT cover running the script -- that is the CI job itself, and it takes as
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

	// Argo passes the script template's staged source file as the trailing arg;
	// --script is the equivalent for direct use.
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

	// Bounded context for the compute-API phase only; the script run below is
	// unbounded (see setupTimeout).
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

	// Pull artifacts back on ANY exit, so a mid-run failure still returns logs.
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

	// Forward selected env vars into a file the script sources before running --
	// the generic path for a commit SHA, tokens, etc. (mirrors argoci's /tmp/secrets).
	// Values are shell-quoted, so any content is safe. An unset var is skipped
	// (lenient: the script decides whether a missing one is fatal).
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

	// Upload the script to a temp path and run it (a file, not a command string).
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

// shellQuote single-quotes s for safe use in a POSIX shell: an embedded
// apostrophe is closed, backslash-escaped, and reopened, so any value survives
// verbatim. The literal escape is in the ReplaceAll below and deliberately not
// repeated here -- gofmt rewrites a doubled apostrophe in a comment into a curly
// quote, which is where the misrendered sequence in review came from.
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
