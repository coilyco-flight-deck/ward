package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
	"github.com/urfave/cli/v3"
)

type fakeDirectorQueueClient struct {
	issues   map[string][]backlogIssue
	prs      map[string][]directorPullRequest
	comments map[string][]issueComment
}

func (f *fakeDirectorQueueClient) listOpenIssues(_ context.Context, owner, repo string, _ int) ([]backlogIssue, error) {
	return append([]backlogIssue(nil), f.issues[owner+"/"+repo]...), nil
}

func (f *fakeDirectorQueueClient) listOpenPullRequests(_ context.Context, owner, repo string, _ int) ([]directorPullRequest, error) {
	return append([]directorPullRequest(nil), f.prs[owner+"/"+repo]...), nil
}

func (f *fakeDirectorQueueClient) listIssueComments(_ context.Context, owner, repo string, number int) ([]issueComment, error) {
	key := owner + "/" + repo + "#" + strconv.Itoa(number)
	return append([]issueComment(nil), f.comments[key]...), nil
}

func TestDirectorQueueCommandWired(t *testing.T) {
	cmd := agentDirectorCommand()
	if !commandHasSubcommand(cmd, "queue") {
		t.Fatal("ward agent director missing queue subcommand")
	}
	if !commandHasSubcommandAlias(cmd, "status") {
		t.Fatal("ward agent director missing status alias on the queue subcommand")
	}
}

