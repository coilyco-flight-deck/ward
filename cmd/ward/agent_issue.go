package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"github.com/urfave/cli/v3"
)

// agent_issue.go wires non-dispatch issue creation for read-only directors.
// Compose uses the sibling broker; older containers retain the Unix path.

func agentIssueCommand() *cli.Command {
	return &cli.Command{
		Name:  "issue",
		Usage: "File issues through the read-only director credential broker; launches nothing.",
		Commands: []*cli.Command{
			agentIssueCreateCommand(),
			agentIssueApproveCommand(),
		},
	}
}

func agentIssueCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "Create a Forgejo issue through the credential broker without dispatching an agent.",
		ArgsUsage: "<owner/repo>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title", Required: true, Usage: "issue title"},
			&cli.StringFlag{Name: "body-file", Required: true, Usage: "path to markdown issue body"},
		},
		Action: agentIssueCreateAction(),
	}
}

func agentIssueCreateAction() cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		return r.WrapVerb(verb.Spec{
			Name:       "agent.issue.create",
			SkipPolicy: true,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return r.runAgentIssueCreate(ctx, cmd)
			},
		}, r.Audit)(ctx, c)
	}
}

func (r *Runner) runAgentIssueCreate(ctx context.Context, c *cli.Command) error {
	const label = "ward agent issue create"
	repoArg := strings.TrimSpace(c.Args().First())
	if repoArg == "" {
		return fmt.Errorf("%s: missing repo: pass owner/repo", label)
	}
	if c.Args().Len() > 1 {
		return fmt.Errorf("%s: got extra arguments %q; pass exactly one owner/repo", label, strings.Join(c.Args().Slice()[1:], " "))
	}
	repo, err := parseRepoRef(repoArg)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	title := strings.TrimSpace(c.String("title"))
	if title == "" {
		return fmt.Errorf("%s: --title is empty", label)
	}
	body, err := issueCreateBody(c.String("body-file"))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	res, err := r.brokerFileIssue(ctx, repo, title, body)
	if err != nil {
		return fmt.Errorf("%s: %w", label, classifyBrokerIssueCreateError(err))
	}
	if res.Number <= 0 {
		return fmt.Errorf("%s: Forgejo/API failure: broker did not return a created issue number", label)
	}
	url := strings.TrimSpace(res.URL)
	if url == "" {
		url = (agentIssueRef{Owner: repo.Owner, Repo: repo.Name, Number: res.Number, Tracker: trackerForgejo}).url()
	}
	var out io.Writer = os.Stdout
	if r != nil && r.Runner != nil && r.Runner.Stdout != nil {
		out = r.Runner.Stdout
	}
	printCreatedIssue(out, repo, res.Number, url)
	return nil
}

func issueCreateBody(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("--body-file is empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read --body-file %q: %w", path, err)
	}
	body := strings.TrimSpace(string(b))
	if body == "" {
		return "", fmt.Errorf("--body-file %q is empty", path)
	}
	return body, nil
}

func (r *Runner) brokerFileIssue(ctx context.Context, repo targetRepo, title, body string) (broker.Result, error) {
	if nativeForgejoBrokerEnabled() {
		number, err := r.hostForgejoClient(ctx).CreateIssue(ctx, repo.Owner, repo.Name, title, body)
		return broker.Result{Number: number}, err
	}
	session, ok := newBrokerSession()
	if !ok {
		return broker.Result{}, errBrokerMissingAccess
	}
	return session.do(ctx, broker.Request{
		Op:     broker.OpFileIssue,
		Target: broker.Target{Owner: repo.Owner, Repo: repo.Name},
		Title:  title,
		Body:   body,
	})
}

var errBrokerMissingAccess = errors.New("missing broker access")

func classifyBrokerIssueCreateError(err error) error {
	switch {
	case errors.Is(err, errBrokerMissingAccess):
		return fmt.Errorf("%w: %s is unset; run from a read-only director surface with the credential broker attached", errBrokerMissingAccess, envBrokerSocket)
	case errors.Is(err, errBrokerUnreachable):
		return fmt.Errorf("%w: %w", errBrokerMissingAccess, err)
	case errors.Is(err, errBrokerOutOfTier):
		return fmt.Errorf("broker refusal: %w", err)
	default:
		return fmt.Errorf("Forgejo/API failure: %w", err)
	}
}

func printCreatedIssue(w io.Writer, repo targetRepo, number int, url string) {
	writef(w, "issue: %s#%d\n", repo.slug(), number)
	writef(w, "url: %s\n", url)
}
