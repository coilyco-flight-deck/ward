package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// agent_director_queue.go wires `ward agent director queue` and its `status` alias.
// It is the read-only queue view for stale reservations and redispatch candidates.

const (
	directorQueueActionRedispatch     = "redispatch"
	directorQueueActionInspectLogs    = "inspect logs"
	directorQueueActionMergePR        = "merge PR"
	directorQueueActionCloseDone      = "close stale-open done issue"
	directorQueueActionWait           = "wait"
	directorQueueStateRunning         = "running/reserved and not stale"
	directorQueueStateStale           = "stale reservation"
	directorQueueStateNeedsRedispatch = "failed dispatch needing redispatch"
	directorQueueStateSubmittedPR     = "submitted PR awaiting merge/review"
	directorQueueStateMergeReadyPR    = "merge-ready PR awaiting merge"
	directorQueueStateDoneOpen        = "done-but-still-open"
	directorQueueStateFailedRun       = "failed run"
	directorQueueStateBlockedRun      = "blocked run"
)

type directorQueueClient interface {
	listOpenIssues(ctx context.Context, owner, repo string, limit int) ([]backlogIssue, error)
	listOpenPullRequests(ctx context.Context, owner, repo string, limit int) ([]directorPullRequest, error)
	listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error)
}

type directorQueueItem struct {
	Repo   string
	Number int
	// Kind is the canonical backlog kind (backlogKindIssue/backlogKindPullRequest),
	// never the display prefix - formatting derives the prefix at render time.
	Kind   string
	Tier   string
	Title  string
	State  string
	Action string
	Note   string
}

func directorQueueFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: director.default-scope from ~/.ward/config.yaml, else the cwd git origin)"},
		&cli.StringSliceFlag{Name: "org", Usage: "expand every repo an org owns into the scope (owner; repeatable), unioned with --repo and de-duped"},
		&cli.IntFlag{Name: "limit", Value: directorLimitDefault(), Usage: "open issues read per repo per refresh"},
	}
}

func agentDirectorQueueCommand() *cli.Command {
	return &cli.Command{
		Name:    "queue",
		Aliases: []string{"status"},
		Usage:   "Show the director queue/status view for stale reservations, redispatch candidates, and open PR handoffs.",
		Description: `queue reads the live open issue and pull request comments in scope and classifies each carry into one
of the durable operator states: running/reserved, stale reservation, failed dispatch needing redispatch,
submitted PR awaiting merge/review, merge-ready PR awaiting merge, done-but-still-open, blocked, or failed.
It prints the next operator action beside each carry so the operator does not have to grep hidden marker
names by hand. See docs/agent-director.md.`,
		Flags: directorQueueFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.runDirectorQueue(ctx, c)
		},
	}
}

func (r *Runner) runDirectorQueue(ctx context.Context, c *cli.Command) error {
	label := "ward agent director queue"
	repos, err := r.resolveDirectorScope(ctx, c, label)
	if err != nil {
		return err
	}
	if err := r.backlogTrustGate(label, repos); err != nil {
		return err
	}
	limit := c.Int("limit")
	if limit < 1 {
		limit = directorLimitDefault()
	}
	cl := r.hostForgejoClient(ctx)
	body, err := renderDirectorQueueStatus(ctx, cl, repos, limit)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return r.emit(body)
}

func renderDirectorQueueStatus(ctx context.Context, cl directorQueueClient, repos []string, limit int) (string, error) {
	items, err := collectDirectorQueueItems(ctx, cl, repos, limit, time.Now().UTC(), agentReservationTTL())
	if err != nil {
		return "", err
	}
	return formatDirectorQueueStatus(repos, items), nil
}

func collectDirectorQueueItems(ctx context.Context, cl directorQueueClient, repos []string, limit int, now time.Time, ttl time.Duration) ([]directorQueueItem, error) {
	items := make([]directorQueueItem, 0)
	for _, repo := range repos {
		owner, name, _ := strings.Cut(repo, "/")
		repoItems, err := collectDirectorQueueItemsForRepo(ctx, cl, repo, owner, name, limit, now, ttl)
		if err != nil {
			return nil, err
		}
		items = append(items, repoItems...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if ai, aj := directorQueueActionOrder(items[i].Action), directorQueueActionOrder(items[j].Action); ai != aj {
			return ai < aj
		}
		if items[i].Repo != items[j].Repo {
			return items[i].Repo < items[j].Repo
		}
		return items[i].Number < items[j].Number
	})

	return items, nil
}

