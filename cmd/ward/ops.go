package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/guardfile"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/respfmt"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/specverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/valuesource"
	"github.com/urfave/cli/v3"
)

// ops.go is ward's OWN in-binary embed of the forgejo guardfile runtime, mounted
// as `ward ops forgejo` (ward#92, #270). See docs/ops-forgejo-in-ward.md.

// opsAssets embeds the forgejo REST guardfile + spec lock (`.generated.` = cp
// copies of cmd/ward-kdl/) plus the hand-written admin/doctor guardfile (ward#81).

//go:embed opsassets/*.generated.kdl opsassets/*.generated.json
//go:embed opsassets/forgejo-admin.guardfile.kdl
var opsAssets embed.FS

// Embed paths named once so the runtime mount and the drift test agree; the
// admin path is the exec-dialect remote-exec slice (ward#81).
const (
	opsForgejoGuardfilePath      = "opsassets/forgejo.guardfile.generated.kdl"
	opsForgejoSpecLockPath       = "opsassets/forgejo.swagger.lock.generated.json"
	opsForgejoAdminGuardfilePath = "opsassets/forgejo-admin.guardfile.kdl"
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
	// Re-root the group prefix to ward's brand so these in-process verbs audit as
	// `ward.ops.forgejo.*`, not `ward-kdl.*` (ward#270, docs/ops-forgejo-in-ward.md).
	rerootGroupToWard(gf.Group)
	spec, err := opsAssets.ReadFile(opsForgejoSpecLockPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded spec lock: %w", err)
	}

	forgejo, err := specverb.Build(specverb.Config{
		Guardfile: gf,
		Spec:      spec,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			// Brokered (WARD_BROKER_SOCK set): writes route to the broker, out-of-tier
			// mutations refuse, reads stay direct. Unset: unchanged (ward#334).
			return r.brokerForgejoAction(s.Name, r.WrapVerb(s, r.Audit))
		},
		// The guardfile's `value ssm` auth resolves through forgejoTokenResolver,
		// lazily - mount and --dry-run never touch the token source.
		Providers: map[string]valuesource.Provider{
			"ssm": r.forgejoTokenResolver,
		},
	})
	if err != nil {
		return nil, err
	}
	r.overrideForgejoViewIssue(forgejo)
	r.overrideForgejoCreateIssue(forgejo)

	// Graft the exec-dialect admin/doctor remote-exec slice onto the same
	// forgejo group, so both transports share one operator verb (ward#81).
	if err := graftForgejoAdminExec(forgejo, r); err != nil {
		return nil, err
	}
	return forgejo, nil
}

// forgejoTokenResolver resolves the Forgejo bot token: the baked $FORGEJO_TOKEN in
// a container (no SSM/aws), else the coilyco-ops bot token from SSM on a host.
func (r *Runner) forgejoTokenResolver(ctx context.Context, ssmPath string) (string, error) {
	if tok := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); tok != "" {
		return tok, nil
	}
	return r.ssmValueResolver(ctx, ssmPath)
}

// graftForgejoAdminExec appends the exec-dialect guardfile's built admin/doctor
// subtrees onto forgejo. See docs/ops-forgejo-admin.md.
func graftForgejoAdminExec(forgejo *cli.Command, r *Runner) error {
	gfBytes, err := opsAssets.ReadFile(opsForgejoAdminGuardfilePath)
	if err != nil {
		return fmt.Errorf("read embedded admin guardfile: %w", err)
	}
	gf, err := execverb.Parse(gfBytes)
	if err != nil {
		return fmt.Errorf("parse admin guardfile: %w", err)
	}
	group, err := execverb.Build(execverb.Config{
		Guardfile: gf,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			return r.WrapVerb(s, r.Audit)
		},
		// Run is nil: the exec-dialect leaves shell out to the real `ssh`
		// transport. The wrapped binary owns its own credentials, so no SSM auth.
	})
	if err != nil {
		return fmt.Errorf("build admin guardfile: %w", err)
	}
	forgejo.Commands = append(forgejo.Commands, group.Commands...)
	return nil
}

// overrideForgejoViewIssue swaps the built `issue view` leaf for the lean
// projection (ward#225). See docs/ops-forgejo-view.md.
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
		Name:       "ward.ops.forgejo.issue.view",
		SkipPolicy: true, // read-only GETs; this leaf does its own scope gate below
		ArgsFunc: func(c *cli.Command) (map[string]string, []string) {
			return nil, c.Args().Slice()
		},
		Action: r.runForgejoViewIssue,
	}, r.Audit)
}

