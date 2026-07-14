package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPRWorkflowMergeAuthorityMatrix pins the ward#1067 matrix:
// director merges pull-request and pull-request-and-merge; nobody merges branch modes.
func TestPRWorkflowMergeAuthorityMatrix(t *testing.T) {
	cases := []struct {
		role    string
		wf      workflowMode
		allowed bool
	}{
		{roleDirector, workflowPullRequest, true},
		{roleDirector, workflowPullRequestAndMerge, true},
		{roleDirector, workflowRemoteBranchOnly, false},
		{roleDirector, workflowDirectToMain, false},
		{roleEngineer, workflowPullRequest, false},
		{roleEngineer, workflowPullRequestAndMerge, true},
		{roleEngineer, workflowRemoteBranchOnly, false},
		{roleEngineer, workflowDirectToMain, false},
		{roleAdvisor, workflowPullRequest, false},
		{roleAdvisor, workflowPullRequestAndMerge, false},
		{roleQA, workflowPullRequest, false},
		{roleQA, workflowPullRequestAndMerge, false},
	}
	for _, tc := range cases {
		err := prWorkflowPermitted(tc.role, tc.wf, prOpMerge)
		if tc.allowed && err != nil {
			t.Errorf("prWorkflowPermitted(%s, %s, merge) = %v, want allowed", tc.role, tc.wf, err)
		}
		if !tc.allowed && err == nil {
			t.Errorf("prWorkflowPermitted(%s, %s, merge) = nil, want denied", tc.role, tc.wf)
		}
	}
}

// TestPRWorkflowReadAndRerunGates pins the non-merge gates, including the
// fail-closed denial of an unknown role.
func TestPRWorkflowReadAndRerunGates(t *testing.T) {
	for _, role := range []string{roleEngineer, roleDirector, roleAdvisor, roleQA} {
		for _, op := range []prWorkflowOp{prOpStatus, prOpRuns} {
			if err := prWorkflowPermitted(role, "", op); err != nil {
				t.Errorf("prWorkflowPermitted(%s, %s) = %v, want allowed", role, op, err)
			}
		}
	}
	for _, tc := range []struct {
		role    string
		allowed bool
	}{
		{roleEngineer, true},
		{roleDirector, true},
		{roleAdvisor, false},
		{roleQA, false},
	} {
		err := prWorkflowPermitted(tc.role, "", prOpRerun)
		if tc.allowed && err != nil {
			t.Errorf("prWorkflowPermitted(%s, rerun) = %v, want allowed", tc.role, err)
		}
		if !tc.allowed && err == nil {
			t.Errorf("prWorkflowPermitted(%s, rerun) = nil, want denied", tc.role)
		}
	}
	for _, op := range []prWorkflowOp{prOpMerge, prOpStatus, prOpRuns, prOpRerun} {
		if err := prWorkflowPermitted("session", workflowPullRequestAndMerge, op); err == nil {
			t.Errorf("prWorkflowPermitted(session, %s) = nil, want fail-closed denial", op)
		}
	}
}

// TestPRWorkflowMarkerMode pins the marker fallback: a PR without a
// ward.workflow marker is the plain pull-request lane.
func TestPRWorkflowMarkerMode(t *testing.T) {
	if got := prWorkflowMarkerMode("closes #12\n\nward.workflow: pull-request-and-merge\n"); got != workflowPullRequestAndMerge {
		t.Errorf("marker mode = %s, want %s", got, workflowPullRequestAndMerge)
	}
	if got := prWorkflowMarkerMode("closes #12\n\nward.workflow: pull-request-and-merge\n"); got != workflowPullRequestAndMerge {
		t.Errorf("legacy marker mode = %s, want %s", got, workflowPullRequestAndMerge)
	}
	if got := prWorkflowMarkerMode("closes #12\n"); got != workflowPullRequest {
		t.Errorf("unmarked mode = %s, want %s", got, workflowPullRequest)
	}
}

// TestPRWorkflowRoleDefaultsToDirectorOnHost pins the acting-role resolution.
func TestPRWorkflowRoleDefaultsToDirectorOnHost(t *testing.T) {
	t.Setenv("WARD_ROLE", "")
	if got := prWorkflowRole(); got != roleDirector {
		t.Errorf("prWorkflowRole() = %q, want %q", got, roleDirector)
	}
	t.Setenv("WARD_ROLE", roleEngineer)
	if got := prWorkflowRole(); got != roleEngineer {
		t.Errorf("prWorkflowRole() = %q, want %q", got, roleEngineer)
	}
}

