package main

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestParseAgentIssueRef(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantErr   bool
	}{
		{"coilyco-flight-deck/ward#98", "coilyco-flight-deck", "ward", 98, false},
		{"  coilyco-flight-deck/ward#98  ", "coilyco-flight-deck", "ward", 98, false},
		{forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98", "coilyco-flight-deck", "ward", 98, false},
		{forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98/", "coilyco-flight-deck", "ward", 98, false},
		// Appended hash fragment (e.g. a comment anchor) is ignored. (#158)
		{forgejoBaseURL + "/coilyco-flight-deck/ward/issues/151#issuecomment-14958", "coilyco-flight-deck", "ward", 151, false},
		// Appended query string is ignored. (#158)
		{forgejoBaseURL + "/coilyco-flight-deck/ward/issues/151?thing=stuff", "coilyco-flight-deck", "ward", 151, false},
		// Trailing slash plus query string, both ignored. (#158)
		{forgejoBaseURL + "/coilyco-flight-deck/ward/issues/151/?thing=stuff", "coilyco-flight-deck", "ward", 151, false},
		// Short form also tolerates an appended query/fragment. (#158)
		{"coilyco-flight-deck/ward#98?thing=stuff", "coilyco-flight-deck", "ward", 98, false},
		{"", "", "", 0, true},
		{"coilyco-flight-deck/ward", "", "", 0, true},               // no #N
		{"coilyco-flight-deck/ward#0", "", "", 0, true},             // non-positive
		{"coilyco-flight-deck/ward#-3", "", "", 0, true},            // negative
		{"https://github.com/owner/repo/issues/1", "", "", 0, true}, // GitHub URL rejected
		{"not-a-ref", "", "", 0, true},
	}
	for _, c := range cases {
		got, err := parseAgentIssueRef(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseAgentIssueRef(%q): want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAgentIssueRef(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Owner != c.wantOwner || got.Repo != c.wantRepo || got.Number != c.wantNum {
			t.Errorf("parseAgentIssueRef(%q) = %s, want %s/%s#%d", c.in, got, c.wantOwner, c.wantRepo, c.wantNum)
		}
	}
}

func TestAgentIssueRefURL(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98}
	want := forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98"
	if got := ref.url(); got != want {
		t.Errorf("url() = %q, want %q", got, want)
	}
	// A URL must round-trip back through the parser.
	back, err := parseAgentIssueRef(ref.url())
	if err != nil || back != ref {
		t.Errorf("url round-trip = %+v, %v; want %+v", back, err, ref)
	}
}

func TestAgentSeedPrompt(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98}
	got := agentSeedPrompt(ref, "  container verb family  ")
	for _, want := range []string{
		"coilyco-flight-deck/ward#98",
		"container verb family",    // title, trimmed
		ref.url(),                  // the read-it-first URL
		"closes #98",               // the close trailer
		"read the full issue body", // first-action instruction
	} {
		if !strings.Contains(got, want) {
			t.Errorf("seed prompt missing %q\n got: %s", want, got)
		}
	}
	// An empty title degrades gracefully, never blank-quotes.
	if !strings.Contains(agentSeedPrompt(ref, "   "), "(untitled)") {
		t.Error("empty title should render as (untitled)")
	}
}

func TestOwnerAllowed(t *testing.T) {
	r := &Runner{}
	for _, ok := range []string{"coilysiren", "coilyco-bridge", "coilyco-flight-deck", "coilyco-gaming"} {
		if !r.ownerAllowed(ok) {
			t.Errorf("ownerAllowed(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"evilcorp", "", "Coilysiren"} {
		if r.ownerAllowed(bad) {
			t.Errorf("ownerAllowed(%q) = true, want false", bad)
		}
	}
}

func TestTaskTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"add a --task flag", "add a --task flag"},
		{"\n\n  add a --task flag  \n\nmore body", "add a --task flag"}, // first non-empty line, trimmed
		{"", "agent task"},          // empty degrades, never blank
		{"   \n  \n", "agent task"}, // whitespace-only degrades too
		{strings.Repeat("x", 80), strings.Repeat("x", taskTitleMaxLen) + "…"}, // truncated + ellipsis
	}
	for _, c := range cases {
		if got := taskTitle(c.in); got != c.want {
			t.Errorf("taskTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// A truncated title must stay within the cap (plus the single ellipsis rune).
	if got := []rune(taskTitle(strings.Repeat("y", 200))); len(got) != taskTitleMaxLen+1 {
		t.Errorf("truncated title rune len = %d, want %d", len(got), taskTitleMaxLen+1)
	}
}

func TestTaskBody(t *testing.T) {
	got := taskBody(modeClaude, "do the thing")
	if !strings.Contains(got, "do the thing") {
		t.Error("body must carry the instructions verbatim")
	}
	if !strings.Contains(got, "ward agent claude task") {
		t.Errorf("body must mark provenance; got: %s", got)
	}
}

func TestPreflightPrompt(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 137}
	got := preflightPrompt(ref, "  pre-flight check  ", "  do the thing  ")
	for _, want := range []string{
		"coilyco-flight-deck/ward#137", // the issue ref
		"pre-flight check",             // title, trimmed
		"do the thing",                 // body, trimmed
		"PRE-FLIGHT",                   // names the check
		"GO",                           // asks for the verdict
		"NO-GO",
		"WRONG-REPO",               // the ward#159 routing verdict
		"coilyco-flight-deck/ward", // names this repo so the agent can contrast
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preflight prompt missing %q\n got: %s", want, got)
		}
	}
	// Empty title/body degrade gracefully, never blank-quote or dangle.
	empty := preflightPrompt(ref, "  ", "  ")
	if !strings.Contains(empty, "(untitled)") || !strings.Contains(empty, "(no description provided)") {
		t.Errorf("empty title/body should render placeholders; got: %s", empty)
	}
}

func TestParsePreflightVerdict(t *testing.T) {
	cases := []struct {
		name       string
		read       string
		want       preflightVerdict
		wantReason string
		wantRepo   string
	}{
		{"bare go", "Looks doable.\nGO", verdictGo, "", ""},
		{"go with punctuation", "Risk is low.\nGO.", verdictGo, "", ""},
		{"nogo with reason", "Scope is unclear.\nNO-GO: needs human scoping", verdictNoGo, "needs human scoping", ""},
		{"nogo no hyphen", "NO GO: the API isn't decided", verdictNoGo, "the API isn't decided", ""},
		{"nogo run together", "NOGO: ambiguous", verdictNoGo, "ambiguous", ""},
		{"nogo bare", "NO-GO", verdictNoGo, "", ""},
		{"markdown bold nogo", "**NO-GO: blocked on a decision**", verdictNoGo, "blocked on a decision", ""},
		{"bulleted go", "- GO", verdictGo, "", ""},
		{"quoted go", "> GO", verdictGo, "", ""},
		{"last line wins", "NO-GO: early doubt\nOn reflection it's fine.\nGO", verdictGo, "", ""},
		{"inline go is not a verdict", "I think we should go ahead and try.", verdictUnknown, "", ""},
		{"empty", "", verdictUnknown, "", ""},
		{"prose only", "This needs more thought before anyone takes it on.", verdictUnknown, "", ""},
		// WRONG-REPO (ward#159): captures the target repo + the trailing reason.
		{"wrong-repo with reason", "This is an ops verb.\nWRONG-REPO: coilyco-bridge/coily - belongs with ops", verdictWrongRepo, "belongs with ops", "coilyco-bridge/coily"},
		{"wrong-repo no hyphen", "WRONG REPO coilyco-flight-deck/cli-guard: engine change", verdictWrongRepo, "engine change", "coilyco-flight-deck/cli-guard"},
		{"wrong-repo run together", "WRONGREPO coilyco-bridge/coily", verdictWrongRepo, "", "coilyco-bridge/coily"},
		{"wrong-repo bare repo only", "WRONG-REPO: coilyco-bridge/coily", verdictWrongRepo, "", "coilyco-bridge/coily"},
		{"wrong-repo markdown bold", "**WRONG-REPO: coilyco-bridge/coily - move it**", verdictWrongRepo, "move it", "coilyco-bridge/coily"},
		{"wrong-repo without a repo is not a verdict", "WRONG-REPO: it goes elsewhere", verdictUnknown, "", ""},
		{"wrong-repo beats nogo on the same line concept", "NO-GO: hmm\nWRONG-REPO: coilyco-bridge/coily - clearer", verdictWrongRepo, "clearer", "coilyco-bridge/coily"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parsePreflightVerdict(c.read)
			if got.Verdict != c.want {
				t.Errorf("parsePreflightVerdict(%q) verdict = %v, want %v", c.read, got.Verdict, c.want)
			}
			if got.Reason != c.wantReason {
				t.Errorf("parsePreflightVerdict(%q) reason = %q, want %q", c.read, got.Reason, c.wantReason)
			}
			if got.Repo != c.wantRepo {
				t.Errorf("parsePreflightVerdict(%q) repo = %q, want %q", c.read, got.Repo, c.wantRepo)
			}
		})
	}
}

