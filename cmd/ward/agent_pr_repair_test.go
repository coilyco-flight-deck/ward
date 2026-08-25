package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type prRepairFakeForge struct {
	headSHA    string
	baseSHA    string
	headState  string
	baseState  string
	workflowID string
	mergeable  bool
}

func (f prRepairFakeForge) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/actions/runs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workflow_runs": []map[string]any{{
				"id":            7,
				"index_in_repo": 7,
				"title":         "test run",
				"status":        f.headState,
				"workflow_id":   f.workflowID,
				"prettyref":     "refs/heads/issue-7",
				"commit_sha":    f.headSHA,
				"event":         "pull_request",
				"html_url":      "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/7",
			}},
		})
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/"+f.headSHA+"/status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":       f.headState,
			"sha":         f.headSHA,
			"total_count": 1,
			"statuses": []map[string]any{{
				"context": "test",
				"status":  f.headState,
			}},
		})
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/branches/main", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":                  "main",
			"protected":             true,
			"enable_status_check":   true,
			"status_check_contexts": []string{"test"},
			"commit": map[string]any{
				"id": f.baseSHA,
			},
		})
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/"+f.baseSHA+"/status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":       f.baseState,
			"sha":         f.baseSHA,
			"total_count": 1,
			"statuses": []map[string]any{{
				"context": "test",
				"status":  f.baseState,
			}},
		})
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 7,
			"title":  "repair branch",
			"body":   "closes #7\n",
			"state":  "open",
			"head": map[string]any{
				"sha": f.headSHA,
				"ref": "issue-7",
			},
			"base": map[string]any{
				"ref": "main",
			},
			"mergeable": f.mergeable,
		})
	})
	return httptest.NewServer(mux)
}

// withWardExecVerbs pins the mirrored-verb set for one test, so moving a dev
// verb out of this repo's own .ward/ward.yaml cannot reclassify a bucket. #1681.
func withWardExecVerbs(t *testing.T, verbs ...string) {
	t.Helper()
	previous := hasWardExecVerb
	mirrored := make(map[string]bool, len(verbs))
	for _, verb := range verbs {
		mirrored[verb] = true
	}
	hasWardExecVerb = func(name string) bool { return mirrored[strings.TrimSpace(name)] }
	t.Cleanup(func() { hasWardExecVerb = previous })
}

func TestClassifyForgejoPRRepairBuckets(t *testing.T) {
	withWardExecVerbs(t, "test")
	base := prRepairFakeForge{
		headSHA:   "headsha",
		baseSHA:   "mainsha",
		headState: "failure",
		baseState: "success",
		mergeable: true,
	}
	cases := []struct {
		name      string
		workflow  string
		baseState string
		mergeable bool
		want      prRepairBucket
		wantNote  string
	}{
		{
			name:      "ci parity gap",
			workflow:  "mystery-check",
			baseState: "success",
			mergeable: true,
			want:      prRepairBucketCIParityGap,
			wantNote:  "no local `ward exec mystery-check` mirror",
		},
		{
			name:      "main red",
			workflow:  "test",
			baseState: "failure",
			mergeable: true,
			want:      prRepairBucketMainRed,
			wantNote:  "origin/main is failure",
		},
		{
			name:      "merge queue churn",
			workflow:  "test",
			baseState: "success",
			mergeable: false,
			want:      prRepairBucketMergeQueue,
			wantNote:  "refresh or rebase the branch once",
		},
		{
			name:      "pr regression",
			workflow:  "test",
			baseState: "success",
			mergeable: true,
			want:      prRepairBucketPRRegression,
			wantNote:  "keep the current engineer repair behavior",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := base
			fake.workflowID = tc.workflow
			fake.baseState = tc.baseState
			fake.mergeable = tc.mergeable
			srv := fake.server(t)
			defer srv.Close()

			cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
			assessment, err := classifyForgejoPRRepair(context.Background(), cl, "coilyco-flight-deck", "ward", agentPullRequestContext{
				HeadSHA: fake.headSHA,
				BaseRef: "main",
				Mergeability: func() string {
					if tc.mergeable {
						return "mergeable=true"
					}
					return "mergeable=false"
				}(),
			})
			if err != nil {
				t.Fatalf("classifyForgejoPRRepair: %v", err)
			}
			if assessment.Bucket != tc.want {
				t.Fatalf("bucket = %s, want %s", assessment.Bucket, tc.want)
			}
			if !strings.Contains(assessment.Note, tc.wantNote) {
				t.Fatalf("note = %q, want substring %q", assessment.Note, tc.wantNote)
			}
		})
	}
}

func TestEngineerPRDetailsCarriesRepairBucket(t *testing.T) {
	pr := agentPullRequestContext{
		State:        "open",
		Title:        "Repair merge conflict handling",
		Body:         "This PR fixes the branch checkout path.",
		URL:          "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/913",
		HeadRef:      "repair/branch-checkout",
		BaseRef:      "main",
		Mergeability: "mergeable=true",
		RepairBucket: string(prRepairBucketMainRed),
		RepairNote:   "origin/main is red",
	}
	got := engineerPRDetails(pr, nil, nil, nil)
	for _, want := range []string{"PR repair bucket: main-red", "PR repair note: origin/main is red"} {
		if !strings.Contains(got, want) {
			t.Fatalf("engineerPRDetails missing %q\n%s", want, got)
		}
	}
}