func collectDirectorQueueItemsForRepo(ctx context.Context, cl directorQueueClient, repo, owner, name string, limit int, now time.Time, ttl time.Duration) ([]directorQueueItem, error) {
	issues, err := cl.listOpenIssues(ctx, owner, name, limit)
	if err != nil {
		return nil, fmt.Errorf("read open issues in %s: %w", repo, err)
	}
	prs, err := cl.listOpenPullRequests(ctx, owner, name, limit)
	if err != nil {
		return nil, fmt.Errorf("read open pull requests in %s: %w", repo, err)
	}
	items := make([]directorQueueItem, 0, len(issues)+len(prs))
	for _, issue := range issues {
		comments, cerr := cl.listIssueComments(ctx, owner, name, issue.Number)
		if cerr != nil {
			return nil, fmt.Errorf("read comments for %s#%d: %w", repo, issue.Number, cerr)
		}
		items = append(items, classifyDirectorQueueIssue(repo, issue, comments, now, ttl))
	}
	for _, pr := range prs {
		comments, cerr := cl.listIssueComments(ctx, owner, name, pr.Number)
		if cerr != nil {
			return nil, fmt.Errorf("read comments for %s#%d: %w", repo, pr.Number, cerr)
		}
		items = append(items, classifyDirectorQueuePR(repo, pr, comments, now, ttl))
	}
	return items, nil
}

func formatDirectorQueueStatus(repos []string, items []directorQueueItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "director queue (%s): %d repo(s), %d open carry(ies)\n", strings.Join(repos, ", "), len(repos), len(items))
	if len(items) == 0 {
		b.WriteString("\n  no open issue or pull-request carries in scope.\n")
		return b.String()
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Action]++
	}
	fmt.Fprintf(&b, "\n  next actions:")
	for _, action := range directorQueueActionSummaryOrder() {
		if n := counts[action]; n > 0 {
			fmt.Fprintf(&b, " %s=%d", action, n)
		}
	}
	b.WriteString("\n\n")
	currentAction := ""
	for _, item := range items {
		if item.Action != currentAction {
			currentAction = item.Action
			fmt.Fprintf(&b, "%s (%d)\n", currentAction, counts[currentAction])
		}
		fmt.Fprintf(&b, "  %-6s %s#%d [%-2s] %s - %s\n", backlogEntryKindPrefix(item.Kind), item.Repo, item.Number, tierOrDash(item.Tier), item.State, backlogTruncate(item.Title, 72))
		fmt.Fprintf(&b, "    next: %s\n", item.Action)
		if strings.TrimSpace(item.Note) != "" {
			fmt.Fprintf(&b, "    note: %s\n", item.Note)
		}
	}
	return b.String()
}

func classifyDirectorQueueIssue(repo string, issue backlogIssue, comments []issueComment, now time.Time, ttl time.Duration) directorQueueItem {
	item := directorQueueItem{
		Repo:   repo,
		Number: issue.Number,
		Kind:   backlogKindOf(issue.Kind),
		Tier:   backlogTierOf(issue.Labels),
		Title:  issue.Title,
	}
	sig := latestDirectorQueueSignal(comments)
	switch sig.Kind {
	case directorQueueSignalNone:
		item.State = "open / wait"
		item.Action = directorQueueActionWait
		return item
	case directorQueueSignalReservation:
		item.State = directorQueueStateRunning
		item.Action = directorQueueActionWait
		if !reservationFresh(sig.At, now, ttl) {
			item.State = directorQueueStateStale
			item.Action = directorQueueActionRedispatch
			item.Note = fmt.Sprintf("reservation at %s is older than the %s TTL", sig.At.UTC().Format(time.RFC3339), conciseDuration(ttl))
			return item
		}
		item.Note = reservationQueueNote(sig.Comment)
		return item
	case directorQueueSignalRedispatch:
		item.State = directorQueueStateNeedsRedispatch
		item.Action = directorQueueActionRedispatch
		item.Note = backlogTruncate(queueCommentSummary(sig.Comment.Body), 140)
		return item
	case directorQueueSignalOutcome:
		return classifyDirectorQueueOutcome(item, sig.Outcome, sig.Comment)
	default:
		item.State = "open / wait"
		item.Action = directorQueueActionWait
		return item
	}
}

func classifyDirectorQueuePR(repo string, pr directorPullRequest, comments []issueComment, now time.Time, ttl time.Duration) directorQueueItem {
	item := directorQueueItem{
		Repo:   repo,
		Number: pr.Number,
		Kind:   backlogKindPullRequest,
		Tier:   backlogTierOf(pr.Labels),
		Title:  pr.Title,
	}
	sig := latestDirectorQueueSignal(comments)
	switch sig.Kind {
	case directorQueueSignalNone:
		item.State = "open / wait"
		item.Action = directorQueueActionWait
		return item
	case directorQueueSignalReservation:
		item.State = directorQueueStateRunning
		item.Action = directorQueueActionWait
		if !reservationFresh(sig.At, now, ttl) {
			item.State = directorQueueStateStale
			item.Action = directorQueueActionRedispatch
			item.Note = fmt.Sprintf("reservation at %s is older than the %s TTL", sig.At.UTC().Format(time.RFC3339), conciseDuration(ttl))
			return item
		}
		item.Note = reservationQueueNote(sig.Comment)
		return item
	case directorQueueSignalRedispatch:
		item.State = directorQueueStateNeedsRedispatch
		item.Action = directorQueueActionRedispatch
		item.Note = backlogTruncate(queueCommentSummary(sig.Comment.Body), 140)
		return item
	case directorQueueSignalOutcome:
		return classifyDirectorQueueOutcome(item, sig.Outcome, sig.Comment)
	default:
		item.State = "open / wait"
		item.Action = directorQueueActionWait
		return item
	}
}

