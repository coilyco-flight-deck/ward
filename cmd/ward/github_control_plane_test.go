package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/coilyco-flight-deck/ward/internal/contracts"
)

func TestGitHubControlPlaneIssueThreadContract(t *testing.T) {
	client, callsLog, bodiesLog := newGitHubControlPlaneClient(t)
	ctx := context.Background()

	issue, err := client.GetIssue(ctx, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.State != "open" || issue.Number != 7 || issue.Title != "ship the fix" {
		t.Fatalf("GetIssue = %+v, want open issue 7", issue)
	}
	if got := strings.Join(issue.Labels, ","); got != "P0,github" {
		t.Fatalf("GetIssue labels = %q, want P0,github", got)
	}

	comments, err := client.ListIssueComments(ctx, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(comments) != 2 || comments[0].User.Login != "alice" || comments[1].User.Login != "bob" {
		t.Fatalf("ListIssueComments = %+v, want two comments oldest-first", comments)
	}
	if comments[1].CreatedAt.IsZero() {
		t.Fatalf("ListIssueComments should parse RFC3339 timestamps")
	}

	if num, err := client.CreateIssue(ctx, "coilyco-flight-deck", "ward", "new issue", "plain body"); err != nil || num != 42 {
		t.Fatalf("CreateIssue = %d,%v, want 42,nil", num, err)
	}
	if err := client.CommentIssue(ctx, "coilyco-flight-deck", "ward", 7, "follow-up"); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	if err := client.DeleteIssueComment(ctx, "coilyco-flight-deck", "ward", 99); err != nil {
		t.Fatalf("DeleteIssueComment: %v", err)
	}
	if err := client.CloseIssue(ctx, "coilyco-flight-deck", "ward", 7); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if err := client.ReopenIssue(ctx, "coilyco-flight-deck", "ward", 7); err != nil {
		t.Fatalf("ReopenIssue: %v", err)
	}
	if err := client.LockIssue(ctx, "coilyco-flight-deck", "ward", 7); err != nil {
		t.Fatalf("LockIssue: %v", err)
	}
	if err := client.UnlockIssue(ctx, "coilyco-flight-deck", "ward", 7); err != nil {
		t.Fatalf("UnlockIssue: %v", err)
	}

	pr, err := client.GetPullRequestContext(ctx, "coilyco-flight-deck", "ward", 9)
	if err != nil {
		t.Fatalf("GetPullRequestContext: %v", err)
	}
	if pr.State != "open" || pr.Title != "carry the fix" || pr.BaseRef != "main" {
		t.Fatalf("GetPullRequestContext = %+v", pr)
	}
	if !strings.Contains(pr.Mergeability, "mergeable=true") || !strings.Contains(pr.Mergeability, "draft=true") {
		t.Fatalf("GetPullRequestContext mergeability = %q, want mergeable+draft markers", pr.Mergeability)
	}

	prComments, err := client.ListPullRequestComments(ctx, "coilyco-flight-deck", "ward", 9)
	if err != nil {
		t.Fatalf("ListPullRequestComments: %v", err)
	}
	if len(prComments) != 2 || prComments[0].Body != "first PR note" || prComments[1].Body != "second PR note" {
		t.Fatalf("ListPullRequestComments = %+v", prComments)
	}

	gotCalls, err := os.ReadFile(callsLog)
	if err != nil {
		t.Fatalf("read calls log: %v", err)
	}
	for _, want := range []string{
		"api /repos/coilyco-flight-deck/ward/issues/7",
		"issue create --repo coilyco-flight-deck/ward --title new issue --body-file",
		"issue comment 7 --repo coilyco-flight-deck/ward --body-file",
		"api -X PATCH /repos/coilyco-flight-deck/ward/issues/7 -f state=closed",
		"api -X PATCH /repos/coilyco-flight-deck/ward/issues/7 -f state=open",
		"api -X PUT /repos/coilyco-flight-deck/ward/issues/7/lock",
		"api -X DELETE /repos/coilyco-flight-deck/ward/issues/7/lock",
		"api /repos/coilyco-flight-deck/ward/pulls/9",
		"api --paginate -f per_page=100 /repos/coilyco-flight-deck/ward/issues/9/comments",
	} {
		if !strings.Contains(string(gotCalls), want) {
			t.Fatalf("calls log missing %q\n%s", want, gotCalls)
		}
	}

	gotBodies, err := os.ReadFile(bodiesLog)
	if err != nil {
		t.Fatalf("read bodies log: %v", err)
	}
	if !strings.Contains(string(gotBodies), "plain body") || !strings.Contains(string(gotBodies), "follow-up") {
		t.Fatalf("body-file log = %q, want issue/comment payloads", gotBodies)
	}
}

func TestGitHubHumanInterventionBlockMirrorsIssueAndPRThreads(t *testing.T) {
	client, _, bodiesLog := newGitHubControlPlaneClient(t)
	reason := "human comment by @alice at 2026-07-10T09:30:00Z is newer than the latest ward acknowledgement at 2026-07-10T09:00:00Z"
	reportHumanInterventionBlock(context.Background(), "coilyco-flight-deck", "ward", 7, 9, reason, client, client)

	got, err := os.ReadFile(bodiesLog)
	if err != nil {
		t.Fatalf("read bodies log: %v", err)
	}
	body := string(got)
	if count := strings.Count(body, "WARD-WORKFLOW: blocked"); count != 2 {
		t.Fatalf("blocked-comment count = %d, want 2\n%s", count, body)
	}
	if !strings.Contains(body, "human comment by @alice") || !strings.Contains(body, "This action is blocked until the feedback is visibly acknowledged.") {
		t.Fatalf("blocked comment body = %q", body)
	}
}

func TestGitHubControlPlaneGapsAreExplicit(t *testing.T) {
	var client any = &githubClient{}
	if _, ok := client.(contracts.PRWorkflowClient); ok {
		t.Fatal("githubClient must not claim Forgejo-native PR workflow support")
	}
	if _, ok := client.(repoIssueScanner); ok {
		t.Fatal("githubClient must not claim issue-scan support for open-PR backpressure")
	}
}

func newGitHubControlPlaneClient(t *testing.T) (*githubClient, string, string) {
	t.Helper()
	dir := t.TempDir()
	callsLog := filepath.Join(dir, "calls.log")
	bodiesLog := filepath.Join(dir, "bodies.log")
	stub := filepath.Join(dir, "gh")

	script := fmt.Sprintf(`#!/bin/sh
set -eu
printf '%%s\n' "$*" >> %[1]q
bodyfile=""
for arg in "$@"; do
	bodyfile="$arg"
done
cmd="${1:-}"
case "$cmd" in
	api)
		method="GET"
		path=""
		shift
		while [ "$#" -gt 0 ]; do
			case "$1" in
				-X)
					method="$2"
					shift 2
					;;
				--paginate)
					shift
					;;
				-f)
					shift 2
					;;
				*)
					path="$1"
					shift
					;;
			esac
		done
		case "$method:$path" in
			GET:/repos/coilyco-flight-deck/ward/issues/7)
				cat <<'JSON'
{"number":7,"title":"ship the fix","body":"issue body","state":"OPEN","html_url":"https://github.com/coilyco-flight-deck/ward/issues/7","labels":[{"name":"P0"},{"name":"github"}]}
JSON
				;;
			GET:/repos/coilyco-flight-deck/ward/issues/7/comments)
				cat <<'JSON'
[{"id":11,"body":"first note","created_at":"2026-07-10T09:00:00Z","user":{"login":"alice"}},{"id":12,"body":"second note","created_at":"2026-07-10T09:10:00Z","user":{"login":"bob"}}]
JSON
				;;
			GET:/repos/coilyco-flight-deck/ward/issues/9/comments)
				cat <<'JSON'
[{"id":21,"body":"first PR note","created_at":"2026-07-10T09:00:00Z","user":{"login":"alice"}},{"id":22,"body":"second PR note","created_at":"2026-07-10T09:10:00Z","user":{"login":"bob"}}]
JSON
				;;
			GET:/repos/coilyco-flight-deck/ward/pulls/9)
				cat <<'JSON'
{"number":9,"title":"carry the fix","body":"closes #7","state":"open","html_url":"https://github.com/coilyco-flight-deck/ward/pull/9","draft":true,"mergeable":true,"head":{"sha":"headsha","ref":"feature/github"},"base":{"ref":"main"}}
JSON
				;;
			PATCH:/repos/coilyco-flight-deck/ward/issues/7)
				:
				;;
			PUT:/repos/coilyco-flight-deck/ward/issues/7/lock)
				:
				;;
			DELETE:/repos/coilyco-flight-deck/ward/issues/7/lock)
				:
				;;
			DELETE:/repos/coilyco-flight-deck/ward/issues/comments/99)
				:
				;;
				*)
					printf 'unexpected gh api call: %%s %%s\n' "$method" "$path" >&2
				exit 1
				;;
		esac
		;;
	issue)
		sub="${2:-}"
		case "$sub" in
			create)
				cat "$bodyfile" >> %[2]q
				printf '\n---\n' >> %[2]q
				printf 'https://github.com/coilyco-flight-deck/ward/issues/42\n'
				;;
			comment)
				cat "$bodyfile" >> %[2]q
				printf '\n---\n' >> %[2]q
				;;
			*)
				printf 'unexpected gh issue subcommand: %%s\n' "$*" >&2
				exit 1
				;;
		esac
		;;
	*)
		printf 'unexpected gh command: %%s\n' "$*" >&2
		exit 1
		;;
esac
`, callsLog, bodiesLog)
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: func(string) (string, error) { return stub, nil }}}
	return &githubClient{r: r, mode: modeClaude}, callsLog, bodiesLog
}