func TestDirectorQueueClassifiesRequestedStates(t *testing.T) {
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	ttl := time.Hour
	repo := "coilyco-flight-deck/ward"
	cases := []struct {
		name      string
		kind      string
		comments  []issueComment
		wantState string
		wantAct   string
	}{
		{
			name: "running reservation",
			comments: []issueComment{{
				Body:      reservationCommentBody(modeCodex, "engineer-codex-ward-1", "host", now.Add(-30*time.Minute), "", nil),
				CreatedAt: now.Add(-30 * time.Minute),
			}},
			wantState: directorQueueStateRunning,
			wantAct:   directorQueueActionWait,
		},
		{
			name: "stale reservation",
			comments: []issueComment{{
				Body:      reservationCommentBody(modeCodex, "engineer-codex-ward-1", "host", now.Add(-2*time.Hour), "", nil),
				CreatedAt: now.Add(-2 * time.Hour),
			}},
			wantState: directorQueueStateStale,
			wantAct:   directorQueueActionRedispatch,
		},
		{
			name: "redispatch marker",
			comments: []issueComment{{
				Body:      reservationReleaseCommentBody(modeCodex, "engineer-codex-ward-1", nil),
				CreatedAt: now.Add(-5 * time.Minute),
			}},
			wantState: directorQueueStateNeedsRedispatch,
			wantAct:   directorQueueActionRedispatch,
		},
		{
			name: "submitted pr",
			kind: backlogKindPullRequest,
			comments: []issueComment{{
				Body:      "WARD-OUTCOME: submitted - PR opened, waiting for human merge",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
			wantState: directorQueueStateSubmittedPR,
			wantAct:   directorQueueActionWait,
		},
		{
			name: "merge-ready pr",
			kind: backlogKindPullRequest,
			comments: []issueComment{{
				Body:      "WARD-OUTCOME: merge-ready - review passed, handoff to director",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
			wantState: directorQueueStateMergeReadyPR,
			wantAct:   directorQueueActionMergePR,
		},
		{
			name: "done issue",
			comments: []issueComment{{
				Body:      "WARD-OUTCOME: done ✅\n\n<details><summary>details</summary>\n\nmerged and pushed\n\n</details>",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
			wantState: directorQueueStateDoneOpen,
			wantAct:   directorQueueActionCloseDone,
		},
		{
			name: "done pr",
			kind: backlogKindPullRequest,
			comments: []issueComment{{
				Body:      "WARD-OUTCOME: done ✅\n\n<details><summary>details</summary>\n\nmerged and pushed\n\n</details>",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
			wantState: directorQueueStateDoneOpen,
			wantAct:   directorQueueActionWait,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := backlogIssue{Number: 1, Title: tc.name, Labels: []string{"P0"}}
			if tc.kind == backlogKindPullRequest {
				issue.Kind = backlogKindPullRequest
			}
			var got directorQueueItem
			if tc.kind == backlogKindPullRequest {
				got = classifyDirectorQueuePR(repo, directorPullRequest{Issue: dispatch.Issue{Number: 1, Title: tc.name, Labels: []string{"P0"}}}, tc.comments, now, ttl)
			} else {
				got = classifyDirectorQueueIssue(repo, issue, tc.comments, now, ttl)
			}
			if got.State != tc.wantState || got.Action != tc.wantAct {
				t.Fatalf("state/action = %q/%q, want %q/%q", got.State, got.Action, tc.wantState, tc.wantAct)
			}
			wantKind := backlogKindIssue
			if tc.kind == backlogKindPullRequest {
				wantKind = backlogKindPullRequest
			}
			if got.Kind != wantKind {
				t.Fatalf("Kind = %q, want canonical backlog kind %q", got.Kind, wantKind)
			}
		})
	}
}

func TestRenderDirectorQueueStatusShowsNextActions(t *testing.T) {
	repo := "coilyco-flight-deck/ward"
	now := time.Now().UTC()
	cl := &fakeDirectorQueueClient{
		issues: map[string][]backlogIssue{
			repo: {
				{Number: 1, Title: "stale reservation", Labels: []string{"P0"}},
				{Number: 2, Title: "redispatch", Labels: []string{"P1"}},
				{Number: 3, Title: "done issue", Labels: []string{"P2"}},
			},
		},
		prs: map[string][]directorPullRequest{
			repo: {
				{Issue: dispatch.Issue{Number: 4, Title: "submitted pr", Labels: []string{"P0"}}},
				{Issue: dispatch.Issue{Number: 5, Title: "merge-ready pr", Labels: []string{"P0"}}},
				{Issue: dispatch.Issue{Number: 6, Title: "done pr", Labels: []string{"P0"}}},
			},
		},
		comments: map[string][]issueComment{
			repo + "#1": {{
				Body:      reservationCommentBody(modeCodex, "engineer-codex-ward-1", "host", now.Add(-2*time.Hour), "", nil),
				CreatedAt: now.Add(-2 * time.Hour),
			}},
			repo + "#2": {{
				Body:      reservationReleaseCommentBody(modeCodex, "engineer-codex-ward-2", nil),
				CreatedAt: now.Add(-5 * time.Minute),
			}},
			repo + "#3": {{
				Body:      "WARD-OUTCOME: done ✅\n\n<details><summary>details</summary>\n\nmerged and pushed\n\n</details>",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
			repo + "#4": {{
				Body:      "WARD-OUTCOME: submitted - PR opened, waiting for human merge",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
			repo + "#5": {{
				Body:      "WARD-OUTCOME: merge-ready - review passed, handoff to director",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
			repo + "#6": {{
				Body:      "WARD-OUTCOME: done ✅\n\n<details><summary>details</summary>\n\nmerged\n\n</details>",
				CreatedAt: now.Add(-10 * time.Minute),
			}},
		},
	}
	out, err := renderDirectorQueueStatus(context.Background(), cl, []string{repo}, 50)
	if err != nil {
		t.Fatalf("renderDirectorQueueStatus: %v", err)
	}
	for _, want := range []string{
		"next actions:",
		directorQueueActionRedispatch,
		directorQueueActionMergePR,
		directorQueueActionCloseDone,
		directorQueueActionWait,
		"PR     " + repo + "#6",
		"issue  " + repo + "#3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered queue missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, directorQueueActionCloseDone+"=2") {
		t.Fatalf("done PR misclassified as closeable stale-open issue\n%s", out)
	}
}

func commandHasSubcommand(cmd *cli.Command, name string) bool {
	for _, sub := range cmd.Commands {
		if sub.Name == name {
			return true
		}
	}
	return false
}

func commandHasSubcommandAlias(cmd *cli.Command, alias string) bool {
	for _, sub := range cmd.Commands {
		for _, a := range sub.Aliases {
			if a == alias {
				return true
			}
		}
	}
	return false
}
