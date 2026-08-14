package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/gittree"
	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/repocfg"
	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/audit"
	"github.com/urfave/cli/v3"
)

// execCommand returns the `exec` verb. See docs/exec-verb.md.
func execCommand() *cli.Command {
	cfg, loadErr := loadDefault()
	if loadErr != nil || cfg == nil || len(cfg.Commands) == 0 {
		return &cli.Command{
			Name:  "exec",
			Usage: "Run a named command from .ward/ward.yaml (no config reachable)",
			Action: func(_ context.Context, _ *cli.Command) error {
				if loadErr != nil {
					return loadErr
				}
				return errNoConfig
			},
		}
	}
	subs := make([]*cli.Command, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		subs = append(subs, buildExecLeaf(cfg, c))
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name < subs[j].Name })
	repoRoot := filepath.Dir(filepath.Dir(cfg.Path))
	return &cli.Command{
		Name:     "exec",
		Usage:    "Run a command declared in " + cfg.Path,
		Commands: subs,
		Description: fmt.Sprintf(
			"Per-repo command declared in %s. Expands to a pre-validated argv "+
				"and runs with cwd set to %s. Every argv token is checked against "+
				"umbra's shell-metacharacter policy before execve. Repo verbs "+
				"require a clean+synced named branch, or a clean Forgejo Actions "+
				"pull-request merge checkout whose CI and Git evidence agree. The "+
				"declaring ward.yaml stays committed so the audit row is reconstructable; "+
				"--audit-override-dirty bypasses named-branch refusals with an audit tag.",
			cfg.Path, repoRoot,
		),
	}
}

// buildExecLeaf wraps one config command as a cli.Command that runs the
// argv through the verb pipeline + clean-tree gate. See docs/exec-verb.md.
func buildExecLeaf(cfg *repocfg.Config, rc repocfg.Command) *cli.Command {
	repoRoot := filepath.Dir(filepath.Dir(cfg.Path))
	usage := rc.Description
	if usage == "" {
		usage = "Run: " + strings.Join(rc.Argv, " ")
	}
	return &cli.Command{
		Name:      rc.Name,
		Usage:     usage,
		ArgsUsage: "[-- extra args]",
		Description: fmt.Sprintf(
			"Per-repo command declared in %s.\nExpands to: %s\nRuns in: %s\n\n"+
				"Runs through umbra's verb pipeline: every argv token is "+
				"validated against the shell-metacharacter policy, one audit row "+
				"is appended, and the repo gate requires either a clean+synced "+
				"named branch or a clean, validated Forgejo Actions pull-request "+
				"merge checkout. It refuses an uncommitted ward.yaml or stale branch "+
				"(--audit-override-dirty bypasses named-branch refusals with audit_override=true).",
			cfg.Path, strings.Join(rc.Argv, " "), repoRoot,
		),
		Action: func(ctx context.Context, c *cli.Command) error {
			return newRunner().runExecLeaf(ctx, c, cfg, rc)
		},
	}
}

// runExecLeaf runs one repo-declared command through the verb pipeline,
// with the clean-tree gate firing inside the wrapped Action.
func (r *Runner) runExecLeaf(ctx context.Context, c *cli.Command, cfg *repocfg.Config, rc repocfg.Command) error {
	repoRoot := filepath.Dir(filepath.Dir(cfg.Path))
	verbName := "repo." + rc.Name
	var (
		capturedState *gittree.State
		capturedCI    *audit.CIContext
		overrideUsed  bool
	)
	spec := verb.Spec{
		Name:       verbName,
		SkipPolicy: rc.AllowMetacharacters,
		ArgsFunc: func(cmd *cli.Command) (map[string]string, []string) {
			positional := append([]string{}, rc.Argv...)
			positional = append(positional, cmd.Args().Slice()...)
			return nil, positional
		},
		OnComplete: func(rec *audit.Record) {
			if rc.AllowMetacharacters {
				rec.PolicySkipped = true
			}
			applyExecGateAudit(rec, capturedState, capturedCI, overrideUsed)
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			state, ci, used, err := runExecGate(cmd, repoRoot, cfg.Path, verbName, os.Getenv("WARD_READONLY") == "1" && rc.Name == "surface-check")
			if err != nil {
				return err
			}
			capturedState = state
			capturedCI = ci
			overrideUsed = used
			fmt.Fprintf(os.Stderr, "ward: exec %s in %s\n", rc.Name, repoRoot)
			argv := append([]string{}, rc.Argv[1:]...)
			argv = append(argv, cmd.Args().Slice()...)
			return r.Runner.ExecIn(ctx, repoRoot, rc.Argv[0], argv...)
		},
	}
	return r.WrapVerb(spec, r.Audit)(ctx, c)
}

func applyExecGateAudit(rec *audit.Record, state *gittree.State, ci *audit.CIContext, overrideUsed bool) {
	if state != nil {
		rec.WorkingTreeStatus = state.Status
		rec.AuditOverride = overrideUsed
	}
	rec.CI = ci
}
