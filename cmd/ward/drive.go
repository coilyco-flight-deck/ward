package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// drive.go wires `ward drive <harness> "<prompt>"`: drive a named harness one-shot
// in a guarded container. `warded` is the public-face shim (ward#247). See docs/drive.md.

// driveLabel renders the audited command line for a drive run.
func driveLabel(mode containerMode) string { return "ward drive " + string(mode) }

// driveCommand builds `ward drive <harness> [prompt...]`: harness positional, the
// rest the prompt. Reuses the one-shot attached path `ask` rides. See docs/drive.md.
func driveCommand() *cli.Command {
	return &cli.Command{
		Name: "drive",
		Usage: "Drive a harness one-shot inside a fresh guarded container, streaming the output inline " +
			"(`warded <harness> \"...\"` is the public-face shim over this; ward#247).",
		ArgsUsage: "<" + agentDriverChoices() + "> <prompt>",
		Description: `drive is the canonical machinery behind the warded agent: it spins up a
fresh, least-access ephemeral container, fresh-clones the context repo inside
it, and runs the named harness one-shot against your prompt - every command the
agent issues is gated by cli-guard policy and written to the audit log. The
boundary is the product: drive is how you watch an agent run bounded.

  ward drive claude "summarize how the audit log is written"
  warded claude "summarize how the audit log is written"     # the public face
  ward drive codex "what does exec_gate.go enforce?" --repo coilyco-flight-deck/ward
  warded claude "..." --print                                 # show the plan, run nothing

The harness is the first positional; everything after it is the prompt. Off-org
repos are refused. drive is one-shot and attached - the container is thrown away
on exit - so to carry a Forgejo issue to merge, reach for 'ward agent work'.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "repo", Usage: "owner/repo to clone for context (default: inferred from the cwd's git origin)"},
			&cli.StringSliceFlag{Name: "with-repo", Usage: "clone an additional repo for context (owner/name; repeatable), landed under /workspace alongside the primary repo (ward#230)."},
			&cli.StringFlag{Name: "image", Value: containerImageDefault, Usage: "dev-base image to run"},
			&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Usage: "image tag"},
			&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
			&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
			&cli.BoolFlag{Name: "print", Usage: "resolve the harness + repo + prompt + docker plan and exit; clone nothing, run nothing"},
			&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, prompt, err := parseDriveArgs(c.Args().Slice())
			if err != nil {
				return fmt.Errorf("ward drive: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "drive." + string(mode),
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runDrive(ctx, cmd, mode, prompt)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// parseDriveArgs splits the tail into the harness (first token) and the prompt (the
// rest, joined). Both required; an unknown harness is a hard error naming the choices.
func parseDriveArgs(args []string) (containerMode, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("no harness: pass one of %s, e.g. `warded claude \"summarize how X works\"`", agentDriverChoices())
	}
	mode, err := parseMode(args[0])
	if err != nil {
		return "", "", fmt.Errorf("invalid harness %q: want %s", args[0], agentDriverChoices())
	}
	prompt := strings.TrimSpace(strings.Join(args[1:], " "))
	if prompt == "" {
		return "", "", fmt.Errorf("no prompt: pass it after the harness, e.g. `warded %s \"summarize how X works\"`", mode)
	}
	return mode, prompt, nil
}