func TestPreflightNoGoComment(t *testing.T) {
	got := preflightNoGoComment(modeClaude, "headless", "needs human scoping", "The scope is unclear.\nNO-GO: needs human scoping")
	for _, want := range []string{
		"NO-GO",                      // names the verdict
		"needs human scoping",        // carries the reason
		"ward agent claude headless", // names the dispatching surface
		"--no-preflight",             // tells the human how to re-dispatch
		"No container was launched",
		"<details>",             // folds the full read away
		"The scope is unclear.", // includes the read verbatim
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preflightNoGoComment missing %q\n got: %s", want, got)
		}
	}
	// The task surface (ward#149) attributes to `task`, but still steers the
	// re-dispatch at `headless` since the issue is already filed.
	task := preflightNoGoComment(modeClaude, "task", "needs human scoping", "")
	if !strings.Contains(task, "ward agent claude task") {
		t.Errorf("task surface should attribute to task; got: %s", task)
	}
	if !strings.Contains(task, "ward agent claude headless <ref> --no-preflight") {
		t.Errorf("re-dispatch should point at headless, not task; got: %s", task)
	}
	// An empty reason degrades to a placeholder, never a dangling blockquote.
	empty := preflightNoGoComment(modeClaude, "headless", "  ", "")
	if !strings.Contains(empty, "(no reason given)") {
		t.Errorf("empty reason should render a placeholder; got: %s", empty)
	}
	if strings.Contains(empty, "<details>") {
		t.Errorf("an empty read should omit the details block; got: %s", empty)
	}
}

