package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_director.go is `ward agent director`, the attached read-only supervision
// surface. Repetition and dispatch judgment belong to the harness-native goal loop.

type directorConfig struct {
	mode          containerMode
	print         bool
	image         string
	tag           string
	wardVersion   string
	versionSource string
	contextBundle string
	wardSource    string
	noPull        bool
	withRepo      []string
}

// directorFlags only configures the retained attached supervision surface and its
// one live startup snapshot. It intentionally exposes no polling or dispatch knobs.
func directorFlags() []cli.Flag {
	flags := agentHarnessFlags()
	flags = append(flags,
		&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: director.default-scope from ~/.ward/config.yaml)"},
		&cli.StringSliceFlag{Name: "org", Usage: "expand every repo an org owns into the scope (owner; repeatable), unioned with --repo and de-duped"},
		&cli.StringSliceFlag{Name: "with-repo", Usage: "clone an additional read-only context repo into the director surface (owner/name; repeatable), landed at /workspace/<owner>/<repo>."},
		&cli.IntFlag{Name: "limit", Value: directorLimitDefault(), Usage: "open issues read per repo for the startup snapshot"},
	)
	flags = append(flags, agentImageFlags()...)
	return append(flags,
		&cli.BoolFlag{Name: "print", Usage: "render the live snapshot and resolved read-only surface plan, then exit without launching"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	)
}

func agentDirectorCommand() *cli.Command {
	return &cli.Command{
		Name:      "director",
		Usage:     "Open the attached read-only director surface for a repo or one exact issue ref.",
		ArgsUsage: "(issue ref | scope via --repo; default: director.default-scope from ~/.ward/config.yaml)",
		Description: `director reads one live queue snapshot, prints it, and opens an attached
read-only director session. The director can inspect lifecycle state and use Ward's
brokered primitives, but Ward does not poll, rank, choose, or redispatch work. A
harness-native goal owns repetition and judgment.

When a single issue ref or Forgejo issue URL is given, director validates and
renders only that exact open issue before opening the repository surface.

  warded director --repo coilyco-flight-deck/ward
  warded director coilyco-flight-deck/ward#988
  warded director --org coilyco-flight-deck
  warded director --print --repo coilyco-flight-deck/ward

It is attached and interactive only. There is no detach or autonomous loop. See
docs/agent-director.md.`,
		Flags:    directorFlags(),
		Commands: []*cli.Command{agentDirectorQueueCommand(), agentDirectorMergeCommand()},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentHarness(c)
			if err != nil {
				return fmt.Errorf("ward agent director: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".director",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runAgentDirector(ctx, cmd, mode) },
			}, r.Audit)(ctx, c)
		},
	}
}

type resolvedDirectorIssue struct {
	Ref      agentIssueRef
	Issue    *Issue
	Comments []issueComment
}

func (r *Runner) runAgentDirector(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "director")
	var (
		repos  []string
		target *resolvedDirectorIssue
		err    error
	)
	if arg := strings.TrimSpace(c.Args().First()); arg != "" {
		resolved, resolveErr := r.resolveDirectorIssue(ctx, label, mode, arg)
		if resolveErr != nil {
			return resolveErr
		}
		target = &resolved
		repos = []string{resolved.Ref.repoSlug()}
	} else {
		repos, err = r.resolveDirectorScope(ctx, c, label)
		if err != nil {
			return err
		}
	}
	if err := r.directorTrustGate(label, repos); err != nil {
		return err
	}
	limit := c.Int("limit")
	if limit < 1 {
		limit = directorLimitDefault()
	}
	if err := r.renderDirectorStartupSnapshot(ctx, repos, target, limit); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	cfg := directorConfig{
		mode:          mode,
		print:         c.Bool("print"),
		image:         c.String("image"),
		tag:           c.String("tag"),
		wardVersion:   strings.TrimSpace(c.String("ward-version")),
		versionSource: resolveWardVersionSource(c, c.String("ward-version")),
		contextBundle: strings.TrimSpace(c.String("context-bundle")),
		wardSource:    strings.TrimSpace(c.String("ward-source")),
		noPull:        c.Bool("no-pull"),
		withRepo:      c.StringSlice("with-repo"),
	}
	return r.directorSurface(ctx, label, repos[0], cfg)
}