func classifyDirectorQueueOutcome(item directorQueueItem, outcome *backlogOutcome, comment issueComment) directorQueueItem {
	if outcome == nil {
		item.State = "open / wait"
		item.Action = directorQueueActionWait
		return item
	}
	switch backlogOutcomeState(outcome.Status) {
	case "done":
		item.State = directorQueueStateDoneOpen
		if item.Kind == backlogKindPullRequest {
			item.Action = directorQueueActionWait
		} else {
			item.Action = directorQueueActionCloseDone
		}
		item.Note = backlogTruncate(strings.TrimSpace(outcome.Text), 140)
	case "submitted":
		item.State = directorQueueStateSubmittedPR
		item.Action = directorQueueActionWait
		item.Note = backlogTruncate(strings.TrimSpace(outcome.Text), 140)
	case "merge-ready":
		item.State = directorQueueStateMergeReadyPR
		item.Action = directorQueueActionMergePR
		item.Note = backlogTruncate(strings.TrimSpace(outcome.Text), 140)
	case "blocked":
		item.State = directorQueueStateBlockedRun
		item.Action = directorQueueActionInspectLogs
		item.Note = backlogTruncate(strings.TrimSpace(outcome.Text), 140)
	case "failed":
		item.State = directorQueueStateFailedRun
		item.Action = directorQueueActionInspectLogs
		item.Note = backlogTruncate(strings.TrimSpace(outcome.Text), 140)
	default:
		item.State = "open / wait"
		item.Action = directorQueueActionWait
		item.Note = backlogTruncate(strings.TrimSpace(outcome.Text), 140)
	}
	if item.Note == "" {
		item.Note = queueCommentSummary(comment.Body)
	}
	return item
}

type directorQueueSignalKind string

const (
	directorQueueSignalNone        directorQueueSignalKind = ""
	directorQueueSignalReservation directorQueueSignalKind = "reservation"
	directorQueueSignalRedispatch  directorQueueSignalKind = "redispatch"
	directorQueueSignalOutcome     directorQueueSignalKind = "outcome"
)

type directorQueueSignal struct {
	Kind    directorQueueSignalKind
	At      time.Time
	Comment issueComment
	Outcome *backlogOutcome
}

func latestDirectorQueueSignal(comments []issueComment) directorQueueSignal {
	var sig directorQueueSignal
	for _, c := range comments {
		switch {
		// The needs-redispatch marker alone is the signal: a reservation-collision
		// deferral carries it without a release marker (ward#1149).
		case strings.Contains(c.Body, agentNeedsRedispatchMarker):
			if c.CreatedAt.After(sig.At) {
				sig = directorQueueSignal{Kind: directorQueueSignalRedispatch, At: c.CreatedAt, Comment: c}
			}
		case strings.Contains(c.Body, agentReservationMarker) && !strings.Contains(c.Body, agentReservationReleaseMarker):
			if c.CreatedAt.After(sig.At) {
				sig = directorQueueSignal{Kind: directorQueueSignalReservation, At: c.CreatedAt, Comment: c}
			}
		default:
			if outcome, ok := backlogOutcomeOfComment(c.Body); ok && c.CreatedAt.After(sig.At) {
				sig = directorQueueSignal{Kind: directorQueueSignalOutcome, At: c.CreatedAt, Comment: c, Outcome: &outcome}
			}
		}
	}
	return sig
}

func reservationQueueNote(comment issueComment) string {
	author := strings.TrimSpace(comment.User.Login)
	if author == "" {
		author = "another run"
	}
	return fmt.Sprintf("by @%s at %s", author, comment.CreatedAt.UTC().Format(time.RFC3339))
}

func queueCommentSummary(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for _, ln := range lines {
		s := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), ">*-•# "))
		if s == "" || strings.HasPrefix(s, "<!--") || strings.HasPrefix(strings.ToLower(s), "<details") {
			continue
		}
		if s != "" {
			return s
		}
	}
	return ""
}

func directorQueueActionOrder(action string) int {
	for i, a := range directorQueueActionSummaryOrder() {
		if a == action {
			return i
		}
	}
	return len(directorQueueActionSummaryOrder())
}

func directorQueueActionSummaryOrder() []string {
	return []string{
		directorQueueActionRedispatch,
		directorQueueActionInspectLogs,
		directorQueueActionMergePR,
		directorQueueActionCloseDone,
		directorQueueActionWait,
	}
}
