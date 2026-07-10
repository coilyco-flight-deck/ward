package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestRunPreflightGooseNeverRunsHostOneShot pins ward#153 closed: a goose dispatch,
// barred from the host one-shot (ward#162), proceeds without a cwd-grounded read.
func TestRunPreflightGooseNeverRunsHostOneShot(t *testing.T) {
	// The barred branch returns before Capture, so io.Discard stderr is enough here;
	// the ward#162 message is re-routed via captureTestStderr below.
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}
	w := resolvedWork{
		Ref:   agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 131},
		Title: "some ward feature",
		Body:  "implement cmd/ward-kdl wiring",
	}

	var proceed bool
	var read string
	var err error
	stderr := captureTestStderr(t, func() {
		proceed, read, err = r.runPreflight(context.Background(), modeGoose, "engineer", w)
	})

	if err != nil {
		t.Fatalf("runPreflight(goose) errored: %v", err)
	}
	if !proceed {
		t.Error("a goose dispatch must proceed to the isolated container run, never gate on a host read")
	}
	if read != "" {
		t.Errorf("a barred goose pre-flight hands back no read; got %q", read)
	}
	// The stderr must name the local-model bar - proof the host one-shot was skipped
	// rather than run in the dispatcher cwd (the ward#153 failure).
	if !strings.Contains(stderr, "local-model harness") {
		t.Errorf("expected the ward#162 local-model bar to fire (closing ward#153); stderr:\n%s", stderr)
	}
}

// TestCapturePreflightRunsInNeutralDir pins the ward#169 lever: the read runs in a
// fresh empty dir, never the dispatch cwd, and restores the cwd afterward.
func TestCapturePreflightRunsInNeutralDir(t *testing.T) {
	if _, err := os.Stat("/bin/pwd"); err != nil {
		t.Skip("/bin/pwd not present")
	}
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}

	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// /bin/pwd computes the physical cwd of the child (its $PWD env is the stale
	// dispatch dir, so it falls back to the real one) - i.e. where the read ran.
	out, err := r.capturePreflight(context.Background(), []string{"/bin/pwd"}, "")
	if err != nil {
		t.Fatalf("capturePreflight: %v", err)
	}
	got := strings.TrimSpace(string(out))

	// The read ran somewhere other than the dispatch cwd...
	if got == before {
		t.Errorf("pre-flight ran in the dispatch cwd %q; want a neutral dir", before)
	}
	// ...and that somewhere is a per-run temp dir, not a leftover path.
	if base := filepath.Base(got); !strings.HasPrefix(base, "ward-preflight-") {
		t.Errorf("pre-flight dir %q is not a ward-preflight temp dir", got)
	}
	// The temp dir is cleaned up after the read (it no longer exists).
	if _, statErr := os.Stat(got); !os.IsNotExist(statErr) {
		t.Errorf("pre-flight temp dir %q should be removed after the read (stat err: %v)", got, statErr)
	}
	// And the process cwd is restored for the launch that follows.
	if after, _ := os.Getwd(); after != before {
		t.Errorf("cwd not restored: before=%q after=%q", before, after)
	}
}

