package main

import (
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
)

// TestIssueModeCeiling pins the label -> ceiling map ward re-implements from
// cli-guard: unlabeled fails closed, several labels take the lowest (#246).
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

// TestAgentSurfaceCeiling pins the role -> ceiling product decision (ward#607):
// only the engineer is gated (needs headless); director/advisor are ungated.
func TestAgentSurfaceCeiling(t *testing.T) {
	if need, name, gated := agentSurfaceCeiling("engineer"); !gated || name != "headless" || need != len(modeCeilingLevels)-1 {
		t.Fatalf("engineer ceiling = (%d, %q, %v), want (%d, headless, true)", need, name, gated, len(modeCeilingLevels)-1)
	}
	for _, role := range []string{"director", "advisor", ""} {
		if _, _, gated := agentSurfaceCeiling(role); gated {
			t.Errorf("role %q should be ungated", role)
		}
	}
}

// TestModeCeilingGateRefusesBelowCeiling runs the exact comparison the dispatch
// seam makes: engineer refuses any below-headless issue, clears a headless one.
func TestModeCeilingGateRefusesBelowCeiling(t *testing.T) {
	need, _, gated := agentSurfaceCeiling("engineer")
	if !gated {
		t.Fatal("engineer must be gated")
	}
	refused := [][]string{nil, {"consult"}, {"interactive"}, {"P0"}}
	for _, labels := range refused {
		if ceil, _ := issueModeCeiling(labels); !(need > ceil) {
			t.Errorf("engineer should refuse labels %v (ceiling not below headless)", labels)
		}
	}
	if ceil, _ := issueModeCeiling([]string{"headless"}); need > ceil {
		t.Error("engineer should clear a headless-labeled issue")
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
