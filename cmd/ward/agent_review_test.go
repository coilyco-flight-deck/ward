package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/coilyco-flight-deck/ward/internal/reviewpanel"
	"github.com/urfave/cli/v3"
)

// TestReviewIssueRefFromEnv proves the ref/URL come off the container target env.
func TestReviewIssueRefFromEnv(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_TARGET_ISSUE", "134")
	t.Setenv("WARD_FORGE", "")
	if got := reviewIssueRef(); got != "coilyco-flight-deck/ward#134" {
		t.Errorf("reviewIssueRef = %q", got)
	}
	if got := reviewIssueURL(); !strings.Contains(got, "/coilyco-flight-deck/ward/issues/134") {
		t.Errorf("reviewIssueURL = %q", got)
	}
}

func TestReviewSkillPathPrefersWorkspaceCopy(t *testing.T) {
	workspace := t.TempDir()
	substrate := t.TempDir()
	workspaceSkill := filepath.Join(workspace, "agentic-os", ".agents", "skills", "tooling-code-review", "SKILL.md")
	substrateSkill := filepath.Join(substrate, "agentic-os", ".agents", "skills", "tooling-code-review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(workspaceSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(substrateSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspaceSkill, []byte("workspace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(substrateSkill, []byte("substrate"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_WORKSPACE_DEST", workspace)
	t.Setenv("WARD_SUBSTRATE_DEST", substrate)

	got := reviewSkillPath()
	want := workspaceSkill
	if got != want {
		t.Fatalf("reviewSkillPath() = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("reviewSkillPath() pointed at missing skill file: %v", err)
	}
}

// TestReviewerCandidatesDefaultToWorker proves the worker's own harness is the
// preferred free tier and the rest of the roster remains available as paid fallbacks.
func TestReviewerCandidatesDefaultToWorker(t *testing.T) {
	got := reviewerCandidates("codex")
	if len(got) == 0 {
		t.Fatal("no reviewer candidates returned")
	}
	if got[0].Family != "codex" || got[0].Paid {
		t.Fatalf("first candidate = %+v, want the worker as the free preferred reviewer", got[0])
	}
	var sawPaid, sawClaude bool
	for _, rv := range got {
		if rv.Paid {
			sawPaid = true
		}
		if rv.Family == "claude" {
			sawClaude = true
		}
	}
	if !sawPaid {
		t.Error("want at least one paid fallback candidate")
	}
	if !sawClaude {
		t.Error("want the remaining roster families to stay available")
	}
}

// TestReviewGateClauseInSeed proves the review gate still renders when explicitly
// enabled, is skipped for remote-branch-only, and is suppressed by reviewGate=false.
func TestReviewGateClauseInSeed(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 134}

	direct := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowDirectToMain, true, "")
	if !strings.Contains(direct, "REVIEW GATE") || !strings.Contains(direct, "ward agent review") {
		t.Errorf("direct-to-main headless seed missing the review gate clause")
	}
	if !strings.Contains(direct, "merge to `main`") {
		t.Errorf("direct-to-main landing phrase missing from the gate clause")
	}
	pr := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowPullRequest, true, "")
	if !strings.Contains(pr, "watching its CI/checks") {
		t.Errorf("pull-request seed should tell PR workflows to keep watching checks after opening the PR")
	}

	branchOnly := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowRemoteBranchOnly, true, "")
	if strings.Contains(branchOnly, "REVIEW GATE") {
		t.Errorf("remote-branch-only lands nothing else; it must not carry the review gate")
	}
	if !strings.Contains(direct, "For `direct-to-main` workflows, landing means merging to `main`") {
		t.Errorf("review gate clause must name the direct-to-main landing path\n got: %s", direct)
	}
	if !strings.Contains(pr, "For `pull-request` workflows, opening the pull request is not a stopping point") {
		t.Errorf("review gate clause must tell pull-request runs to keep watching PR CI\n got: %s", pr)
	}

	off := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowDirectToMain, false, "")
	if strings.Contains(off, "REVIEW GATE") {
		t.Errorf("--skip-review (reviewGate=false) must suppress the clause")
	}
	if !strings.Contains(direct, "review summary") {
		t.Errorf("headless seed must tell the worker to include the review summary")
	}
	if !strings.Contains(direct, "workflow: <mode>; review summary: <summary or skip state>") {
		t.Errorf("headless seed must tell the worker to include the workflow marker")
	}
	if !strings.Contains(off, "intentionally skipped") {
		t.Errorf("skipped review must be explicit in the final comment instructions")
	}
}

