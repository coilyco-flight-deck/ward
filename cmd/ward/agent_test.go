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
	// A vision-capable harness with a real body keeps the read-it-at-the-URL flow.
	got := agentSeedPrompt(ref, "  container verb family  ", "do the thing", "", modeClaude)
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
	// No --details note when none is passed (and no dangling "Operator note" header).
	if strings.Contains(got, "Operator note") {
		t.Errorf("seed prompt should omit the operator note when details is empty\n got: %s", got)
	}
	// An empty title degrades gracefully, never blank-quotes.
	if !strings.Contains(agentSeedPrompt(ref, "   ", "b", "", modeClaude), "(untitled)") {
		t.Error("empty title should render as (untitled)")
	}
}

// TestAgentSeedPromptEmptyBody covers ward#157: an empty body must be called out
// explicitly for every harness, never implying there is content to go find.
func TestAgentSeedPromptEmptyBody(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 151}
	for _, mode := range agentModes {
		got := agentSeedPrompt(ref, "setup ward-kdl", "   \n  ", "", mode)
		if !strings.Contains(got, "This issue has no body") {
			t.Errorf("%s: empty body should be called out explicitly\n got: %s", mode, got)
		}
		if !strings.Contains(got, "work from the title alone") {
			t.Errorf("%s: empty body should point at the title\n got: %s", mode, got)
		}
		// The hallucination trigger - sending the agent off to "read the full issue
		// body" at the URL - must be gone when there is no body.
		if strings.Contains(got, "read the full issue body") {
			t.Errorf("%s: empty body must drop the read-it-at-the-URL instruction\n got: %s", mode, got)
		}
		if !strings.Contains(got, "closes #151") {
			t.Errorf("%s: close trailer missing\n got: %s", mode, got)
		}
	}
}

// TestAgentSeedPromptNonVisionInlines covers ward#157 steps 2+3: a non-vision
// local harness gets the media-stripped body inlined instead of a URL to read.
func TestAgentSeedPromptNonVisionInlines(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98}
	body := "Real instructions here.\n\n![screenshot](https://imgur.com/7K8q4pQ.png)\n\nSee https://example.com/shot.PNG?v=2 too.\n\nKeep https://example.com/page for context."
	got := agentSeedPrompt(ref, "fix it", body, "", modeGoose)
	for _, want := range []string{
		"issue body (inlined; media stripped)", // the inline marker
		"Real instructions here.",              // the real content survives
		"Keep https://example.com/page",        // non-image links survive
		"do not re-fetch the URL",              // the inline instruction
	} {
		if !strings.Contains(got, want) {
			t.Errorf("non-vision seed missing %q\n got: %s", want, got)
		}
	}
	for _, unwanted := range []string{
		"7K8q4pQ.png",              // the markdown image is gone
		"shot.PNG",                 // the bare image URL is gone
		"read the full issue body", // not sent off to read the URL
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("non-vision seed should strip %q\n got: %s", unwanted, got)
		}
	}
	// A vision-capable harness still reads the body at the URL, no inlining.
	vis := agentSeedPrompt(ref, "fix it", body, "", modeClaude)
	if strings.Contains(vis, "inlined; media stripped") {
		t.Errorf("vision harness should not inline the body\n got: %s", vis)
	}
	if !strings.Contains(vis, "read the full issue body") {
		t.Errorf("vision harness should keep the read-it-at-the-URL flow\n got: %s", vis)
	}
	// A body that is nothing but an image collapses to the empty-body path.
	onlyImg := agentSeedPrompt(ref, "fix it", "![x](https://imgur.com/a.png)", "", modeGoose)
	if !strings.Contains(onlyImg, "This issue has no body") {
		t.Errorf("image-only body should fall back to the empty-body action\n got: %s", onlyImg)
	}
}

func TestAgentSeedPromptDetails(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98}
	got := agentSeedPrompt(ref, "container verb family", "a body", "  do it like this instead, not that  ", modeClaude)
	for _, want := range []string{
		"Operator note",            // the labeled note section (ward#167)
		"--details",                // names where it came from
		"do it like this instead",  // the note, trimmed
		"override the issue text",  // the precedence instruction
		"read the full issue body", // the base seed survives
	} {
		if !strings.Contains(got, want) {
			t.Errorf("seed prompt missing %q\n got: %s", want, got)
		}
	}
	// Whitespace-only details is treated as no note.
	if strings.Contains(agentSeedPrompt(ref, "t", "b", "   \n  ", modeClaude), "Operator note") {
		t.Error("whitespace-only details should not render an operator note")
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
	if !strings.Contains(got, "ward agent task --driver claude") {
		t.Errorf("body must mark provenance; got: %s", got)
	}
}