// TestHostOneShotKeepsPromptOffCommandLine is the ward#548 regression: the host one-shot
// prompt rides stdin, never argv, so Windows arg quoting can't truncate the read.
func TestHostOneShotKeepsPromptOffCommandLine(t *testing.T) {
	// A prompt whose ref + question sit BELOW the first newline - exactly the shape
	// the Windows shim truncated to its preamble, dropping the ref/question (ward#548).
	prompt := "You are doing one-shot research on the authoritative issue thread for this repo.\n\n" +
		"Issue: coilyco-gaming/eco-ops#26\n----- the question to answer -----\nverify the telemetry flows\n----- end -----"

	argv, stdin, ok := hostOneShot(modeClaude, prompt)
	if !ok {
		t.Fatal("hostOneShot(claude) ok = false, want a wired host one-shot")
	}
	if stdin != prompt {
		t.Errorf("stdin = %q, want the full prompt fed on stdin", stdin)
	}
	// The prompt must not leak onto the command line in whole or in part.
	for _, a := range argv {
		if strings.Contains(a, "eco-ops#26") || strings.Contains(a, "\n") || a == prompt {
			t.Errorf("prompt leaked onto the command line via argv %q (ward#548)", a)
		}
	}
	// What survives is exactly the base command claude reads its stdin prompt with.
	want := lookupAgent(modeClaude).Record().Argv.Preflight
	if len(argv) != len(want) {
		t.Fatalf("base argv = %v, want the prompt-free base %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("base argv = %v, want the prompt-free base %v", argv, want)
		}
	}

	// A local-model harness stays barred (ok=false), argv and stdin empty (ward#162).
	if a, s, ok := hostOneShot(modeGoose, prompt); ok || a != nil || s != "" {
		t.Errorf("hostOneShot(goose) = (%v, %q, %v), want (nil, \"\", false)", a, s, ok)
	}
}

// TestPreflightPromptRefSurvivesOntoStdin pins ward#560 for the pre-flight's own prompt
// (ward#548's tests use a hand-shaped stand-in): see docs/agent-preflight.md, mechanics.
func TestPreflightPromptRefSurvivesOntoStdin(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 560}
	prompt := preflightPrompt(ref, "Windows pre-flight silently no-ops",
		"cmd.exe shim truncates the multi-line prompt at the first newline", "", nil, nil)
	needle := ref.String() // coilyco-flight-deck/ward#560

	// Preconditions: the test only models the truncation if the ref genuinely sits
	// below the first newline (the shim kept only the preamble above it).
	first := prompt
	if i := strings.IndexByte(prompt, '\n'); i >= 0 {
		first = prompt[:i]
	}
	if strings.Contains(first, needle) {
		t.Fatalf("precondition: ref %q must sit below the first newline to model #560's truncation", needle)
	}
	if !strings.Contains(prompt, needle) || !strings.Contains(prompt, "NO-GO") {
		t.Fatalf("preflightPrompt missing the ref or the GO/NO-GO line the shim used to drop")
	}

	argv, stdin, ok := hostOneShot(modeClaude, prompt)
	if !ok {
		t.Fatal("hostOneShot(claude) ok = false, want a wired host one-shot")
	}
	// The whole pre-flight prompt - ref and verdict instruction included - reaches stdin.
	if stdin != prompt {
		t.Error("the pre-flight prompt must be fed whole on the child's stdin (ward#560)")
	}
	if !strings.Contains(stdin, needle) || !strings.Contains(stdin, "NO-GO") {
		t.Error("stdin lost the ref or the GO/NO-GO instruction below the first newline (ward#560)")
	}
	// Nothing multi-line or ref-bearing may leak onto the command line the shim mangles.
	for _, a := range argv {
		if strings.Contains(a, "\n") || strings.Contains(a, needle) {
			t.Errorf("pre-flight prompt content leaked onto the command line via argv %q (ward#560)", a)
		}
	}
}

// TestCapturePreflightPipesStdinToChild proves the prompt reaches the child on stdin:
// /bin/cat echoes stdin, so the piped prompt must round-trip back out (ward#548).
func TestCapturePreflightPipesStdinToChild(t *testing.T) {
	if _, err := os.Stat("/bin/cat"); err != nil {
		t.Skip("/bin/cat not present")
	}
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}
	prompt := "line one\nIssue: coilyco-gaming/eco-ops#26\nquestion below\n"

	out, err := r.capturePreflight(context.Background(), []string{"/bin/cat"}, prompt)
	if err != nil {
		t.Fatalf("capturePreflight: %v", err)
	}
	if got := string(out); got != prompt {
		t.Errorf("child stdin round-trip = %q, want the piped prompt %q", got, prompt)
	}
	// The swap is scoped to the one capture: the Runner's stdin is restored after.
	if r.Runner.Stdin != nil {
		t.Errorf("Runner.Stdin = %v, want restored to its prior nil after the capture", r.Runner.Stdin)
	}
}
