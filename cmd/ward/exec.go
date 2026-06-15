package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/gittree"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
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
				"cli-guard's shell-metacharacter policy before execve. Repo verbs "+
				"require a clean+synced tree with the declaring ward.yaml committed "+
				"so the audit row's argv can be reconstructed from git history; "+
				"--audit-override-dirty bypasses the gate with an audit tag.",
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
				"Runs through cli-guard's verb pipeline: every argv token is "+
				"validated against the shell-metacharacter policy, one audit row "+
				"is appended, and the clean+synced tree gate refuses if the "+
				"declaring ward.yaml is uncommitted or the branch is out of sync "+
				"(--audit-override-dirty bypasses with audit_override=true).",
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
			if capturedState == nil {
				return
			}
			rec.WorkingTreeStatus = capturedState.Status
			rec.AuditOverride = overrideUsed
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			state, used, err := runExecGate(cmd, repoRoot, cfg.Path, verbName)
			if err != nil {
				return err
			}
			if state.Status != "" {
				capturedState = state
				overrideUsed = used
			}
			fmt.Fprintf(os.Stderr, "ward: exec %s in %s\n", rc.Name, repoRoot)
			argv := append([]string{}, rc.Argv[1:]...)
			argv = append(argv, cmd.Args().Slice()...)
			return r.Runner.ExecIn(ctx, repoRoot, rc.Argv[0], argv...)
		},
	}
	return r.WrapVerb(spec, r.Audit)(ctx, c)
}
