// Package contracts holds ward's internal kernel seams: the stable, private
// interfaces the cmd/ward implementation can code against without committing to
// a public API.
package contracts

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
	"github.com/coilyco-flight-deck/ward/internal/reviewpanel"
)

// Harness is the stable internal name for the agent/harness contract already
// implemented by internal/agentsapi.
type Harness interface {
	agentsapi.Agent
}

// Issue is the forge-neutral issue row shared by the tracker seams.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"html_url"`
	UpdatedAt time.Time `json:"updated_at"`
	Labels    []string
}

// IssueComment is one row of an issue thread.
type IssueComment struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// PullRequestContext carries the extra context ward attaches to a PR run.
type PullRequestContext struct {
	State        string
	Title        string
	Body         string
	URL          string
	HeadSHA      string
	HeadRef      string
	BaseRef      string
	Mergeability string
	RepairBucket string
	RepairNote   string
}

// SummaryLine renders the compact PR context summary used in logs and prompts.
func (pr PullRequestContext) SummaryLine() string {
	parts := []string{
		"source branch " + emptyDefault(pr.HeadRef, "(unknown)"),
		"base branch " + emptyDefault(pr.BaseRef, "(unknown)"),
		"mergeability " + emptyDefault(pr.Mergeability, "(unknown)"),
	}
	return strings.Join(parts, ", ")
}

// Branch is the branch projection the PR repair workflow reads.
type Branch struct {
	Name                string   `json:"name"`
	Protected           bool     `json:"protected"`
	EnableStatusCheck   bool     `json:"enable_status_check"`
	StatusCheckContexts []string `json:"status_check_contexts"`
	Commit              struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// CommitCombinedStatus is the combined-status projection the PR workflow reads.
type CommitCombinedStatus struct {
	State    string         `json:"state"`
	SHA      string         `json:"sha"`
	Total    int            `json:"total_count"`
	Statuses []CommitStatus `json:"statuses"`
}

// CommitStatus is one per-context status row.
type CommitStatus struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Status      string `json:"status"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
}

// EffectiveState reports the forge's per-context status field when present, else State.
func (s CommitStatus) EffectiveState() string {
	if v := strings.TrimSpace(s.State); v != "" {
		return v
	}
	return strings.TrimSpace(s.Status)
}

// PullRequest is the focused pull-request projection the workflow reads.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	Mergeable bool      `json:"mergeable"`
	Additions int       `json:"additions"`
	Deletions int       `json:"deletions"`
	HTMLURL   string    `json:"html_url"`
	UpdatedAt time.Time `json:"updated_at"`
	Head      struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// Ref renders the canonical PR ref for logs.
func (pr PullRequest) Ref(owner, repo string) string {
	if pr.Number <= 0 {
		return ""
	}
	return owner + "/" + repo + "#" + strconv.Itoa(pr.Number)
}

// HeadSHA returns the PR head SHA.
func (pr PullRequest) HeadSHA() string { return strings.TrimSpace(pr.Head.SHA) }

// ActionRun is one row of the repo Actions run feed.
type ActionRun struct {
	ID          int64     `json:"id"`
	IndexInRepo int64     `json:"index_in_repo"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	WorkflowID  string    `json:"workflow_id"`
	PrettyRef   string    `json:"prettyref"`
	CommitSHA   string    `json:"commit_sha"`
	Event       string    `json:"event"`
	HTMLURL     string    `json:"html_url"`
	Started     time.Time `json:"started"`
	Stopped     time.Time `json:"stopped"`
}

// Tracker is the forge-independent issue-thread surface for host dispatch and reaping.
type Tracker interface {
	GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error)
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error)
	CreateIssue(ctx context.Context, owner, repo, title, body string) (int, error)
	CommentIssue(ctx context.Context, owner, repo string, number int, body string) error
	DeleteIssueComment(ctx context.Context, owner, repo string, commentID int) error
	CloseIssue(ctx context.Context, owner, repo string, number int) error
	ReopenIssue(ctx context.Context, owner, repo string, number int) error
	LockIssue(ctx context.Context, owner, repo string, number int) error
	UnlockIssue(ctx context.Context, owner, repo string, number int) error
}

// PullRequestContextTracker is the narrow PR-ref seam shared by the issue and PR flows.
type PullRequestContextTracker interface {
	GetPullRequestContext(context.Context, string, string, int) (*PullRequestContext, error)
	ListPullRequestComments(context.Context, string, string, int) ([]IssueComment, error)
}

// PRRepairClassifier is the Forgejo-only PR repair seam.
type PRRepairClassifier interface {
	GetBranch(context.Context, string, string, string) (*Branch, error)
	GetCommitCombinedStatus(context.Context, string, string, string) (*CommitCombinedStatus, error)
	ListActionRuns(context.Context, string, string, int) ([]ActionRun, error)
}

// PRWorkflowClient is the native PR/CI workflow seam.
type PRWorkflowClient interface {
	GetPullRequest(context.Context, string, string, int) (*PullRequest, error)
	GetCommitCombinedStatus(context.Context, string, string, string) (*CommitCombinedStatus, error)
	PullRequestMerged(context.Context, string, string, int) (bool, error)
	ClosePullRequest(context.Context, string, string, int) error
	ReopenPullRequest(context.Context, string, string, int) error
	MergePullRequestWithHeadAndStyle(context.Context, string, string, int, string, string) error
	ListActionRuns(context.Context, string, string, int) ([]ActionRun, error)
	GetActionRun(context.Context, string, string, int64) (*ActionRun, error)
	RerunActionRun(context.Context, string, string, int64) error
	GetBranch(context.Context, string, string, string) (*Branch, error)
}

// ReviewService is the internal seam for the review panel.
type ReviewService interface {
	Execute(reviewpanel.Config) reviewpanel.PanelResult
}

func emptyDefault(s, fallback string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return fallback
}