func TestWrongRepoTarget(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantName  string
		wantOK    bool
	}{
		{"coilyco-bridge/coily", "coilyco-bridge", "coily", true},
		{"  coilyco-flight-deck/cli-guard  ", "coilyco-flight-deck", "cli-guard", true},
		{"", "", "", false},
		{"noslash", "", "", false},
		{"owner/", "", "", false},
		{"/repo", "", "", false},
	}
	for _, c := range cases {
		got, ok := wrongRepoTarget(c.in)
		if ok != c.wantOK {
			t.Errorf("wrongRepoTarget(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && (got.Owner != c.wantOwner || got.Name != c.wantName) {
			t.Errorf("wrongRepoTarget(%q) = %s, want %s/%s", c.in, got.slug(), c.wantOwner, c.wantName)
		}
	}
}

func TestBlindfireIssueBody(t *testing.T) {
	w := resolvedWork{
		Ref:  agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 159},
		Body: "Make ward route ops verbs to coily.",
	}
	got := blindfireIssueBody(modeClaude, "headless", w, "this is an ops verb")
	for _, want := range []string{
		"coilyco-flight-deck/ward#159",        // names the source issue
		"this is an ops verb",                 // the routing reason
		"Make ward route ops verbs to coily.", // the source body, verbatim
		"ward#159",                            // provenance to this feature
		"filed blind",                         // flags that nobody searched
	} {
		if !strings.Contains(got, want) {
			t.Errorf("blindfireIssueBody missing %q\n got: %s", want, got)
		}
	}
	// An empty reason and empty body degrade to placeholders, never dangle.
	empty := blindfireIssueBody(modeClaude, "task", resolvedWork{Ref: w.Ref}, "  ")
	if !strings.Contains(empty, "(no reason given)") {
		t.Errorf("empty reason should render a placeholder; got: %s", empty)
	}
	if !strings.Contains(empty, "(the source issue had no description)") {
		t.Errorf("empty body should render a placeholder; got: %s", empty)
	}
}

func TestPreflightWrongRepoComment(t *testing.T) {
	filed := agentIssueRef{Owner: "coilyco-bridge", Repo: "coily", Number: 42}
	got := preflightWrongRepoComment(modeClaude, "headless", filed, "ops verb", "It's ops.\nWRONG-REPO: coilyco-bridge/coily - ops verb")
	for _, want := range []string{
		"WRONG-REPO",           // names the verdict
		"coilyco-bridge/coily", // the target repo slug
		filed.url(),            // links the freshly-filed issue
		"ops verb",             // the reason
		"No container was launched here",
		"--no-preflight", // how to override if the routing is wrong
		"<details>",      // folds the read away
		"It's ops.",      // the read verbatim
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preflightWrongRepoComment missing %q\n got: %s", want, got)
		}
	}
	// An empty read omits the details block; an empty reason degrades gracefully.
	empty := preflightWrongRepoComment(modeClaude, "task", filed, "  ", "")
	if !strings.Contains(empty, "(no reason given)") {
		t.Errorf("empty reason should render a placeholder; got: %s", empty)
	}
	if strings.Contains(empty, "<details>") {
		t.Errorf("an empty read should omit the details block; got: %s", empty)
	}
}

