package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/guardfile"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/respfmt"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/specverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/valuesource"
	"github.com/urfave/cli/v3"
)

// ops.go mounts the ward-kdl forgejo guardfile runtime inside the ward binary
// as `ward ops forgejo` (ward#92). See docs/ops-forgejo.md.

// opsAssets embeds copies of the cmd/ward-kdl forgejo guardfile + spec lock
// (opsassets_test.go guards drift). See docs/ops-forgejo-in-ward.md.

//go:embed opsassets/forgejo.guardfile.kdl opsassets/forgejo.swagger.lock.json
var opsAssets embed.FS

// opsForgejoGuardfilePath / opsForgejoSpecLockPath name the embed paths once, so
// the runtime mount and the drift test agree.
const (
	opsForgejoGuardfilePath = "opsassets/forgejo.guardfile.kdl"
	opsForgejoSpecLockPath  = "opsassets/forgejo.swagger.lock.json"
)

// opsCommand is the `ops` umbrella: operator verbs run by cli-guard's specverb
// runtime. A build error degrades to a leaf that surfaces it on invocation.
func opsCommand() *cli.Command {
	forgejo, err := buildForgejoOps()
	if err != nil {
		forgejo = &cli.Command{
			Name:  "forgejo",
			Usage: "guarded Forgejo REST surface (unavailable)",
			Action: func(context.Context, *cli.Command) error {
				return fmt.Errorf("ward ops forgejo: guardfile runtime failed to mount: %w", err)
			},
		}
	}
	return &cli.Command{
		Name:     "ops",
		Usage:    "operator verbs routed through the ward-kdl guardfile runtime",
		Commands: []*cli.Command{forgejo},
	}
}

// buildForgejoOps parses the embedded guardfile + spec lock and specverb.Builds
// the `forgejo` group, audited through ward and SSM-resolved via the aws runner.
func buildForgejoOps() (*cli.Command, error) {
	r := leanRunner()

	gfBytes, err := opsAssets.ReadFile(opsForgejoGuardfilePath)
	if err != nil {
		return nil, fmt.Errorf("read embedded guardfile: %w", err)
	}
	gf, err := guardfile.Parse(gfBytes)
	if err != nil {
		return nil, fmt.Errorf("parse guardfile: %w", err)
	}
	spec, err := opsAssets.ReadFile(opsForgejoSpecLockPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded spec lock: %w", err)
	}

	forgejo, err := specverb.Build(specverb.Config{
		Guardfile: gf,
		Spec:      spec,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			return r.WrapVerb(s, r.Audit)
		},
		// The guardfile's `value ssm` auth resolves through ward's audited aws
		// runner (no AWS SDK), lazily - mount and --dry-run never touch SSM.
		Providers: map[string]valuesource.Provider{
			"ssm": r.ssmValueResolver,
		},
	})
	if err != nil {
		return nil, err
	}
	r.overrideForgejoViewIssue(forgejo)
	return forgejo, nil
}

// overrideForgejoViewIssue swaps the built `issue view` leaf for the lean
// projection (ward#225). See docs/ops-forgejo-in-ward.md.
func (r *Runner) overrideForgejoViewIssue(forgejo *cli.Command) {
	issue := subCommandNamed(forgejo, "issue")
	if issue == nil {
		return
	}
	view := subCommandNamed(issue, "view")
	if view == nil {
		return
	}
	view.Action = r.WrapVerb(verb.Spec{
		Name:       "ward-kdl.ops.forgejo.issue.view",
		SkipPolicy: true, // read-only GETs; this leaf does its own scope gate below
		ArgsFunc: func(c *cli.Command) (map[string]string, []string) {
			return nil, c.Args().Slice()
		},
		Action: r.runForgejoViewIssue,
	}, r.Audit)
}

// subCommandNamed returns parent's subcommand named name, or nil.
func subCommandNamed(parent *cli.Command, name string) *cli.Command {
	for _, c := range parent.Commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// runForgejoViewIssue prints the lean {issue, comments} projection, honoring
// --output/--dry-run and the guardfile's `restrict owner coily*` gate (ward#225).
func (r *Runner) runForgejoViewIssue(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 3 {
		return fmt.Errorf("ward ops forgejo issue view: need <owner> <repo> <index>, got %d arg(s)", len(args))
	}
	owner, repo, idxArg := args[0], args[1], args[2]
	if !strings.HasPrefix(owner, "coily") {
		return fmt.Errorf("ward ops forgejo issue view: owner %q is out of scope; restricted to coily* owners", owner)
	}
	number, err := strconv.Atoi(idxArg)
	if err != nil {
		return fmt.Errorf("ward ops forgejo issue view: index %q is not a number: %w", idxArg, err)
	}
	output := cmd.String(flagOutput)
	if cmd.Bool(flagDryRun) {
		base := strings.TrimSuffix(forgejoBaseURL, "/")
		fmt.Printf("would GET %s/api/v1/repos/%s/%s/issues/%d\n", base, owner, repo, number)
		fmt.Printf("would GET %s/api/v1/repos/%s/%s/issues/%d/comments\n", base, owner, repo, number)
		return nil
	}
	token, err := r.forgejoAPIToken(ctx)
	if err != nil {
		return err
	}
	view, err := newForgejoClient(forgejoBaseURL, token).viewIssue(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(view)
	if err != nil {
		return fmt.Errorf("ward ops forgejo issue view: marshal: %w", err)
	}
	rendered, err := respfmt.Render(raw, "", output)
	if err != nil {
		return fmt.Errorf("ward ops forgejo issue view: render: %w", err)
	}
	fmt.Print(string(rendered))
	return nil
}

// flagOutput / flagDryRun mirror the engine leaf's flag names so this override
// reads the same `--output` / `--dry-run` the guardfile action declared.
const (
	flagOutput = "output"
	flagDryRun = "dry-run"
)

// ssmValueResolver fetches an SSM SecureString through ward's audited aws runner,
// parameterized by the guardfile-supplied path (vs forgejoAPIToken's hardcoded one).
func (r *Runner) ssmValueResolver(ctx context.Context, ssmPath string) (string, error) {
	out, err := r.Runner.Capture(ctx, "aws",
		"ssm", "get-parameter",
		"--name", ssmPath,
		"--with-decryption",
		"--query", "Parameter.Value",
		"--output", "text",
	)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", ssmPath, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("%s resolved to empty value", ssmPath)
	}
	return v, nil
}