// TestEngineerSeedDefaultsSkipReviewGate proves engineer dispatches now omit the
// in-container review clause by default, while the manual review verb remains.
func TestEngineerSeedDefaultsSkipReviewGate(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 676}
	cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", ref.String()})

	if got := reviewGateWanted(cmd, modeCodex, ref); got {
		t.Fatal("engineer dispatch default still wants the review gate")
	}
	reviewGate, reviewSkip := reviewGateDecision(cmd, "engineer", modeCodex, ref)
	if reviewGate {
		t.Fatal("engineer dispatch default should skip the review gate")
	}
	if !strings.Contains(reviewSkip, "temporary ward default") {
		t.Fatalf("skip reason = %q, want the temporary ward default to be named", reviewSkip)
	}
	seed := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowPullRequest, reviewGate, reviewSkip)
	if strings.Contains(seed, "REVIEW GATE") {
		t.Fatalf("default engineer seed still carries the review gate clause:\n%s", seed)
	}
	if !strings.Contains(seed, "temporary ward default") {
		t.Fatalf("default engineer seed should explain the temporary skip:\n%s", seed)
	}
	if got := agentReviewCommand(); got == nil || got.Name != "review" {
		t.Fatalf("agentReviewCommand() = %+v, want the manual review verb to remain available", got)
	}
}

// TestReportPanelMachineLine proves the machine-readable WARD-REVIEW line and the
// human summary reach stdout/stderr for the worker's seed to grep.
func TestReportPanelMachineLine(t *testing.T) {
	for _, tc := range []struct {
		gate     reviewpanel.Gate
		wantLine string
	}{
		{reviewpanel.GatePass, "WARD-REVIEW: pass"},
		{reviewpanel.GateBlock, "WARD-REVIEW: block"},
		{reviewpanel.GateAdvisory, "WARD-REVIEW: advisory"},
	} {
		var out, errb bytes.Buffer
		r := &Runner{Runner: &shell.Runner{Stdout: &out, Stderr: &errb}}
		res := reviewpanel.PanelResult{
			Worker: "claude", Class: reviewpanel.ClassDefault, Gate: tc.gate,
			Note:      "ADVISORY-ONLY REVIEW: nope",
			Reviewers: []reviewpanel.ReviewerResult{{Family: "codex", Verdict: reviewpanel.Block, Reason: "bug"}},
		}
		r.reportPanel(&cli.Command{}, res)
		if !strings.Contains(out.String(), tc.wantLine) {
			t.Errorf("gate %s: stdout missing %q\n got: %s", tc.gate, tc.wantLine, out.String())
		}
		if tc.gate == reviewpanel.GateAdvisory {
			if !strings.Contains(errb.String(), "review note:") {
				t.Errorf("advisory must print the review note; got: %s", errb.String())
			}
			if !strings.Contains(errb.String(), "review summary:") {
				t.Errorf("advisory must print the review summary; got: %s", errb.String())
			}
		}
	}
}

func TestReviewGateWantedHonorsSkipsAndConfig(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 676}
	t.Run("default engineer dispatch skips review temporarily", func(t *testing.T) {
		cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", ref.String()})
		wanted, reason := reviewGateDecision(cmd, "engineer", modeCodex, ref)
		if wanted {
			t.Fatal("temporary engineer default did not disable the review gate")
		}
		if !strings.Contains(reason, "temporary ward default") {
			t.Fatalf("reason = %q, want the temporary ward default to be named", reason)
		}
	})
	t.Run("skip-review flag wins", func(t *testing.T) {
		cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", ref.String(), "--skip-review"})
		if reviewGateWanted(cmd, modeCodex, ref) {
			t.Fatal("skip-review flag did not disable the review gate")
		}
	})
	t.Run("skip-preflight alias also disables review", func(t *testing.T) {
		cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", ref.String(), "--skip-preflight"})
		if reviewGateWanted(cmd, modeCodex, ref) {
			t.Fatal("skip-preflight did not disable the review gate")
		}
	})
	t.Run("config disables review by role, worker, and repo", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, ".ward"), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(home, ".ward", "config.yaml"), []byte("agent:\n  review:\n    skip:\n      - role:engineer\n      - harness:codex\n      - repo:coilyco-flight-deck/ward\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", ref.String()})
		if reviewGateWanted(cmd, modeCodex, ref) {
			t.Fatal("config skip rules did not disable the review gate")
		}
	})
}

