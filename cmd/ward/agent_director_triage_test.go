package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestP0ContentCandidate covers ward#397: the P0 content-net is a recall stage that trips
// on incident language in title+body and stays quiet on ordinary work.
func TestP0ContentCandidate(t *testing.T) {
	hits := []struct{ title, body string }{
		{"token leaked into commit", ""},
		{"rotate the compromised credential", ""},
		{"auth bypass in the gate", ""},
		{"arbitrary code execution via chmod", ""},
		{"deploy pipeline crashloop", "every deploy fails"},
		{"", "this blocks all committed work"},
		{"lost commit after rebase", "drops local commits silently"},
	}
	for _, h := range hits {
		if !p0ContentCandidate(h.title, h.body) {
			t.Errorf("expected P0 candidate for %q/%q", h.title, h.body)
		}
	}
	misses := []struct{ title, body string }{
		{"add a docs page for tokens", "explains how tokens work"},
		{"refactor the deploy helper", "no behavior change"},
		{"tidy imports", ""},
	}
	for _, m := range misses {
		if p0ContentCandidate(m.title, m.body) {
			t.Errorf("did not expect P0 candidate for %q/%q", m.title, m.body)
		}
	}
}

// TestCollectTriageCandidates covers the skip rule: a fully-labeled issue is already
// triaged and dropped; a partially-labeled one is kept but only for its missing axis.
func TestCollectTriageCandidates(t *testing.T) {
	issues := []backlogIssue{
		{Number: 1, Title: "untriaged", Labels: nil},
		{Number: 2, Title: "fully labeled", Labels: []string{"P2", "headless"}},
		{Number: 3, Title: "tier only", Labels: []string{"P1"}},
		{Number: 4, Title: "mode only", Labels: []string{"consult"}},
		{Number: 5, Title: "leaked token into commit", Labels: nil},
	}
	got := collectTriageCandidates(issues)
	if len(got) != 4 {
		t.Fatalf("kept %d candidates, want 4 (the fully-labeled #2 dropped): %+v", len(got), got)
	}
	by := map[int]triageCandidate{}
	for _, c := range got {
		by[c.Num] = c
	}
	if !by[1].NeedTier || !by[1].NeedMode {
		t.Errorf("#1 unlabeled should need both axes: %+v", by[1])
	}
	if by[3].NeedTier || !by[3].NeedMode {
		t.Errorf("#3 tier-only should need mode only: %+v", by[3])
	}
	if !by[4].NeedTier || by[4].NeedMode {
		t.Errorf("#4 mode-only should need tier only: %+v", by[4])
	}
	if !by[5].P0Candidate {
		t.Errorf("#5 incident text should be a P0 candidate: %+v", by[5])
	}
	if by[1].P0Candidate {
		t.Errorf("#1 ordinary text should not be a P0 candidate: %+v", by[1])
	}
}