// flagQuiet is the machine-output flag overrideForgejoCreateIssue grafts onto
// `issue create` (ward#316).
const flagQuiet = "quiet"

// overrideForgejoCreateIssue grafts a --quiet machine-output mode onto `issue
// create`: terse `{owner}/{repo}#N` over YAML (docs/ops-forgejo-quiet.md, ward#316).
func (r *Runner) overrideForgejoCreateIssue(forgejo *cli.Command) {
	issue := subCommandNamed(forgejo, "issue")
	if issue == nil {
		return
	}
	create := subCommandNamed(issue, "create")
	if create == nil {
		return
	}
	orig := create.Action
	create.Flags = append(create.Flags, &cli.BoolFlag{
		Name:  flagQuiet,
		Usage: "on success print only the created issue ref ({owner}/{repo}#N) to stdout; signal failure by exit code",
	})
	create.Action = func(ctx context.Context, cmd *cli.Command) error {
		return runForgejoCreateIssueQuiet(ctx, cmd, orig)
	}
}

// runForgejoCreateIssueQuiet delegates to the engine action untouched unless
// --quiet is set, then projects+reshapes the new number to a ref (ward#316).
func runForgejoCreateIssueQuiet(ctx context.Context, cmd *cli.Command, orig cli.ActionFunc) error {
	if !cmd.Bool(flagQuiet) || cmd.Bool(flagDryRun) {
		return orig(ctx, cmd)
	}
	if cmd.IsSet(flagOutput) || cmd.IsSet(flagQuery) {
		return fmt.Errorf("ward ops forgejo issue create: --quiet cannot combine with --output/--query")
	}
	args := cmd.Args().Slice()
	if len(args) < 2 {
		return fmt.Errorf("ward ops forgejo issue create: need <owner> <repo>, got %d arg(s)", len(args))
	}
	owner, repo := args[0], args[1]
	// Force the engine to render only the new number as a bare scalar, then
	// reshape it into the ref. The leaf already carries --output/--query.
	if err := cmd.Set(flagOutput, respfmt.OutputText); err != nil {
		return fmt.Errorf("ward ops forgejo issue create: force --output: %w", err)
	}
	if err := cmd.Set(flagQuery, "number"); err != nil {
		return fmt.Errorf("ward ops forgejo issue create: force --query: %w", err)
	}
	out, err := captureLeafStdout(func() error { return orig(ctx, cmd) })
	if err != nil {
		return err
	}
	ref, err := formatCreatedIssueRef(owner, repo, out)
	if err != nil {
		return err
	}
	fmt.Println(ref)
	return nil
}

// formatCreatedIssueRef turns the engine's number projection into the terse
// `{owner}/{repo}#N` ref, rejecting any non-numeric capture (ward#316).
func formatCreatedIssueRef(owner, repo, captured string) (string, error) {
	num := strings.TrimSpace(captured)
	if _, err := strconv.Atoi(num); err != nil {
		return "", fmt.Errorf("ward ops forgejo issue create: unexpected response %q (no issue number)", captured)
	}
	return fmt.Sprintf("%s/%s#%s", owner, repo, num), nil
}

// captureLeafStdout redirects os.Stdout for fn and returns what it wrote; the
// payload is one tiny scalar, well under the pipe buffer, so no deadlock (ward#316).
func captureLeafStdout(fn func() error) (string, error) {
	orig := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("capture stdout: %w", err)
	}
	os.Stdout = pw
	runErr := fn()
	_ = pw.Close()
	os.Stdout = orig
	out, readErr := io.ReadAll(pr)
	_ = pr.Close()
	if runErr != nil {
		return "", runErr
	}
	if readErr != nil {
		return "", fmt.Errorf("capture stdout: %w", readErr)
	}
	return string(out), nil
}

// rerootGroupToWard rewrites a parsed guardfile's leading group token from the
// ward-kdl generator brand to `ward` (ward#270). A no-op if already `ward`.
func rerootGroupToWard(group []string) {
	if len(group) > 0 && group[0] == "ward-kdl" {
		group[0] = "ward"
	}
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
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return err
	}
	view, err := cl.viewIssue(ctx, owner, repo, number)
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
	flagQuery  = "query"
)

// ssmValueResolver fetches an SSM SecureString through ward's audited aws runner,
// parameterized by the guardfile-supplied path.
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
