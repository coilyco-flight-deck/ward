package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/sandbox"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
)

// init registers ward's own ".ward" app dir: audit rows land in ~/.ward/audit,
// session sentinels under it, and the dispatch queue at /tmp/ward-dispatch-queue.
func init() {
	config.SetAppDir(".ward")
}

// Runner owns the shell runner + audit writer for ward's audited verbs.
// Mirrors coily's Runner minus the layered-config / lockdown-profile layer.
type Runner struct {
	Runner *shell.Runner
	Audit  *audit.Writer

	// pullHeartbeatInterval overrides the silenced-pull heartbeat cadence
	// (ward#322); zero means pullHeartbeatDefault. A field so tests can shrink it.
	pullHeartbeatInterval time.Duration
}

// newRunner builds the production Runner, lazily (only inside a pkg action)
// so lean verbs like hook/version never touch the audit directory.
func newRunner() *Runner {
	path, err := config.DefaultAuditPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward: fatal: resolve audit path: %v\n", err)
		os.Exit(2)
	}
	aw := audit.NewWriter(path)
	// Fail loud if the audit dir is not writable, rather than silently drop.
	if err := aw.Preflight(); err != nil {
		fmt.Fprintf(os.Stderr, "ward: fatal: %v\n", err)
		os.Exit(2)
	}
	return &Runner{
		Runner: &shell.Runner{
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
			Stdin:   os.Stdin,
			Sandbox: sandboxSpec(),
		},
		Audit: aw,
	}
}

// leanRunner builds a Runner without newRunner's fatal audit preflight, so a
// lean verb tree (dispatch, ops) constructs at startup without an os.Exit.
func leanRunner() *Runner {
	path, err := config.DefaultAuditPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward: fatal: resolve audit path: %v\n", err)
		os.Exit(2)
	}
	return &Runner{
		Runner: &shell.Runner{
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
			Stdin:   os.Stdin,
			Sandbox: sandboxSpec(),
		},
		Audit: audit.NewWriter(path),
	}
}

// wardSandboxTools is the set of wrapped tools ward shims inside the jail.
// brew is the first enforced surface; extend as other passthroughs land.
var wardSandboxTools = []string{"brew"}

// sandboxSpec builds the jail spec for ward's audited verbs (inert off Linux /
// inside a jail). Returns nil if the binary path is unresolvable.
func sandboxSpec() *sandbox.Spec {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	return &sandbox.Spec{SelfExe: exe, Tools: wardSandboxTools}
}

// WrapVerb wraps spec through cli-guard's verb pipeline, setting the
// invoke-cwd resolver. Ward injects no profile evaluator (nil is fine).
func (r *Runner) WrapVerb(spec verb.Spec, writer *audit.Writer) cli.ActionFunc {
	if spec.ResolveInvokeCWD == nil {
		spec.ResolveInvokeCWD = resolveInvokeCWD
	}
	return verb.Wrap(spec, writer)
}

// startupCWD is the process cwd captured at load, before any verb-time chdir, so
// resolveInvokeCWD reports where the operator actually launched ward.
var startupCWD, _ = os.Getwd()

// resolveInvokeCWD reports the operator's invoke-time cwd: COILY_INVOKE_CWD override,
// then the startup cwd, then a live os.Getwd(). $OLDPWD is not consulted (ward#302).
func resolveInvokeCWD() string {
	if v := strings.TrimSpace(os.Getenv("COILY_INVOKE_CWD")); v != "" {
		// #nosec G304 -- read-only stat for cwd routing; no file open follows.
		if info, err := os.Stat(filepath.Clean(v)); err == nil && info.IsDir() {
			return v
		}
	}
	if startupCWD != "" {
		return startupCWD
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

// defaultPrimaryOrgs is the fleet's primary-org set - the brew tap/formula
// scope allowlist. Mirrors coily's defaultPrimaryOrgs.
func defaultPrimaryOrgs() []string {
	return []string{"coilysiren", "coilyco-bridge", "coilyco-flight-deck", "coilyco-gaming"}
}

// primaryOrgs returns the brew tap/formula scope allowlist.
func (r *Runner) primaryOrgs() []string {
	return defaultPrimaryOrgs()
}