// TestTriageModeFailClosed covers the fail-closed posture: only a confident
// headless/interactive promotes; everything else lands consult.
func TestTriageModeFailClosed(t *testing.T) {
	cases := []struct {
		v    triageVerdict
		want string
	}{
		{triageVerdict{Mode: "headless", Confident: true}, "headless"},
		{triageVerdict{Mode: "interactive", Confident: true}, "interactive"},
		{triageVerdict{Mode: "headless", Confident: false}, "consult"}, // low confidence fails closed
		{triageVerdict{Mode: "consult", Confident: true}, "consult"},
		{triageVerdict{Mode: "", Confident: true}, "consult"}, // unread mode fails closed
		{triageVerdict{}, "consult"},                          // empty verdict fails closed
	}
	for _, c := range cases {
		if got := triageMode(c.v); got != c.want {
			t.Errorf("triageMode(%+v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// TestAssignTierBands covers the percentile cut: a graded pool splits top-20/20/20/40
// into P1-P4, and a pool with no urgency signal all lands the P3 default.
func TestAssignTierBands(t *testing.T) {
	if got := assignTierBands(nil); len(got) != 0 {
		t.Errorf("empty pool should yield no tiers, got %v", got)
	}

	// Uniform scores: nothing to rank -> all P3 (the documented default tier).
	uniform := []scoredTriage{{1, 1}, {2, 1}, {3, 1}}
	for n, tier := range assignTierBands(uniform) {
		if tier != "P3" {
			t.Errorf("uniform pool #%d = %q, want P3", n, tier)
		}
	}

	// A graded pool of 10: 2 P1, 2 P2, 2 P3, 4 P4, highest score first.
	graded := []scoredTriage{
		{1, 3}, {2, 3}, {3, 2}, {4, 2}, {5, 2}, {6, 1}, {7, 1}, {8, 0}, {9, 0}, {10, 0},
	}
	bands := assignTierBands(graded)
	counts := map[string]int{}
	for _, tier := range bands {
		counts[tier]++
	}
	want := map[string]int{"P1": 2, "P2": 2, "P3": 2, "P4": 4}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("band counts = %v, want %v", counts, want)
	}
	// The top-scored issue must land in the top band.
	if bands[1] != "P1" {
		t.Errorf("highest-scored #1 = %q, want P1", bands[1])
	}
	// The lowest-scored issues sink to P4.
	if bands[10] != "P4" {
		t.Errorf("lowest-scored #10 = %q, want P4", bands[10])
	}
}

// TestAssignTriageTiers covers the carve-then-cut: a confirmed P0 leaves the pool, an
// unconfirmed candidate is scored, and an already-tiered issue is not re-tiered.
func TestAssignTriageTiers(t *testing.T) {
	cands := []triageCandidate{
		{Num: 1, NeedTier: true, P0Candidate: true},  // confirmed -> P0
		{Num: 2, NeedTier: true, P0Candidate: true},  // candidate but NOT confirmed -> scored
		{Num: 3, NeedTier: true},                     // scored
		{Num: 4, NeedTier: false, P0Candidate: true}, // already tiered -> excluded
	}
	verdicts := map[int]triageVerdict{
		1: {P0Confirmed: true, Score: 3},
		2: {P0Confirmed: false, Score: 2},
		3: {Score: 1},
	}
	tiers := assignTriageTiers(cands, verdicts)
	if tiers[1] != "P0" {
		t.Errorf("#1 confirmed candidate = %q, want P0", tiers[1])
	}
	if tiers[2] == "P0" {
		t.Errorf("#2 unconfirmed candidate must not be P0, got %q", tiers[2])
	}
	if _, ok := tiers[4]; ok {
		t.Errorf("#4 already-tiered issue must not be re-tiered, got %q", tiers[4])
	}
}

// TestParseTriageVerdicts covers the one-shot parse: well-formed lines read cleanly,
// decoration is tolerated, and a garbled line fails closed (no headless promotion).
func TestParseTriageVerdicts(t *testing.T) {
	read := strings.Join([]string{
		"here is my triage:",
		"#10 SCORE=3 MODE=headless CONF=high",
		"- #11 SCORE=2 MODE=interactive CONF=low",
		"#12 score: 0 mode: consult conf: high",
		"#13 [P0-CANDIDATE] SCORE=3 MODE=headless CONF=high P0=yes",
		"#14 garbled with no fields",
	}, "\n")
	got := parseTriageVerdicts(read)

	if v := got[10]; v.Score != 3 || v.Mode != "headless" || !v.Confident {
		t.Errorf("#10 = %+v, want score3/headless/confident", v)
	}
	if v := got[11]; v.Score != 2 || v.Mode != "interactive" || v.Confident {
		t.Errorf("#11 = %+v, want score2/interactive/low-conf", v)
	}
	if v := got[12]; v.Score != 0 || v.Mode != "consult" || !v.Confident {
		t.Errorf("#12 (decorated) = %+v, want score0/consult/confident", v)
	}
	if v := got[13]; !v.P0Confirmed || v.Mode != "headless" {
		t.Errorf("#13 = %+v, want P0 confirmed + headless", v)
	}
	// A fieldless line yields a zero verdict, which fails closed to consult.
	if v := got[14]; triageMode(v) != "consult" {
		t.Errorf("#14 garbled must fail closed to consult, got %+v", v)
	}
}

// TestTriageLabelsFor covers the write set: only the missing axis is written (an existing
// human label is never clobbered), and the mode always resolves (fail-closed).
func TestTriageLabelsFor(t *testing.T) {
	// Needs both axes: tier + resolved mode.
	both := triageLabelsFor(
		triageCandidate{NeedTier: true, NeedMode: true},
		triageVerdict{Mode: "headless", Confident: true}, "P1")
	if !reflect.DeepEqual(both, []string{"P1", "headless"}) {
		t.Errorf("both-axes labels = %v, want [P1 headless]", both)
	}

	// Tier already set: only the mode is written.
	modeOnly := triageLabelsFor(
		triageCandidate{NeedTier: false, NeedMode: true},
		triageVerdict{}, "P2")
	if !reflect.DeepEqual(modeOnly, []string{"consult"}) {
		t.Errorf("mode-only labels = %v, want [consult] (fail-closed)", modeOnly)
	}

	// Mode already set: only the tier is written.
	tierOnly := triageLabelsFor(
		triageCandidate{NeedTier: true, NeedMode: false},
		triageVerdict{Mode: "headless", Confident: true}, "P0")
	if !reflect.DeepEqual(tierOnly, []string{"P0"}) {
		t.Errorf("tier-only labels = %v, want [P0]", tierOnly)
	}
}

// TestTriagePromptShape covers the prompt: it flags P0 candidates, includes each issue,
// and asks for the machine-readable per-issue line the parser reads.
func TestTriagePromptShape(t *testing.T) {
	cands := []triageCandidate{
		{Num: 7, Title: "ordinary work", Body: "do a thing"},
		{Num: 8, Title: "token leaked", Body: "secret in commit", P0Candidate: true},
	}
	p := triagePrompt(cands)
	for _, want := range []string{"#7", "#8", "[P0-CANDIDATE]", "SCORE=", "MODE=", "CONF="} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	// Only the incident issue's list row is flagged (the rubric text mentions it too).
	if !strings.Contains(p, "#8 [P0-CANDIDATE]") || strings.Contains(p, "#7 [P0-CANDIDATE]") {
		t.Errorf("expected only #8's row flagged as a P0 candidate:\n%s", p)
	}
}

// TestDirectorTriageDefaultOn covers ward#397: startup triage is on by default, and
// --no-triage turns it off. The run resolves triage as `triage && !no-triage`.
func TestDirectorTriageDefaultOn(t *testing.T) {
	parse := func(args ...string) *cli.Command {
		cmd := &cli.Command{Name: "director", Flags: directorFlags()}
		if err := cmd.Run(t.Context(), append([]string{"director"}, args...)); err != nil {
			t.Fatalf("parse director flags %v: %v", args, err)
		}
		return cmd
	}
	resolved := func(c *cli.Command) bool { return c.Bool("triage") && !c.Bool("no-triage") }

	if !resolved(parse()) {
		t.Error("startup triage should default on")
	}
	if resolved(parse("--no-triage")) {
		t.Error("--no-triage should turn startup triage off")
	}
}