func TestSkipAliasesRemainAccepted(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 676}
	for _, args := range [][]string{
		{"engineer", ref.String(), "--no-review-gate"},
		{"engineer", ref.String(), "--no-preflight"},
	} {
		cmd := parseCommandForTest(t, agentSurfaceFlags(), args)
		if reviewGateWanted(cmd, modeCodex, ref) {
			t.Fatalf("alias args %v did not disable the review gate", args)
		}
	}
}

func TestReviewSummaryIncludesNotes(t *testing.T) {
	if got := reviewSummary(reviewpanel.PanelResult{Gate: reviewpanel.GatePass, Reviewers: []reviewpanel.ReviewerResult{{Reason: "tight"}}}); !strings.Contains(got, "passed: tight") {
		t.Fatalf("pass summary = %q", got)
	}
	if got := reviewSummary(reviewpanel.PanelResult{Gate: reviewpanel.GateBlock, Note: "no runnable reviewer"}); !strings.Contains(got, "blocked: no runnable reviewer") {
		t.Fatalf("block summary = %q", got)
	}
	if got := reviewSummary(reviewpanel.PanelResult{Gate: reviewpanel.GateBlock, Reviewers: []reviewpanel.ReviewerResult{{Family: "codex", Verdict: reviewpanel.Block, Reason: "diff misses baseline"}, {Family: "claude", Verdict: reviewpanel.Pass, Reason: "looks fine"}}}); !strings.Contains(got, "blocked: diff misses baseline") {
		t.Fatalf("mixed block summary = %q", got)
	}
	if got := reviewSummary(reviewpanel.PanelResult{Gate: reviewpanel.GateAdvisory, Note: "intentionally skipped"}); !strings.Contains(got, "skipped: intentionally skipped") {
		t.Fatalf("advisory summary = %q", got)
	}
}

func TestFailClosedNoReviewerResultRewordsAdvisoryNote(t *testing.T) {
	advisory := reviewpanel.PanelResult{
		Gate:   reviewpanel.GateAdvisory,
		Worker: "codex",
		Note:   "ADVISORY-ONLY REVIEW: no reviewer family was available for codex, so the adversarial panel could not run and did NOT gate this diff. Dropped: claude (unavailable: no network). A human should review this change with the extra scrutiny an unrun panel would have applied.",
	}
	blocked := failClosedNoReviewerResult(advisory)
	if blocked.Gate != reviewpanel.GateBlock {
		t.Fatalf("gate = %q; want block", blocked.Gate)
	}
	for _, bad := range []string{"ADVISORY-ONLY REVIEW", "did NOT gate this diff"} {
		if strings.Contains(blocked.Note, bad) {
			t.Fatalf("blocked note still contains %q: %q", bad, blocked.Note)
		}
	}
	if got := reviewSummary(blocked); !strings.Contains(got, "blocked: no reviewer family was available for codex, so the adversarial panel could not run and the gate blocked fail-closed.") {
		t.Fatalf("blocked summary = %q", got)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := writeReviewSummaryHandoff(blocked); err != nil {
		t.Fatalf("writeReviewSummaryHandoff: %v", err)
	}
	path, err := reviewSummaryPath()
	if err != nil {
		t.Fatalf("reviewSummaryPath: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	for _, bad := range []string{"ADVISORY-ONLY REVIEW", "did NOT gate this diff"} {
		if strings.Contains(string(got), bad) {
			t.Fatalf("handoff still contains %q: %q", bad, got)
		}
	}

	var out, errb bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stdout: &out, Stderr: &errb}}
	r.reportPanel(&cli.Command{}, blocked)
	if !strings.Contains(out.String(), "WARD-REVIEW: block") {
		t.Fatalf("stdout missing blocked machine line: %s", out.String())
	}
	for _, bad := range []string{"ADVISORY-ONLY REVIEW", "did NOT gate this diff"} {
		if strings.Contains(errb.String(), bad) {
			t.Fatalf("panel output still contains %q: %s", bad, errb.String())
		}
	}

	if got := (reviewBlockedError{result: blocked}).Error(); strings.Contains(got, "ADVISORY-ONLY REVIEW") || strings.Contains(got, "did NOT gate this diff") {
		t.Fatalf("blocked error still contains advisory wording: %q", got)
	}

	if got := reviewConclusionCommentBody(blocked); strings.Contains(got, "ADVISORY-ONLY REVIEW") || strings.Contains(got, "did NOT gate this diff") {
		t.Fatalf("conclusion comment still contains advisory wording: %q", got)
	}
}

func TestWriteReviewSummaryHandoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	res := reviewpanel.PanelResult{Gate: reviewpanel.GateBlock, Note: "no runnable reviewer"}
	if err := writeReviewSummaryHandoff(res); err != nil {
		t.Fatalf("writeReviewSummaryHandoff: %v", err)
	}
	path, err := reviewSummaryPath()
	if err != nil {
		t.Fatalf("reviewSummaryPath: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if !strings.Contains(string(got), "blocked: no runnable reviewer") {
		t.Fatalf("handoff file = %q", got)
	}
}

func TestReviewerRunnerIncludesStderrDetail(t *testing.T) {
	binDir := t.TempDir()
	src := filepath.Join(binDir, "codexmain.go")
	binName := "codex"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(binDir, binName)
	program := `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "auth smoke test: codex credentials missing")
	os.Exit(1)
}
`
	if err := os.WriteFile(src, []byte(program), 0o600); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, src)
	cmd.Dir = binDir
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper binary: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stderr bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stderr: &stderr}}
	run := r.reviewerRunner(context.Background())
	_, err := run(reviewpanel.Reviewer{Family: "codex"}, "prompt body")
	if err == nil {
		t.Fatal("reviewerRunner should fail for the helper binary")
	}
	if !strings.Contains(err.Error(), "auth smoke test: codex credentials missing") {
		t.Fatalf("error %q missing captured stderr detail", err)
	}
	if !strings.Contains(stderr.String(), "auth smoke test: codex credentials missing") {
		t.Fatalf("stderr sink %q missing helper stderr", stderr.String())
	}
}

func TestReviewerAvailableUsesLaunchGate(t *testing.T) {
	binDir := t.TempDir()
	src := filepath.Join(binDir, "claudemain.go")
	binName := "claude"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(binDir, binName)
	program := `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "auth smoke test: claude -p rejected the credentials (exit 1) - they are unusable in-container (ward#222). Refresh the host claude login (re-run 'claude' on the host) and relaunch; WARD_SMOKE_TEST_SKIP=1 bypasses")
	os.Exit(1)
}
`
	if err := os.WriteFile(src, []byte(program), 0o600); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, src)
	cmd.Dir = binDir
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper binary: %v\n%s", err, out)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stderr bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stderr: &stderr, Stdout: io.Discard}}
	ok, reason := r.reviewerAvailable(context.Background())(reviewpanel.Reviewer{Family: "claude"})
	if ok {
		t.Fatal("reviewerAvailable should fail when the launch gate rejects the reviewer")
	}
	if !strings.Contains(reason, "auth smoke test") {
		t.Fatalf("reason %q missing launch-gate detail", reason)
	}
}

func TestReviewConclusionCommentBodyIncludesSummary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		res     reviewpanel.PanelResult
		visible string
		want    []string
	}{
		{
			name: "pass",
			res: reviewpanel.PanelResult{
				Gate:   reviewpanel.GatePass,
				Worker: "codex",
				Reviewers: []reviewpanel.ReviewerResult{{
					Family: "codex", Verdict: reviewpanel.Pass, Reason: "looks good", Confidence: 0.91,
				}},
			},
			visible: "WARD-OUTCOME: done ✅",
			want:    []string{"WARD-OUTCOME: done", "review summary: passed: looks good"},
		},
		{
			name: "block",
			res: reviewpanel.PanelResult{
				Gate:   reviewpanel.GateBlock,
				Note:   "no runnable reviewer",
				Worker: "codex",
				Reviewers: []reviewpanel.ReviewerResult{{
					Family: "codex", Verdict: reviewpanel.Block, Reason: "diff misses baseline", Confidence: 0.91,
				}},
			},
			visible: "WARD-OUTCOME: blocked 🛑",
			want:    []string{"WARD-OUTCOME: blocked", "review summary: blocked: no runnable reviewer", "codex: diff misses baseline"},
		},
		{
			name: "skipped",
			res: reviewpanel.PanelResult{
				Gate:   reviewpanel.GateAdvisory,
				Note:   "review gate skipped by --skip-review / --no-review-gate",
				Worker: "codex",
			},
			visible: "WARD-OUTCOME: done ✅",
			want:    []string{"WARD-OUTCOME: done", "review summary: skipped: review gate skipped by --skip-review / --no-review-gate"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reviewConclusionCommentBody(tc.res)
			if visible := visibleLinesBeforeDetails(got); visible != tc.visible {
				t.Fatalf("reviewConclusionCommentBody visible line = %q\n%s", visible, got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("reviewConclusionCommentBody missing %q\n%s", want, got)
				}
			}
			if !strings.Contains(got, "<details><summary>review details</summary>") {
				t.Fatalf("reviewConclusionCommentBody should collapse details\n%s", got)
			}
		})
	}
}