// runDrive resolves the context repo, seeds the prompt, and runs the harness one-shot
// in a fresh attached container - runAgentAsk's plumbing (WARD_ASK) under a drive name.
func (r *Runner) runDrive(ctx context.Context, c *cli.Command, mode containerMode, prompt string) error {
	label := driveLabel(mode)

	// The context repo is --repo, else inferred from the cwd's git origin - the
	// same resolution `ward container up` and `ward agent ask` use.
	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: drive spins a bypassPermissions container and clones the repo, so
	// only act on an owner in the primary-org set - the same gate work/ask apply.
	if !r.ownerAllowed(repo.Owner) {
		return fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, repo.Owner, strings.Join(r.primaryOrgs(), ", "))
	}

	seed := drivePrompt(prompt)

	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return err
	}
	// drive always runs attached and ephemeral, so its assets clean up on return.
	defer cleanupAssets()

	plan, err := buildUpPlan(c, repo, mode, cwd, assetsDir, []string{seed})
	if err != nil {
		return err
	}
	// drive shares ask's one-shot attached branch (WARD_ASK=1): the seed, not the
	// env, decides whether the run is read-only.
	plan.Ask = true
	// Name it ward-<repo>-drive-<mode>-<rand> so `docker ps` tells a drive run apart
	// from an ask run, a carry run, or a bare `container up`.
	plan.Name = fmt.Sprintf("%s-%s-drive-%s-%s", containerNamePrefix, safeRepoName(repo), mode, randHex())

	if c.Bool("print") {
		return printDrivePlan(c, plan, prompt, seed)
	}

	// drive is interactive (you watch the run), so this dispatch is the moment to
	// surface a stale-ward reminder before the container spins (ward#143).
	r.maybeWarnWardOutdated(ctx)

	// Reclaim dead containers' writable layers before adding one more, so a busy
	// fleet can't exhaust the docker disk and wedge new launches (ward#272).
	r.sweepStaleContainers(ctx)
	if !c.Bool("no-pull") {
		if perr := r.Runner.Exec(ctx, "docker", "pull", plan.Image); perr != nil {
			fmt.Fprintf(os.Stderr, "%s: image pull failed (%v); trying the local image\n", label, perr)
		}
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, r.resolveAgentCreds(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()
	fmt.Fprintf(os.Stderr, "%s: driving %s in a fresh guarded container against %s...\n\n", label, mode.agentBinary(), repo.slug())
	return r.Runner.Exec(ctx, "docker", dockerCreateArgv(plan, envFile)...)
}

// drivePrompt light-wraps the operator's prompt so the in-container harness names
// the boundary it runs behind, answers/works inline, and stays one-shot. Pure.
func drivePrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "(no prompt given)"
	}
	return fmt.Sprintf(
		"You are a warded agent: you run inside a fresh, ephemeral, least-access ward container, and "+
			"every command you issue is gated by cli-guard policy and written to an append-only audit log. "+
			"The boundary is the point - a denied command is the product working, not a failure to route "+
			"around.\n\n"+
			"This is a one-shot drive: carry out the prompt below using the fresh clone of this repo and the "+
			"usual operating context (read the code, run guarded commands, search as needed). Your output "+
			"streams straight to a terminal for a human to read inline, so write it in clean text or "+
			"GitHub-flavored markdown - no preamble like \"here is my answer\", no sign-off. The container is "+
			"thrown away when you exit, so do not assume work persists: if the prompt needs a change carried "+
			"to a branch and merged, say so and point the operator at `ward agent work`.\n\n"+
			"----- the prompt -----\n%s\n----- end prompt -----",
		prompt)
}

// printDrivePlan renders the resolved repo, the prompt, the seeded prompt, and the
// docker plan without cloning or firing - the dry-run preview for drive.
func printDrivePlan(c *cli.Command, p upPlan, prompt, seed string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print)\n", driveLabel(p.Mode))
	fmt.Fprintf(&b, "drive: harness runs one-shot, attached, in a fresh ephemeral container\n")
	fmt.Fprintf(&b, "repo:   %s\n", p.Repo.slug())
	fmt.Fprintf(&b, "name:   %s\n", p.Name)
	fmt.Fprintf(&b, "----- prompt -----\n%s\n----- end -----\n", prompt)
	fmt.Fprintf(&b, "----- seeded prompt -----\n%s\n----- end -----\n", seed)
	if c.Bool("no-pull") {
		fmt.Fprintf(&b, "# pull skipped (--no-pull); image: %s\n", p.Image)
	} else {
		fmt.Fprintf(&b, "docker pull %s\n", p.Image)
	}
	fmt.Fprintf(&b, "docker %s\n", strings.Join(dockerCreateArgv(p, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, b.String())
	return err
}
