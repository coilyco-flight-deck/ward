package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/audit"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/config"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/sandbox"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/shell"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/verb"
	"github.com/urfave/cli/v3"
)

// Runner owns the shell runner + audit writer for ward's audited verbs.
// Mirrors coily's Runner minus the layered-config / lockdown-profile layer.
type Runner struct {
	Runner *shell.Runner
	Audit  *audit.Writer
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

// resolveInvokeCWD picks the operator's invoke-time cwd (vs the post-cd
// subprocess cwd): $COILY_INVOKE_CWD, then $OLDPWD, then os.Getwd().
func resolveInvokeCWD() string {
	for _, env := range []string{"COILY_INVOKE_CWD", "OLDPWD"} {
		v := strings.TrimSpace(os.Getenv(env))
		if v == "" {
			continue
		}
		// #nosec G304 -- read-only stat for cwd routing; no file open follows.
		if info, err := os.Stat(filepath.Clean(v)); err == nil && info.IsDir() {
			return v
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

// defaultPrimaryOrgs is the fleet's primary-org set - the brew tap/formula
// scope allowlist. Mirrors coily's defaultPrimaryOrgs.
func defaultPrimaryOrgs() []string {
	return []string{"coilysiren", "coilyco-bridge", "coilyco-flight-deck"}
}

// primaryOrgs returns the brew tap/formula scope allowlist.
func (r *Runner) primaryOrgs() []string {
	return defaultPrimaryOrgs()
}
