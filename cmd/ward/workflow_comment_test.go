package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWardedWorkflowCommentVariants(t *testing.T) {
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
	if len(wardedWorkflowCommentVariants) != len(want) {
		t.Fatalf("variant count = %d, want %d", len(wardedWorkflowCommentVariants), len(want))
	}
	seen := map[string]bool{}
	for _, v := range wardedWorkflowCommentVariants {
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

func TestWorkflowCommentParsingAcceptsLegacyAndCanonicalHeaders(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "legacy", body: "WARD-OUTCOME: merge-ready - review passed", want: "merge-ready"},
		{name: "canonical", body: "WARDED_WORKFLOW: merge-ready - review passed", want: "merge-ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome, ok := backlogOutcomeOfComment(tc.body)
			if !ok {
				t.Fatalf("backlogOutcomeOfComment(%q) did not parse", tc.body)
			}
			if outcome.Status != tc.want {
				t.Fatalf("status = %q, want %q", outcome.Status, tc.want)
			}
		})
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
