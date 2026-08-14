package main

import (
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/exitcode"
)

// TestIssueModeCeiling pins the label -> ceiling map ward re-implements from
// umbra: unlabeled fails closed, several labels take the lowest (#246).
func TestIssueModeCeiling(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   int
		wantNm string
	}{
		{"unlabeled fails closed to consult", nil, 0, "consult (unlabeled default)"},
		{"unrecognized labels ignored", []string{"P1", "bug"}, 0, "consult (unlabeled default)"},
		{"consult", []string{"consult"}, 0, "consult"},
		{"interactive", []string{"interactive"}, 1, "interactive"},
		{"headless", []string{"headless"}, 2, "headless"},
		{"case and space tolerant", []string{" Headless "}, 2, "headless"},
		{"most restrictive of several wins", []string{"headless", "consult"}, 0, "consult"},
		{"interactive over headless", []string{"headless", "interactive"}, 1, "interactive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, name := issueModeCeiling(tc.labels)
			if got != tc.want || name != tc.wantNm {
				t.Fatalf("issueModeCeiling(%v) = (%d, %q), want (%d, %q)", tc.labels, got, name, tc.want, tc.wantNm)
			}
		})
	}
}

// TestIssueHasModeLabel proves the engineer gate keys off the explicit
// interactive label, not the lowest resolved mode ceiling.
func TestIssueHasModeLabel(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"missing", nil, false},
		{"consult only", []string{"consult"}, false},
		{"unlabeled fallback", []string{"P1", "bug"}, false},
		{"interactive", []string{"interactive"}, true},
		{"interactive with consult", []string{"consult", "interactive"}, true},
		{"headless only", []string{"headless"}, false},
		{"case and space tolerant", []string{" interactive "}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := issueHasModeLabel(tc.labels, "interactive"); got != tc.want {
				t.Fatalf("issueHasModeLabel(%v, interactive) = %v, want %v", tc.labels, got, tc.want)
			}
		})
	}
}

// TestEngineerDispatchGateRefusesInteractiveOnly pins the dispatch seam: consult
// and unlabeled/default issues clear, explicit interactive issues refuse.
func TestEngineerDispatchGateRefusesInteractiveOnly(t *testing.T) {
	for _, labels := range [][]string{nil, {"consult"}, {"headless"}, {"P0"}} {
		if issueHasModeLabel(labels, "interactive") {
			t.Errorf("labels %v should not trip the engineer gate", labels)
		}
	}
	if !issueHasModeLabel([]string{"interactive"}, "interactive") {
		t.Fatal("interactive issue should trip the engineer gate")
	}
}

// TestModeCeilingDeclineIsTerminal pins that a mode-ceiling refusal is a Coded
// decline the director marks failed, not a deferral it requeues (ward#607).
func TestModeCeilingDeclineIsTerminal(t *testing.T) {
	err := dispatchDeclineErr(dispatchModeCeiling, "mode-ceiling", "refusing engineer on a/b#1")
	coded := exitcode.From(err)
	if coded == nil {
		t.Fatal("mode-ceiling decline is not Coded; main.go would exit generic 1")
	}
	if coded.Code() != dispatchModeCeiling {
		t.Fatalf("mode-ceiling code = %d, want %d", coded.Code(), dispatchModeCeiling)
	}
	if !isDispatchDecline(err) {
		t.Error("mode-ceiling should classify as a director decline (failed, not deferred)")
	}
	if !strings.Contains(coded.Kind(), "mode-ceiling") {
		t.Errorf("mode-ceiling kind = %q, want it to name the ceiling", coded.Kind())
	}
}
