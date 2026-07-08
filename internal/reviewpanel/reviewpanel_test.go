package reviewpanel

import (
	"errors"
	"strings"
	"testing"
)

func TestParseClass(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    Class
		wantErr bool
	}{
		{"", ClassDefault, false},
		{"default", ClassDefault, false},
		{"lint-cleanup", ClassLintCleanup, false},
		{"refactor", ClassRefactor, false},
		{"  refactor  ", ClassRefactor, false},
		{"nonsense", "", true},
	} {
		got, err := ParseClass(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseClass(%q): want error, got nil", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseClass(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestRequiredPasses(t *testing.T) {
	for _, tc := range []struct {
		class Class
		n     int
		want  int
	}{
		{ClassLintCleanup, 2, 1},
		{ClassLintCleanup, 3, 1},
		{ClassDefault, 2, 2}, // majority of 2 = 2
		{ClassDefault, 3, 2}, // majority of 3 = 2
		{ClassDefault, 4, 3},
		{ClassRefactor, 2, 2}, // unanimous
		{ClassRefactor, 3, 3},
		{ClassDefault, 0, 0},
		{ClassLintCleanup, 0, 0},
	} {
		if got := tc.class.RequiredPasses(tc.n); got != tc.want {
			t.Errorf("%s.RequiredPasses(%d) = %d; want %d", tc.class, tc.n, got, tc.want)
		}
	}
}

func TestParseVerdictFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stdout      string
		wantVerdict Verdict
		wantErr     bool
		wantConf    float64
	}{
		{"clean pass", "prose\n```json\n{\"verdict\":\"pass\",\"reason\":\"tried to break it\",\"confidence\":0.8}\n```", Pass, false, 0.8},
		{"clean block", "```json\n{\"verdict\":\"block\",\"reason\":\"off-by-one\",\"confidence\":0.9}\n```", Block, false, 0.9},
		{"empty output blocks", "", Block, true, 0},
		{"garbage blocks", "the diff looks fine to me, ship it", Block, true, 0},
		{"unrecognized verdict blocks", "```json\n{\"verdict\":\"maybe\",\"reason\":\"unsure\"}\n```", Block, true, 0},
		{"bare object no fence", "{\"verdict\":\"pass\",\"reason\":\"ok\",\"confidence\":1.5}", Pass, false, 1.0},
		{"confidence clamped low", "```json\n{\"verdict\":\"block\",\"reason\":\"x\",\"confidence\":-2}\n```", Block, false, 0},
		{"last block wins", "```json\n{\"verdict\":\"block\",\"reason\":\"a\"}\n```\n```json\n{\"verdict\":\"pass\",\"reason\":\"b\",\"confidence\":0.5}\n```", Pass, false, 0.5},
		{"reason with brace", "```json\n{\"verdict\":\"block\",\"reason\":\"the map {k:v} leaks\",\"confidence\":0.7}\n```", Block, false, 0.7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseVerdict("codex", "gpt", tc.stdout)
			if got.Verdict != tc.wantVerdict {
				t.Errorf("verdict = %q; want %q", got.Verdict, tc.wantVerdict)
			}
			if (got.Error != "") != tc.wantErr {
				t.Errorf("error = %q; wantErr=%v", got.Error, tc.wantErr)
			}
			if got.Confidence != tc.wantConf {
				t.Errorf("confidence = %v; want %v", got.Confidence, tc.wantConf)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	pass := ReviewerResult{Family: "a", Verdict: Pass}
	block := ReviewerResult{Family: "b", Verdict: Block}
	errored := ReviewerResult{Family: "c", Verdict: Block, Error: "boom"}
	advisory := ReviewerResult{Family: "d", Verdict: Pass, Advisory: true}

	for _, tc := range []struct {
		name      string
		class     Class
		results   []ReviewerResult
		wantGate  Gate
		wantPass  int
		wantThres int
	}{
		{"default two-pass clears", ClassDefault, []ReviewerResult{pass, pass}, GatePass, 2, 2},
		{"default one-pass blocks", ClassDefault, []ReviewerResult{pass, block}, GateBlock, 1, 2},
		{"lint one-pass clears", ClassLintCleanup, []ReviewerResult{pass, block}, GatePass, 1, 1},
		{"refactor needs all", ClassRefactor, []ReviewerResult{pass, pass}, GatePass, 2, 2},
		{"refactor one block fails", ClassRefactor, []ReviewerResult{pass, block}, GateBlock, 1, 2},
		{"errored never passes", ClassLintCleanup, []ReviewerResult{errored}, GateBlock, 0, 1},
		{"all advisory degrades", ClassDefault, []ReviewerResult{advisory}, GateAdvisory, 0, 0},
		{"empty degrades", ClassDefault, nil, GateAdvisory, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate, passes, thres := Evaluate(tc.class, tc.results)
			if gate != tc.wantGate || passes != tc.wantPass || thres != tc.wantThres {
				t.Errorf("Evaluate = (%q,%d,%d); want (%q,%d,%d)", gate, passes, thres, tc.wantGate, tc.wantPass, tc.wantThres)
			}
		})
	}
}

func TestRefutePromptEmbedsSkill(t *testing.T) {
	got := RefutePrompt(PromptInput{
		Class: ClassDefault,
		Skill: "skill line 1\nskill line 2",
		Diff:  "diff body",
	})
	for _, want := range []string{
		"----- REVIEW SKILL -----",
		"skill line 1",
		"skill line 2",
		"----- END REVIEW SKILL -----",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q\n%s", want, got)
		}
	}
}