func (r *Runner) resolveDirectorIssue(ctx context.Context, label string, mode containerMode, arg string) (resolvedDirectorIssue, error) {
	ref, err := r.resolveAgentIssueRef(ctx, arg)
	if err != nil {
		return resolvedDirectorIssue{}, fmt.Errorf("%s: %w", label, err)
	}
	if !r.ownerAllowed(ref.Owner) {
		return resolvedDirectorIssue{}, r.untrustedOwnerErr(label, ref.Owner)
	}
	issue, err := r.fetchIssueByForge(ctx, label, ref.Forge, mode, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return resolvedDirectorIssue{}, fmt.Errorf("%s: resolve issue %s: %w", label, ref, err)
	}
	if err := validateDirectorIssueTarget(label, ref, issue); err != nil {
		return resolvedDirectorIssue{}, err
	}
	comments, err := r.fetchIssueComments(ctx, ref)
	if err != nil {
		return resolvedDirectorIssue{}, fmt.Errorf("%s: resolve issue %s comments: %w", label, ref, err)
	}
	target, err := approvalTargetFromIssue(ref, issue)
	if err != nil {
		return resolvedDirectorIssue{}, fmt.Errorf("%s: %w", label, err)
	}
	if _, err := admitActorContent(target, comments, loadActorAuthorityPolicy()); err != nil {
		return resolvedDirectorIssue{}, fmt.Errorf("%s: %w", label, err)
	}
	return resolvedDirectorIssue{Ref: ref, Issue: issue, Comments: comments}, nil
}

func validateDirectorIssueTarget(label string, ref agentIssueRef, issue *Issue) error {
	if state := strings.ToLower(strings.TrimSpace(issue.State)); state != "open" {
		return dispatchDeclineErr(dispatchIssueClosed, "issue-closed",
			"%s: issue %s is %s, not open - nothing to do", label, ref, emptyDefault(state, "unknown"))
	}
	return nil
}

func (r *Runner) renderDirectorStartupSnapshot(ctx context.Context, repos []string, target *resolvedDirectorIssue, limit int) error {
	if target != nil {
		issue := target.Issue
		item := classifyDirectorQueueIssue(target.Ref.repoSlug(), backlogIssue{
			Number: issue.Number,
			Kind:   backlogKindIssue,
			Author: issue.User.Login,
			Title:  issue.Title,
			Body:   issue.Body,
			Labels: append([]string(nil), issue.Labels...),
			URL:    issue.URL,
		}, target.Comments, false, time.Now().UTC(), agentReservationTTL())
		return r.emit(formatDirectorQueueStatus(repos, []directorQueueItem{item}))
	}
	body, err := renderDirectorQueueStatus(ctx, r.hostForgejoClient(ctx), repos, limit)
	if err != nil {
		return err
	}
	return r.emit(body)
}

func (r *Runner) directorSurface(ctx context.Context, label, contextRepo string, cfg directorConfig) error {
	if !cfg.print && !terminalAttached() {
		return fmt.Errorf("%s: the director surface requires an attached terminal (use --print for a no-launch preview)", label)
	}
	if err := directorSurfaceCommand().Run(ctx, directorSurfaceArgv(contextRepo, cfg)); err != nil {
		return fmt.Errorf("%s: interactive surface session: %w", label, err)
	}
	return nil
}

func directorSurfaceArgv(contextRepo string, cfg directorConfig) []string {
	argv := []string{directorSurfaceVerb, "--repo", contextRepo, "--harness", string(cfg.mode)}
	if value := strings.TrimSpace(cfg.image); value != "" {
		argv = append(argv, "--image", value)
	}
	if value := strings.TrimSpace(cfg.tag); value != "" {
		argv = append(argv, "--tag", value)
	}
	if cfg.versionSource == wardVersionSourceExplicit {
		if value := strings.TrimSpace(cfg.wardVersion); value != "" {
			argv = append(argv, "--ward-version", value)
		}
	}
	if value := strings.TrimSpace(cfg.wardSource); value != "" {
		argv = append(argv, "--ward-source", value)
	}
	if value := strings.TrimSpace(cfg.contextBundle); value != "" {
		argv = append(argv, "--context-bundle", value)
	}
	if cfg.noPull {
		argv = append(argv, "--no-pull")
	}
	if cfg.print {
		argv = append(argv, "--print")
	}
	for _, repo := range cfg.withRepo {
		if value := strings.TrimSpace(repo); value != "" {
			argv = append(argv, "--with-repo", value)
		}
	}
	return argv
}

func (r *Runner) directorTrustGate(label string, repos []string) error {
	for _, slug := range repos {
		owner, name, ok := strings.Cut(slug, "/")
		if !ok || owner == "" || name == "" {
			return fmt.Errorf("%s: invalid repo %q in scope (want owner/name)", label, slug)
		}
		if !r.ownerAllowed(owner) {
			return r.untrustedOwnerErr(label, owner)
		}
	}
	return nil
}

func (r *Runner) out() io.Writer {
	if r.Runner != nil && r.Runner.Stdout != nil {
		return r.Runner.Stdout
	}
	return os.Stdout
}

func (r *Runner) emit(value string) error {
	_, err := io.WriteString(r.out(), value)
	return err
}