// prWorkflowFakeForge serves the minimal Forgejo API surface the merge executor
// walks: PR read, merged-state, base branch, combined status, merge.
type prWorkflowFakeForge struct {
	prBody                        string
	prState                       string
	prStateAfterMerge             string
	combinedState                 string
	contextState                  string
	combinedStateAfterMerge       string
	contextStateAfterMerge        string
	defaultMergeStyle             string
	defaultDeleteBranchAfterMerge bool
	allowMergeCommits             bool
	allowSquashMerge              bool
	allowFastForwardOnlyMerge     bool
	allowRebase                   bool
	allowRebaseExplicit           bool
	mergeResponses                []int
	statusFailureAfterMergeCalls  int
	merged                        bool
	markMergedOnSuccess           *bool
	mergeCalls                    int
	mergeDo                       string
	mergeDeleteBranchAfterMerge   bool
	mergedChecks                  int
}

func (f *prWorkflowFakeForge) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		state := f.prState
		if state == "" {
			state = "open"
		}
		if f.merged {
			if f.prStateAfterMerge != "" {
				state = f.prStateAfterMerge
			} else {
				state = "closed"
			}
		}
		_, _ = w.Write([]byte(`{"number":7,"title":"t","body":` + jsonString(f.prBody) + `,"state":"` + state + `","head":{"sha":"headsha","ref":"issue-7"},"base":{"ref":"main"}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"allow_merge_commits":` + jsonBool(f.allowMergeCommits) + `,"allow_squash_merge":` + jsonBool(f.allowSquashMerge) + `,"allow_fast_forward_only_merge":` + jsonBool(f.allowFastForwardOnlyMerge) + `,"allow_rebase":` + jsonBool(f.allowRebase) + `,"allow_rebase_explicit":` + jsonBool(f.allowRebaseExplicit) + `,"default_merge_style":` + jsonString(f.defaultMergeStyle) + `,"default_delete_branch_after_merge":` + jsonBool(f.defaultDeleteBranchAfterMerge) + `}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/7/merge", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			f.mergeCalls++
			status := http.StatusAccepted
			if len(f.mergeResponses) > 0 {
				status = f.mergeResponses[0]
				f.mergeResponses = f.mergeResponses[1:]
			}
			var body struct {
				Do                     string `json:"do"`
				HeadCommitID           string `json:"head_commit_id"`
				DeleteBranchAfterMerge bool   `json:"delete_branch_after_merge"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			f.mergeDo = body.Do
			f.mergeDeleteBranchAfterMerge = body.DeleteBranchAfterMerge
			if status >= 200 && status < 300 {
				if f.prStateAfterMerge != "" {
					f.prState = f.prStateAfterMerge
				} else if f.markMergedOnSuccess == nil || *f.markMergedOnSuccess {
					f.prState = "closed"
				}
				if f.markMergedOnSuccess == nil || *f.markMergedOnSuccess {
					f.merged = true
				}
			}
			w.WriteHeader(status)
			if status == http.StatusMethodNotAllowed {
				_, _ = w.Write([]byte(`{"message":"Please try again later","url":"https://forgejo.coilysiren.me/api/swagger"}`))
			}
			return
		default:
			f.mergedChecks++
			if f.merged {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			f.prState = "open"
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/branches/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"main","protected":true,"enable_status_check":true,"status_check_contexts":["test"]}`))
	})
	// Per-context state rides the `status` key on live Forgejo (gitea-compat),
	// not `state` - the fake serves the live shape to pin effectiveState.
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/headsha/status", func(w http.ResponseWriter, _ *http.Request) {
		combinedState := f.combinedState
		contextState := f.contextState
		if f.statusFailureAfterMergeCalls > 0 && f.mergeCalls >= f.statusFailureAfterMergeCalls {
			combinedState = f.combinedStateAfterMerge
			contextState = f.contextStateAfterMerge
		}
		_, _ = w.Write([]byte(`{"state":"` + combinedState + `","sha":"headsha","total_count":1,"statuses":[{"context":"test","status":"` + contextState + `"}]}`))
	})
	return httptest.NewServer(mux)
}

func jsonBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestPRWorkflowMergeExecEngineerSelfMerge drives the engineer self-merge lane
// end to end against a fake forge, head-pinned and merged-state confirmed.
func TestPRWorkflowMergeExecEngineerSelfMerge(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
	if !strings.Contains(out, "merged coilyco-flight-deck/ward#7") || !strings.Contains(out, "merged-state check: merged") {
		t.Fatalf("merge output = %q, want merged + merged-state confirmation", out)
	}
	if fake.mergeDo != "merge" {
		t.Fatalf("merge do = %q, want merge", fake.mergeDo)
	}
}

// TestPRWorkflowMergeExecExplicitSquash pins the explicit style path.
func TestPRWorkflowMergeExecExplicitSquash(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "squash"); err != nil {
		t.Fatalf("prWorkflowMergeExec squash: %v", err)
	}
	if fake.mergeDo != "squash" {
		t.Fatalf("merge do = %q, want squash", fake.mergeDo)
	}
}

// TestPRWorkflowMergeExecUsesRepoDefaultStyle pins the no-flag path on a
// squash-only repo that defaults to squash.
func TestPRWorkflowMergeExecUsesRepoDefaultStyle(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                        "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:                 "success",
		contextState:                  "success",
		defaultMergeStyle:             "squash",
		defaultDeleteBranchAfterMerge: true,
		allowSquashMerge:              true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, ""); err != nil {
		t.Fatalf("prWorkflowMergeExec default style: %v", err)
	}
	if fake.mergeDo != "squash" {
		t.Fatalf("merge do = %q, want squash", fake.mergeDo)
	}
	if !fake.mergeDeleteBranchAfterMerge {
		t.Fatalf("merge delete_branch_after_merge = %t, want true", fake.mergeDeleteBranchAfterMerge)
	}
}

// TestPRWorkflowMergeExecUsesSmartDefaultStyle pins the KDL override path when
// no explicit --style is passed.
func TestPRWorkflowMergeExecUsesSmartDefaultStyle(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-reservation-ttl "3h"
    pr-merge-style "squash"
}`
	reposBody := `repos {
    repo-authority default=forgejo {
        trusted-owner "coilyco-flight-deck"
        repo "coilyco-flight-deck/*" forge=forgejo
    }
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(reposBody), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+filepath.ToSlash(dir))

	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, ""); err != nil {
		t.Fatalf("prWorkflowMergeExec smart-default style: %v", err)
	}
	if fake.mergeDo != "squash" {
		t.Fatalf("merge do = %q, want squash", fake.mergeDo)
	}
}

// TestPRWorkflowMergeExecRetriesTransient405 pins the bounded settle loop.
// The first 405 should retry and land once the forge accepts it.
func TestPRWorkflowMergeExecRetriesTransient405(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
		mergeResponses:            []int{http.StatusMethodNotAllowed, http.StatusAccepted},
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 2 {
		t.Fatalf("merge calls = %d, want 2", fake.mergeCalls)
	}
	if !strings.Contains(out, "merged coilyco-flight-deck/ward#7") {
		t.Fatalf("merge output = %q, want merged", out)
	}
}

// TestPRWorkflowMergeExecFailsClosedUnmergedPostcondition pins the hard fail:
// Forgejo cannot close the PR without the merged-state proof.
func TestPRWorkflowMergeExecFailsClosedUnmergedPostcondition(t *testing.T) {
	markMergedOnSuccess := false
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		prStateAfterMerge:         "closed",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
		mergeResponses:            []int{http.StatusMethodNotAllowed, http.StatusAccepted},
		markMergedOnSuccess:       &markMergedOnSuccess,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("prWorkflowMergeExec: want closed-unmerged postcondition failure, got nil")
	}
	for _, want := range []string{"state closed", "merged=false", "head headsha"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err, want)
		}
	}
	if fake.mergeCalls != 2 {
		t.Fatalf("merge calls = %d, want 2", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecExplainsLostEligibilityAfter405 pins the diagnostic
// path when Forgejo keeps rejecting the merge and the live gate goes red.
func TestPRWorkflowMergeExecExplainsLostEligibilityAfter405(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                       "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:                "success",
		contextState:                 "success",
		combinedStateAfterMerge:      "failure",
		contextStateAfterMerge:       "failure",
		defaultMergeStyle:            "merge",
		allowMergeCommits:            true,
		allowSquashMerge:             true,
		allowFastForwardOnlyMerge:    true,
		allowRebase:                  true,
		allowRebaseExplicit:          true,
		mergeResponses:               []int{http.StatusMethodNotAllowed},
		statusFailureAfterMergeCalls: 1,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("prWorkflowMergeExec: want diagnostic error, got nil")
	}
	if !strings.Contains(err.Error(), "test=failure") {
		t.Fatalf("error = %q, want failing required context", err)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecCarriesRepoDeleteBranchDefault pins the merge payload's
// delete-branch flag for both repo-default values.
func TestPRWorkflowMergeExecCarriesRepoDeleteBranchDefault(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		defaultDeleteBranchAfterMerge bool
	}{
		{name: "false", defaultDeleteBranchAfterMerge: false},
		{name: "true", defaultDeleteBranchAfterMerge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &prWorkflowFakeForge{
				prBody:                        "closes #6\n\nward.workflow: pull-request-and-merge\n",
				combinedState:                 "success",
				contextState:                  "success",
				defaultMergeStyle:             "merge",
				defaultDeleteBranchAfterMerge: tc.defaultDeleteBranchAfterMerge,
				allowMergeCommits:             true,
				allowSquashMerge:              true,
				allowFastForwardOnlyMerge:     true,
				allowRebase:                   true,
				allowRebaseExplicit:           true,
			}
			srv := fake.server(t)
			defer srv.Close()
			cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
			if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "squash"); err != nil {
				t.Fatalf("prWorkflowMergeExec delete-branch default: %v", err)
			}
			if fake.mergeDeleteBranchAfterMerge != tc.defaultDeleteBranchAfterMerge {
				t.Fatalf("merge delete_branch_after_merge = %t, want %t", fake.mergeDeleteBranchAfterMerge, tc.defaultDeleteBranchAfterMerge)
			}
		})
	}
}

// TestPRWorkflowMergeExecRejectsUnsupportedStyle pins the explicit style
// validation and its error wording.
func TestPRWorkflowMergeExecRejectsUnsupportedStyle(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "manual-rocket")
	if err == nil {
		t.Fatal("want unsupported style refusal, got nil")
	}
	if !strings.Contains(err.Error(), "supported styles: merge, squash, fast-forward-only, rebase, rebase-merge") {
		t.Fatalf("error = %v, want supported-style list", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecDeniesEngineerWithoutMarker pins the mode gate: an
// unmarked PR is the pull-request lane, engineer denied, no forge mutation.
func TestPRWorkflowMergeExecDeniesEngineerWithoutMarker(t *testing.T) {
	fake := &prWorkflowFakeForge{prBody: "closes #6\n", combinedState: "success", contextState: "success"}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("want engineer denial in the pull-request lane, got nil")
	}
	if !strings.Contains(err.Error(), "may not merge under workflow pull-request") {
		t.Fatalf("denial = %v, want role x mode wording", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0 (gate must precede mutation)", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecDirectorMergesUnmarkedPR pins the director half of the
// matrix: an unmarked (pull-request lane) PR is director-mergeable.
func TestPRWorkflowMergeExecDirectorMergesUnmarkedPR(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, ""); err != nil {
		t.Fatalf("director merge in pull-request lane: %v", err)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecRefusesRedStatus pins the live status gate: a failing
// required context stops the merge before any mutation.
func TestPRWorkflowMergeExecRefusesRedStatus(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "failure",
		contextState:              "failure",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("want red-status refusal, got nil")
	}
	if !strings.Contains(err.Error(), "test=failure") {
		t.Fatalf("refusal = %v, want the failing required context named", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
}

// TestPRWorkflowStatusReportRendersCombinedStatus pins the native per-PR CI
// status read a director surface depends on (infrastructure#538).
func TestPRWorkflowStatusReportRendersCombinedStatus(t *testing.T) {
	fake := &prWorkflowFakeForge{prBody: "closes #6\n", combinedState: "failure", contextState: "failure"}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowStatusReport(context.Background(), cl, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("prWorkflowStatusReport: %v", err)
	}
	for _, want := range []string{"combined status: failure", "test = failure", "required on main: test"} {
		if !strings.Contains(out, want) {
			t.Errorf("status report %q missing %q", out, want)
		}
	}
}

// TestValidateDispatchBrokerPRWorkflowShapes pins the broker request gate for
// the ward#1067 actions.
func TestValidateDispatchBrokerPRWorkflowShapes(t *testing.T) {
	valid := []dispatchBrokerRequest{
		{Action: dispatchActionPRStatus, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"},
		{Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"},
		{Action: dispatchActionCIRuns, Role: roleDirector, Target: "coilyco-flight-deck/ward", Limit: 5},
		{Action: dispatchActionCIRerun, Role: roleDirector, Target: "coilyco-flight-deck/ward", RunID: 42},
	}
	for _, req := range valid {
		if err := validateDispatchBrokerPRWorkflow(req); err != nil {
			t.Errorf("validate(%s) = %v, want ok", req.Action, err)
		}
	}
	invalid := []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{"missing role", dispatchBrokerRequest{Action: dispatchActionPRMerge, Target: "coilyco-flight-deck/ward#7"}},
		{"unknown role", dispatchBrokerRequest{Action: dispatchActionPRMerge, Role: "session", Target: "coilyco-flight-deck/ward#7"}},
		{"launch argv", dispatchBrokerRequest{Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward#7", Argv: []string{"x"}}},
		{"no target", dispatchBrokerRequest{Action: dispatchActionPRStatus, Role: roleDirector}},
		{"non-ref target", dispatchBrokerRequest{Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward"}},
		{"out-of-scope owner", dispatchBrokerRequest{Action: dispatchActionPRMerge, Role: roleDirector, Target: "evil/ward#7"}},
		{"runs with ref target", dispatchBrokerRequest{Action: dispatchActionCIRuns, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"}},
		{"rerun without run id", dispatchBrokerRequest{Action: dispatchActionCIRerun, Role: roleDirector, Target: "coilyco-flight-deck/ward"}},
		{"rerun out-of-scope owner", dispatchBrokerRequest{Action: dispatchActionCIRerun, Role: roleDirector, Target: "evil/ward", RunID: 1}},
	}
	for _, tc := range invalid {
		if err := validateDispatchBrokerPRWorkflow(tc.req); err == nil {
			t.Errorf("validate(%s) = nil, want refusal", tc.name)
		}
	}
}

// TestExecDispatchBrokerPRWorkflowGatesRerunByRole pins the host-side re-check:
// the broker denies an advisor rerun before any forge call.
func TestExecDispatchBrokerPRWorkflowGatesRerunByRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("denied rerun must not reach the forge")
	}))
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := execDispatchBrokerPRWorkflowWith(context.Background(), cl, dispatchBrokerRequest{
		Action: dispatchActionCIRerun, Role: roleAdvisor, Target: "coilyco-flight-deck/ward", RunID: 42,
	})
	if err == nil || !strings.Contains(err.Error(), "rerun is withheld") {
		t.Fatalf("advisor rerun = %v, want role denial", err)
	}
}

// TestExecDispatchBrokerPRWorkflowMergeRoundTrip drives the brokered merge on
// the fake forge - the read-only surface path with the specgen bundle absent.
func TestExecDispatchBrokerPRWorkflowMergeRoundTrip(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := execDispatchBrokerPRWorkflowWith(context.Background(), cl, dispatchBrokerRequest{
		Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward#7", MergeStyle: "squash",
	})
	if err != nil {
		t.Fatalf("brokered merge: %v", err)
	}
	if fake.mergeCalls != 1 || !strings.Contains(out, "merged coilyco-flight-deck/ward#7") {
		t.Fatalf("brokered merge output = %q (mergeCalls %d), want one merge", out, fake.mergeCalls)
	}
	if fake.mergeDo != "squash" {
		t.Fatalf("merge do = %q, want squash", fake.mergeDo)
	}
}

// TestAgentRoleCatalogParsesMergeAuthority pins the embedded catalog grants and
// the fail-closed authoring rules for the merge-authority node.
func TestAgentRoleCatalogParsesMergeAuthority(t *testing.T) {
	cat, err := cachedBuiltInAgentRoleCatalog()
	if err != nil {
		t.Fatalf("load embedded catalog: %v", err)
	}
	if got := cat.Definitions[roleEngineer].MergeAuthority; len(got) != 1 || got[0] != workflowPullRequestAndMerge {
		t.Errorf("engineer merge authority = %v, want [%s]", got, workflowPullRequestAndMerge)
	}
	if got := cat.Definitions[roleDirector].MergeAuthority; len(got) != 2 {
		t.Errorf("director merge authority = %v, want pull-request + pull-request-and-merge", got)
	}
	if got := cat.Definitions[roleAdvisor].MergeAuthority; len(got) != 0 {
		t.Errorf("advisor merge authority = %v, want none", got)
	}

	bad := `agent-roles {
    role engineer {
        tagline "t"
        capabilities read
        modes "m"
        default-harness claude
        posture code-landing
        merge-authority "merge-remote-main"
    }
}`
	if _, err := parseAgentRoleCatalog([]byte(bad)); err == nil || !strings.Contains(err.Error(), "merge-authority") {
		t.Errorf("parse bad merge-authority = %v, want fail-closed error", err)
	}
}

// TestForgeRerunGapSurfacesLoudly pins the agentic-os#434 degradation: a forge
// without the rerun API yields the distinct unsupported error, never silence.
func TestForgeRerunGapSurfacesLoudly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	err := cl.RerunActionRun(context.Background(), "coilyco-flight-deck", "ward", 42)
	if !errors.Is(err, errForgeRerunUnsupported) {
		t.Fatalf("rerun on gap forge = %v, want errForgeRerunUnsupported", err)
	}
}
