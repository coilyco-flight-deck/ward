package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWardWorkflowCommentVariants(t *testing.T) {
	want := map[string]bool{
		"reservation-held":      true,
		"reservation-released":  true,
		"dispatch-failed":       true,
		"dispatch-deferred":     true,
		"done":                  true,
		"submitted":             true,
		"merge-ready":           true,
		"blocked":               true,
		"failed":                true,
		"review-pass":           true,
		"review-block":          true,
		"review-advisory":       true,
		"qa-pass":               true,
		"qa-failed":             true,
		"qa-blocked":            true,
		"routed":                true,
		"route-unclear":         true,
		"pre-flight-no-go":      true,
		"pre-flight-wrong-repo": true,
		"reopened":              true,
		"triage":                true,
	}
	if len(wardWorkflowCommentVariants) != len(want) {
		t.Fatalf("variant count = %d, want %d", len(wardWorkflowCommentVariants), len(want))
	}
	seen := map[string]bool{}
	for _, v := range wardWorkflowCommentVariants {
		if v == "" {
			t.Fatal("variant list contains an empty entry")
		}
		if seen[v] {
			t.Fatalf("variant %q listed twice", v)
		}
		seen[v] = true
		if !want[v] {
			t.Fatalf("unexpected variant %q in central list", v)
		}
	}
	for v := range want {
		if !seen[v] {
			t.Fatalf("missing variant %q from central list", v)
		}
	}
}

func TestWorkflowCommentParsingAcceptsLegacyCanonicalAndPRURLHeaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		want    string
		wantURL string
		wantPR  int
	}{
		{name: "legacy outcome", body: "WARD-OUTCOME: merge-ready - review passed", want: "merge-ready"},
		{name: "legacy warded", body: "WARDED_WORKFLOW: merge-ready - review passed", want: "merge-ready"},
		{name: "canonical", body: "WARD-WORKFLOW: merge-ready - review passed", want: "merge-ready"},
		{name: "pr url", body: "WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/12", want: "submitted", wantURL: "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/12", wantPR: 12},
		{name: "pr url reviewed and ready", body: strings.Join([]string{
			"WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/12",
			"",
			"<details><summary>details</summary>",
			"",
			"director merge authorization: reviewed-and-ready",
			"",
			"</details>",
		}, "\n"), want: "merge-ready", wantURL: "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/12", wantPR: 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome, ok := backlogOutcomeOfComment(tc.body)
			if !ok {
				t.Fatalf("backlogOutcomeOfComment(%q) did not parse", tc.body)
			}
			if outcome.Status != tc.want {
				t.Fatalf("status = %q, want %q", outcome.Status, tc.want)
			}
			if tc.wantURL != "" && outcome.PRURL != tc.wantURL {
				t.Fatalf("PRURL = %q, want %q", outcome.PRURL, tc.wantURL)
			}
			if tc.wantPR != 0 && outcome.PRNumber != tc.wantPR {
				t.Fatalf("PRNumber = %d, want %d", outcome.PRNumber, tc.wantPR)
			}
		})
	}
}

func TestWorkflowCommentParsingRejectsMalformedPRURL(t *testing.T) {
	for _, body := range []string{
		"WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/not-a-number",
		"WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/12 extra",
	} {
		if outcome, ok := backlogOutcomeOfComment(body); ok {
			t.Fatalf("backlogOutcomeOfComment(%q) parsed unexpectedly: %+v", body, outcome)
		}
	}
}

func TestNoHardcodedWorkflowHeadersOutsideCentralFile(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	legacyEmitter := regexp.MustCompile(`(?m)(collapsedIssueComment\(|visible := |return )"WARD-[A-Z-]+:`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "workflow_comment.go" {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if legacyEmitter.Find(b) != nil {
			t.Fatalf("%s still hardcodes an emitted WARD-* header", file)
		}
	}
}