// TestDockerCreateArgvSeedsAgentArgs verifies the seeded prompt rides as the
// in-container agent's argv: after the image, never as a -e env, never leaked.
func TestDockerCreateArgvSeedsAgentArgs(t *testing.T) {
	p := sampleUpPlan()
	seed := "Work on Forgejo issue coilyco-flight-deck/ward#98."
	p.AgentArgs = []string{seed}
	argv := dockerCreateArgv(p, "/tmp/ward-env-xyz")

	if argv[len(argv)-1] != seed {
		t.Errorf("seed must be the final arg (the agent's argv), got %q", argv[len(argv)-1])
	}
	// The image must sit immediately before the agent args, not at the end.
	imageIdx := -1
	for i, a := range argv {
		if a == p.Image {
			imageIdx = i
		}
	}
	if imageIdx == -1 || imageIdx != len(argv)-2 {
		t.Errorf("image must immediately precede the seeded agent args; image at %d of %d", imageIdx, len(argv))
	}
	// The seed must not have been turned into an env var.
	for _, a := range argv {
		if strings.HasPrefix(a, "WARD_") && strings.Contains(a, seed) {
			t.Errorf("seed prompt leaked into env arg %q", a)
		}
	}
}

// Headless threads WARD_HEADLESS=1 into the container env (the entrypoint runs
// claude -p); a non-headless plan must not set it.
func TestWardEnvHeadless(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_HEADLESS"]; ok {
		t.Error("non-headless plan must not set WARD_HEADLESS")
	}
	p.Headless = true
	if got := p.wardEnv()["WARD_HEADLESS"]; got != "1" {
		t.Errorf("headless plan WARD_HEADLESS = %q, want 1", got)
	}
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	if !strings.Contains(joined, "-e WARD_HEADLESS=1") {
		t.Errorf("docker argv missing -e WARD_HEADLESS=1\n got: %s", joined)
	}
}

// ward#141: goose is a first-class agent surface, so `ward agent goose
// {work,headless,task}` must exist alongside claude/codex/qwen.
func TestAgentModesIncludeGoose(t *testing.T) {
	found := false
	for _, m := range agentModes {
		if m == modeGoose {
			found = true
		}
	}
	if !found {
		t.Errorf("agentModes missing goose; got %v", agentModes)
	}
	// The umbrella command must build a `goose` subcommand with work/headless/task.
	var goose *cli.Command
	for _, c := range agentCommand().Commands {
		if c.Name == string(modeGoose) {
			goose = c
		}
	}
	if goose == nil {
		t.Fatal("agent command has no goose subcommand")
	}
	surfaces := map[string]bool{}
	for _, c := range goose.Commands {
		surfaces[c.Name] = true
	}
	for _, want := range []string{"work", "headless", "task"} {
		if !surfaces[want] {
			t.Errorf("ward agent goose missing %q surface", want)
		}
	}
}

// A goose headless plan threads both WARD_MODE=goose and WARD_HEADLESS=1 so the
// entrypoint picks the `goose run -t` branch.
func TestGooseHeadlessPlanEnv(t *testing.T) {
	p := sampleUpPlan()
	p.Mode = modeGoose
	p.Headless = true
	env := p.wardEnv()
	if env["WARD_MODE"] != "goose" {
		t.Errorf("WARD_MODE = %q, want goose", env["WARD_MODE"])
	}
	if env["WARD_AGENT"] != "goose" {
		t.Errorf("WARD_AGENT = %q, want goose", env["WARD_AGENT"])
	}
	if env["WARD_HEADLESS"] != "1" {
		t.Errorf("WARD_HEADLESS = %q, want 1", env["WARD_HEADLESS"])
	}
}

// A bare up plan (no AgentArgs) still ends at the image - container up's shape.
func TestDockerCreateArgvNoAgentArgs(t *testing.T) {
	p := sampleUpPlan()
	p.AgentArgs = nil
	argv := dockerCreateArgv(p, "")
	if argv[len(argv)-1] != p.Image {
		t.Errorf("with no agent args the image must be the final arg, got %q", argv[len(argv)-1])
	}
}