func TestPreflightPrompt(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 137}
	got := preflightPrompt(ref, "  pre-flight check  ", "  do the thing  ", "", nil)
	for _, want := range []string{
		"coilyco-flight-deck/ward#137", // the issue ref
		"pre-flight check",             // title, trimmed
		"do the thing",                 // body, trimmed
		"PRE-FLIGHT",                   // names the check
		"GO",                           // asks for the verdict
		"NO-GO",
		"WRONG-REPO",               // the ward#159 routing verdict
		"coilyco-flight-deck/ward", // names this repo so the agent can contrast
		"FRESH CLONE",              // ward#169: names the real clone the run gets
		"local working tree",       // ward#169: tells it not to judge from cwd
		"current directory",        // ward#169: missing-here means nothing
		"comment thread",           // ward#154: tells the agent to weigh the comments
		"(no comments yet)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preflight prompt missing %q\n got: %s", want, got)
		}
	}
	// No steering note appears when --details is empty.
	if strings.Contains(got, "steering note") {
		t.Errorf("preflight prompt should omit the steering note when details is empty; got: %s", got)
	}
	// A --details note is woven in for the feasibility read (ward#167).
	withNote := preflightPrompt(ref, "t", "b", "  ship it the other way  ", nil)
	for _, want := range []string{"steering note", "--details", "ship it the other way"} {
		if !strings.Contains(withNote, want) {
			t.Errorf("preflight prompt with details missing %q\n got: %s", want, withNote)
		}
	}
	// Empty title/body degrade gracefully, never blank-quote or dangle.
	empty := preflightPrompt(ref, "  ", "  ", "", nil)
	if !strings.Contains(empty, "(untitled)") || !strings.Contains(empty, "(no description provided)") {
		t.Errorf("empty title/body should render placeholders; got: %s", empty)
	}
	// A decision in the comments must reach the prompt - the ward#154 bug was a
	// pre-flight that re-derived a NO-GO because it never saw the author decide.
	withComments := preflightPrompt(ref, "title", "options A-D, no decision yet", "", []issueComment{
		{Body: "my decision: go with option A", User: struct {
			Login string `json:"login"`
		}{Login: "coilysiren"}},
	})
	for _, want := range []string{"my decision: go with option A", "coilysiren"} {
		if !strings.Contains(withComments, want) {
			t.Errorf("preflight prompt should surface the author's comment %q\n got: %s", want, withComments)
		}
	}
}

func TestPreflightComments(t *testing.T) {
	author := func(login string) struct {
		Login string `json:"login"`
	} {
		return struct {
			Login string `json:"login"`
		}{Login: login}
	}
	comments := []issueComment{
		{Body: "real human question", User: author("coilysiren")},
		{Body: "reservation ping " + agentReservationMarker, User: author("ward-bot")},
		{Body: "stale verdict\n" + preflightNoGoMarker, User: author("ward-bot")},
		{Body: "  ", User: author("coilysiren")},
		{Body: "my decision: option A", User: author("coilysiren")},
	}
	got := preflightComments(comments)
	for _, want := range []string{"real human question", "my decision: option A", "coilysiren"} {
		if !strings.Contains(got, want) {
			t.Errorf("preflightComments dropped human signal %q\n got: %s", want, got)
		}
	}
	for _, drop := range []string{agentReservationMarker, preflightNoGoMarker, "reservation ping", "stale verdict"} {
		if strings.Contains(got, drop) {
			t.Errorf("preflightComments should drop ward's own bookkeeping %q\n got: %s", drop, got)
		}
	}
	// The human's decision must land after the earlier question, so the agent
	// reads the latest word last.
	if i, j := strings.Index(got, "real human question"), strings.Index(got, "my decision"); i < 0 || j < 0 || i > j {
		t.Errorf("comments should render oldest-first; got: %s", got)
	}
	if preflightComments(nil) != "" {
		t.Errorf("nil comments should render empty, got %q", preflightComments(nil))
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
		"NO-GO",                               // names the verdict
		"needs human scoping",                 // carries the reason
		"ward agent headless --driver claude", // names the dispatching surface
		"--no-preflight",                      // tells the human how to re-dispatch
		"No container was launched",
		"<details>",             // folds the full read away
		"The scope is unclear.", // includes the read verbatim
		preflightNoGoMarker,     // hidden token so later reads can drop this comment
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preflightNoGoComment missing %q\n got: %s", want, got)
		}
	}
	// The task surface (ward#149) attributes to `task`, but still steers the
	// re-dispatch at `headless` since the issue is already filed.
	task := preflightNoGoComment(modeClaude, "task", "needs human scoping", "")
	if !strings.Contains(task, "ward agent task --driver claude") {
		t.Errorf("task surface should attribute to task; got: %s", task)
	}
	if !strings.Contains(task, "ward agent headless --driver claude <ref> --no-preflight") {
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

// ward#141/#185: goose stays a first-class driver (in agentModes + the --driver
// choices), and each top-level surface carries a --driver flag.
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
	if !strings.Contains(agentDriverChoices(), "goose") {
		t.Errorf("--driver choices missing goose; got %q", agentDriverChoices())
	}
	// Each top-level surface must exist and carry a --driver flag (so any harness,
	// goose included, is selectable on it).
	surfaces := map[string]*cli.Command{}
	for _, c := range agentCommand().Commands {
		surfaces[c.Name] = c
	}
	for _, want := range []string{"work", "headless", "task", "reply", "ask"} {
		cmd, ok := surfaces[want]
		if !ok {
			t.Errorf("ward agent missing %q surface", want)
			continue
		}
		if !commandHasFlag(cmd, "driver") {
			t.Errorf("ward agent %s missing the --driver flag", want)
		}
	}
}

// commandHasFlag reports whether cmd declares a flag with the given name.
func commandHasFlag(cmd *cli.Command, name string) bool {
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			if n == name {
				return true
			}
		}
	}
	return false
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