// TestPanelIncludesWorker proves the worker's own family is the default free tier.
func TestPanelIncludesWorker(t *testing.T) {
	deps := Deps{
		Run:   func(Reviewer, string) (string, error) { return passStdout, nil },
		Avail: func(Reviewer) (bool, string) { return true, "" },
		Now:   func() int64 { return 7 },
	}
	res := deps.Execute(Config{
		Worker: "claude",
		Class:  ClassDefault,
		Candidates: []Reviewer{
			{Family: "claude"},
			{Family: "opencode"},
			{Family: "codex", Paid: true},
		},
	})
	if len(res.Reviewers) == 0 || res.Reviewers[0].Family != "claude" {
		t.Fatalf("worker family should run first: %+v", res.Reviewers)
	}
	if res.Gate != GatePass {
		t.Fatalf("gate = %q; want pass", res.Gate)
	}
	for _, r := range res.Reviewers {
		if r.Family == "claude" && r.Verdict != Pass {
			t.Fatalf("worker family should pass with passStdout: %+v", res.Reviewers)
		}
	}
	if res.Timestamp != 7 {
		t.Errorf("timestamp not sourced from Now: %d", res.Timestamp)
	}
}

// TestPanelSingleFamilyFallback proves the advisory degrade when no reviewer can run.
func TestPanelSingleFamilyFallback(t *testing.T) {
	deps := Deps{
		Run:   func(Reviewer, string) (string, error) { return passStdout, nil },
		Avail: func(Reviewer) (bool, string) { return false, "not installed" },
	}
	res := deps.Execute(Config{
		Worker:     "claude",
		Class:      ClassDefault,
		Candidates: []Reviewer{{Family: "claude"}, {Family: "opencode"}, {Family: "codex", Paid: true}},
	})
	if res.Gate != GateAdvisory {
		t.Fatalf("gate = %q; want advisory", res.Gate)
	}
	if res.Blocks() {
		t.Error("advisory must not block")
	}
	if !strings.Contains(res.Note, "ADVISORY-ONLY") {
		t.Errorf("advisory note missing: %q", res.Note)
	}
}

// TestPanelCostTierEscalation proves the paid tier runs on a free-tier block or a
// high-risk class, not on a clean free-tier pass.
func TestPanelCostTierEscalation(t *testing.T) {
	newDeps := func(freeOut string, ran *[]string) Deps {
		return Deps{
			Avail: func(Reviewer) (bool, string) { return true, "" },
			Run: func(rv Reviewer, _ string) (string, error) {
				*ran = append(*ran, rv.Family)
				if rv.Paid {
					return passStdout, nil
				}
				return freeOut, nil
			},
		}
	}
	cands := []Reviewer{{Family: "opencode"}, {Family: "codex", Paid: true}}

	t.Run("free pass does not escalate", func(t *testing.T) {
		var ran []string
		deps := newDeps(passStdout, &ran)
		deps.Execute(Config{Worker: "claude", Class: ClassLintCleanup, Candidates: cands})
		if contains(ran, "codex") {
			t.Errorf("paid codex ran on a clean lint pass: %v", ran)
		}
	})
	t.Run("free block escalates", func(t *testing.T) {
		var ran []string
		deps := newDeps(blockStdout, &ran)
		deps.Execute(Config{Worker: "claude", Class: ClassDefault, Candidates: cands})
		if !contains(ran, "codex") {
			t.Errorf("paid codex did not run as tiebreaker: %v", ran)
		}
	})
	t.Run("refactor always escalates", func(t *testing.T) {
		var ran []string
		deps := newDeps(passStdout, &ran)
		deps.Execute(Config{Worker: "claude", Class: ClassRefactor, Candidates: cands})
		if !contains(ran, "codex") {
			t.Errorf("paid codex did not run on high-risk refactor: %v", ran)
		}
	})
}

// TestPanelReviewerErrorFailsClosed proves a runner error blocks the gate.
func TestPanelReviewerErrorFailsClosed(t *testing.T) {
	deps := Deps{
		Avail: func(Reviewer) (bool, string) { return true, "" },
		Run:   func(Reviewer, string) (string, error) { return "", errors.New("timeout") },
	}
	res := deps.Execute(Config{
		Worker:     "claude",
		Class:      ClassLintCleanup,
		Candidates: []Reviewer{{Family: "opencode"}},
	})
	if res.Gate != GateBlock {
		t.Fatalf("gate = %q; want block on reviewer error", res.Gate)
	}
	if !res.Blocks() {
		t.Error("errored panel must block")
	}
}

func TestRefutePromptShape(t *testing.T) {
	p := RefutePrompt(PromptInput{
		Class: ClassRefactor, IssueRef: "o/r#1", Diff: "diff-body", CIOutput: "PASS",
	})
	for _, want := range []string{"ADVERSARIAL", "REFUTE", "Default to BLOCK", "diff-body", "refactor", "```json"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

const (
	passStdout  = "```json\n{\"verdict\":\"pass\",\"reason\":\"tried and failed to break it\",\"confidence\":0.8}\n```"
	blockStdout = "```json\n{\"verdict\":\"block\",\"reason\":\"real bug\",\"confidence\":0.9}\n```"
)

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
