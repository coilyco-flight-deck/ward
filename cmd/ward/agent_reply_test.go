package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseReplyThoroughness(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty defaults to standard", "", defaultReplyThoroughness, false},
		{"whitespace defaults", "   ", defaultReplyThoroughness, false},
		{"quick", "quick", "quick", false},
		{"standard", "standard", "standard", false},
		{"deep", "deep", "deep", false},
		{"case-insensitive", "DEEP", "deep", false},
		{"trimmed", "  quick  ", "quick", false},
		{"unknown errors", "exhaustive", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lvl, err := parseReplyThoroughness(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseReplyThoroughness(%q): want error, got %q", c.in, lvl.Name)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReplyThoroughness(%q): unexpected error %v", c.in, err)
			}
			if lvl.Name != c.want {
				t.Errorf("parseReplyThoroughness(%q) = %q, want %q", c.in, lvl.Name, c.want)
			}
			if lvl.Timeout <= 0 {
				t.Errorf("parseReplyThoroughness(%q): timeout must be positive, got %s", c.in, lvl.Timeout)
			}
			if strings.TrimSpace(lvl.Guidance) == "" {
				t.Errorf("parseReplyThoroughness(%q): empty guidance", c.in)
			}
		})
	}
}

// The depth ladder should get strictly more wall-clock as it digs deeper.
func TestReplyThoroughnessTimeoutsClimb(t *testing.T) {
	var prev time.Duration
	for i, lvl := range replyThoroughnessLevels {
		if i > 0 && lvl.Timeout <= prev {
			t.Errorf("level %q timeout %s not greater than the prior %s", lvl.Name, lvl.Timeout, prev)
		}
		prev = lvl.Timeout
	}
}

// Ref-mode research (ward#411) recasts to a read-only captured one-shot: it streams
// stdout with NO stdin attach (no -i/-t/-d), so a broker forward can't 125 (ward#606).
func TestAdvisorResearchPlan(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 411}
	base := sampleUpPlan()
	base.Repo = targetRepo{Owner: ref.Owner, Name: ref.Repo}
	p := advisorResearchPlan(base, ref)

	if !p.Ask || !p.ReadOnly {
		t.Errorf("research plan must set Ask+ReadOnly, got Ask=%v ReadOnly=%v", p.Ask, p.ReadOnly)
	}
	if !p.Capture || p.TTY {
		t.Errorf("research plan must capture stdout with no TTY (Capture && !TTY), got Capture=%v TTY=%v", p.Capture, p.TTY)
	}
	if p.Role != roleAdvisor {
		t.Errorf("research plan role = %q, want %q", p.Role, roleAdvisor)
	}
	if p.Issue != 0 {
		t.Errorf("research plan must not carry an Issue (host posts; container never lands), got %d", p.Issue)
	}
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	// Captured foreground run: none of -i (stdin attach, the ward#606 125), -d
	// (detached, uncapturable), or -it (TTY). Plain `docker run` streams stdout.
	for _, forbidden := range []string{" -i ", " -it ", " -d "} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("research argv must not contain %q (captured run attaches no stdin)\n got: %s", strings.TrimSpace(forbidden), joined)
		}
	}
	for _, want := range []string{"-e WARD_ASK=1", "-e WARD_READONLY=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("research argv missing %q\n got: %s", want, joined)
		}
	}
}

// The container research timeout must exceed the pure dig time so bring-up (clone,
// install, context compose) doesn't guillotine a shallow depth (ward#411).
func TestContainerResearchSetupBudget(t *testing.T) {
	if containerResearchSetupBudget <= 0 {
		t.Fatalf("setup budget must be positive, got %s", containerResearchSetupBudget)
	}
	for _, lvl := range replyThoroughnessLevels {
		if lvl.Timeout+containerResearchSetupBudget <= lvl.Timeout {
			t.Errorf("level %q: padded timeout must exceed the dig timeout", lvl.Name)
		}
	}
}

func TestReplyResearchPrompt(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 179}
	level, _ := parseReplyThoroughness("deep")
	comments := []issueComment{
		{Body: "a human said this", User: struct {
			Login string `json:"login"`
		}{Login: "alice"}},
		// ward's own bookkeeping must be stripped from the woven thread.
		{Body: "noise " + agentReservationMarker, User: struct {
			Login string `json:"login"`
		}{Login: "coilyco-ops"}},
	}
	got := replyResearchPrompt(ref, "Reply mode", "the body text", comments, "what would it take?", level)

	for _, want := range []string{
		"one-shot research",
		"NOT implementing",             // the read-only contract
		"```json",                      // the structured-emit contract
		"\"issues\"",                   // the fan-out spec list
		"2+ DISTINCT repos",            // the fan-out trigger steer
		"coilyco-flight-deck/ward#179", // the ref
		ref.url(),                      // the URL
		"the body text",                // the issue body
		"a human said this",            // the surviving human comment
		"what would it take?",          // the operator's question
		level.Guidance,                 // the depth steer
		"coilyco-flight-deck/ward.git", // the clone URL it may investigate
	} {
		if !strings.Contains(got, want) {
			t.Errorf("research prompt missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, agentReservationMarker) {
		t.Errorf("research prompt leaked ward's reservation marker into the thread:\n%s", got)
	}
}

func TestAdvisorResearchPrompt(t *testing.T) {
	got := advisorResearchPrompt("")
	for _, want := range []string{
		"Read the issue title, body, and comment thread below as the research brief.",
		"Identify the decisions, options, open questions, and risks",
		"posted back on the issue",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("advisorResearchPrompt default missing %q\n---\n%s", want, got)
		}
	}
	withExtra := advisorResearchPrompt("focus on migration risk")
	for _, want := range []string{
		"Additional framing from the dispatcher:",
		"focus on migration risk",
	} {
		if !strings.Contains(withExtra, want) {
			t.Errorf("advisorResearchPrompt extra missing %q\n---\n%s", want, withExtra)
		}
	}
}

// An empty body and an empty thread degrade to readable placeholders, never blanks.
func TestReplyResearchPromptEmpties(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1}
	level, _ := parseReplyThoroughness("quick")
	got := replyResearchPrompt(ref, "", "", nil, "go", level)
	for _, want := range []string{"(untitled)", "(no description provided)", "(no comments yet)"} {
		if !strings.Contains(got, want) {
			t.Errorf("research prompt missing placeholder %q\n---\n%s", want, got)
		}
	}
}

func TestReplyComment(t *testing.T) {
	level, _ := parseReplyThoroughness("standard")
	got := replyComment(modeClaude, level, "what would it take?", "Here is the answer.")

	for _, want := range []string{
		"ward agent advisor",    // header
		"one-shot **standard**", // the depth in the header
		"> what would it take?", // the quoted question
		"Here is the answer.",   // the research body, inline
		"ward#179",              // provenance
		"not a carried change",  // the verify-before-acting caveat
		replyReplyMarker,        // the hidden marker
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply comment missing %q\n---\n%s", want, got)
		}
	}
}

// A multi-line prompt must stay inside the markdown blockquote, not break out of it.
func TestReplyCommentMultilinePromptStaysQuoted(t *testing.T) {
	level, _ := parseReplyThoroughness("quick")
	got := replyComment(modeClaude, level, "line one\nline two", "answer")
	if !strings.Contains(got, "> line one\n> line two") {
		t.Errorf("multi-line prompt broke out of the blockquote:\n%s", got)
	}
}

// reply signs under the driving mode's identity via commentIssue->signBody; the
// comment body itself must carry no signature (signing happens at send time).
func TestReplyCommentUnsigned(t *testing.T) {
	level, _ := parseReplyThoroughness("deep")
	got := replyComment(modeGoose, level, "q", "a")
	if strings.Contains(got, agentSignatureMarker) {
		t.Errorf("reply comment should not pre-sign; signBody handles that at send time:\n%s", got)
	}
	if !strings.Contains(got, "ward agent advisor --harness goose") {
		t.Errorf("reply comment should name the driving mode:\n%s", got)
	}
}

// A fenced ```json plan decodes into the summary + ordered specs; a prose read
// (no block) fails to parse so the caller falls back to a raw comment (ward#424).
func TestParseReplyPlan(t *testing.T) {
	read := "here is my thinking\n\n```json\n{\"summary\":\"S\",\"issues\":[" +
		"{\"repo\":\"coilyco-flight-deck/cli-guard\",\"title\":\"grammar\",\"body\":\"B1\"}," +
		"{\"repo\":\"coilyco-flight-deck/ward\",\"title\":\"migrate\",\"body\":\"B2\"}]}\n```\ntrailing"
	p, ok := parseReplyPlan(read)
	if !ok {
		t.Fatalf("parseReplyPlan: expected the fenced block to decode")
	}
	if p.Summary != "S" || len(p.Issues) != 2 {
		t.Fatalf("parseReplyPlan: got summary %q, %d issues", p.Summary, len(p.Issues))
	}
	if p.Issues[0].Repo != "coilyco-flight-deck/cli-guard" || p.Issues[1].Title != "migrate" {
		t.Errorf("parseReplyPlan: specs decoded wrong: %+v", p.Issues)
	}

	if _, ok := parseReplyPlan("just prose, no json here"); ok {
		t.Errorf("parseReplyPlan: prose with no block should not parse")
	}
	if _, ok := parseReplyPlan("```json\n{not valid json\n```"); ok {
		t.Errorf("parseReplyPlan: a malformed block should not parse")
	}
}

func TestSanitizeReplyReadExtractsGooseJSONAnswer(t *testing.T) {
	read := "Goose session started\nworking directory: /workspace/ward\n" +
		"────────────────────────────────────────\n" +
		"```json\n{\"summary\":\"Final advisor answer\",\"issues\":[]}\n```\n" +
		"session saved to /tmp/goose/session.json"
	clean := sanitizeReplyRead(modeGoose, read)
	if strings.Contains(clean, "working directory") || strings.Contains(clean, "session saved") {
		t.Errorf("sanitizeReplyRead leaked Goose transcript wrapper:\n%s", clean)
	}
	plan, ok := parseReplyPlan(clean)
	if !ok {
		t.Fatalf("sanitized Goose read should still parse as a reply plan:\n%s", clean)
	}
	if got := plan.singleComment(); got != "Final advisor answer" {
		t.Errorf("sanitized Goose summary = %q, want final answer", got)
	}
}

func TestSanitizeReplyReadSuppressesGooseTranscriptFallback(t *testing.T) {
	read := "Goose session started\nworking directory: /workspace/ward\n" +
		"tool request: shell\nprovider: ollama\nsession saved to /tmp/goose/session.json"
	clean := sanitizeReplyRead(modeGoose, read)
	for _, leaked := range []string{"working directory", "tool request", "session saved"} {
		if strings.Contains(clean, leaked) {
			t.Errorf("sanitized fallback leaked %q:\n%s", leaked, clean)
		}
	}
	if !strings.Contains(clean, "suppressed the raw `goose` advisor transcript") {
		t.Errorf("sanitized fallback should post a compact suppression note, got:\n%s", clean)
	}
}

// A bare {...} span (no fence) is still recovered; broken braces are rejected.
func TestExtractJSONBlock(t *testing.T) {
	if got, ok := extractJSONBlock("noise {\"a\":1} tail"); !ok || got != "{\"a\":1}" {
		t.Errorf("extractJSONBlock bare span = %q, %v", got, ok)
	}
	if _, ok := extractJSONBlock("no braces at all"); ok {
		t.Errorf("extractJSONBlock: text with no braces should fail")
	}
}

// Regression (ward#598): a summary whose markdown carries nested ``` code fences
// must decode, not truncate the object at the first inner fence and leak the raw block.
func TestExtractJSONBlockNestedFences(t *testing.T) {
	// The summary string itself contains fenced code blocks - the exact shape that
	// broke extraction. Braces inside those strings must not close the object early.
	read := "Preamble prose the agent should not have emitted.\n\n" +
		"```json\n{\n" +
		"  \"summary\": \"## Config\\n\\nApply this:\\n\\n```yaml\\nkey: {nested: value}\\n```\\n\\nThen this:\\n\\n```json\\n{\\\"a\\\": 1}\\n```\\ndone\",\n" +
		"  \"issues\": []\n" +
		"}\n```\n\ntrailing prose"
	blk, ok := extractJSONBlock(read)
	if !ok {
		t.Fatalf("extractJSONBlock: nested-fence summary should still yield a block")
	}
	p, ok := parseReplyPlan(read)
	if !ok {
		t.Fatalf("parseReplyPlan: nested-fence summary should decode, got block:\n%s", blk)
	}
	if len(p.Issues) != 0 {
		t.Errorf("expected no fan-out issues, got %d", len(p.Issues))
	}
	// The rendered single comment is the summary markdown, not the raw JSON envelope.
	body := p.singleComment()
	if !strings.HasPrefix(strings.TrimSpace(body), "## Config") {
		t.Errorf("singleComment should render the summary markdown, got:\n%s", body)
	}
	if strings.Contains(body, "\"summary\"") {
		t.Errorf("the JSON envelope leaked into the comment body:\n%s", body)
	}
}

func TestSplitRepoSlug(t *testing.T) {
	cases := []struct {
		in          string
		owner, name string
		ok          bool
	}{
		{"coilyco-flight-deck/ward", "coilyco-flight-deck", "ward", true},
		{"  owner/name  ", "owner", "name", true},
		{"owner/name#5", "", "", false}, // a smuggled ref
		{"owner name", "", "", false},   // whitespace
		{"owner", "", "", false},        // no slash
		{"a/b/c", "", "", false},        // too many parts
		{"/name", "", "", false},
		{"owner/", "", "", false},
	}
	for _, c := range cases {
		o, n, ok := splitRepoSlug(c.in)
		if o != c.owner || n != c.name || ok != c.ok {
			t.Errorf("splitRepoSlug(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, o, n, ok, c.owner, c.name, c.ok)
		}
	}
}

// The trust gate drops untrusted owners and malformed slugs; only trusted, named
// specs survive, and distinctRepoCount reflects the surviving repos (ward#424).
func TestPartitionReplySpecs(t *testing.T) {
	r := &Runner{}
	specs := []replyIssueSpec{
		{Repo: "coilyco-flight-deck/cli-guard", Title: "a", Body: "b"},
		{Repo: "coilyco-flight-deck/ward", Title: "c", Body: "d"},
		{Repo: "evilcorp/pwn", Title: "x", Body: "y"},        // untrusted -> dropped
		{Repo: "coilyco-flight-deck/ward#9", Title: "z"},     // malformed -> dropped
		{Repo: "coilysiren/agentic-os", Title: "", Body: ""}, // empty title -> dropped
	}
	allowed, dropped := r.partitionReplySpecs(specs)
	if len(allowed) != 2 {
		t.Fatalf("partitionReplySpecs: want 2 allowed, got %d (%+v)", len(allowed), allowed)
	}
	if len(dropped) != 3 {
		t.Fatalf("partitionReplySpecs: want 3 dropped, got %d (%+v)", len(dropped), dropped)
	}
	if distinctRepoCount(allowed) != 2 {
		t.Errorf("distinctRepoCount = %d, want 2", distinctRepoCount(allowed))
	}
	// Two specs in the same repo count as one distinct repo (no fan-out trigger).
	same := []resolvedReplySpec{{Owner: "o", Name: "r"}, {Owner: "o", Name: "r"}}
	if distinctRepoCount(same) != 1 {
		t.Errorf("distinctRepoCount(same-repo) = %d, want 1", distinctRepoCount(same))
	}
}

func TestSingleComment(t *testing.T) {
	p := &replyPlan{Summary: "top overview", Issues: []replyIssueSpec{{Title: "T", Body: "the body"}}}
	got := p.singleComment()
	for _, want := range []string{"top overview", "### T", "the body"} {
		if !strings.Contains(got, want) {
			t.Errorf("singleComment missing %q\n---\n%s", want, got)
		}
	}
	// An all-empty plan yields an empty string (caller falls back to the raw read).
	if (&replyPlan{}).singleComment() != "" {
		t.Errorf("empty plan should render an empty comment")
	}
}

func TestReplyChildBody(t *testing.T) {
	source := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "cli-guard", Number: 179}
	first := replyChildBody("do the grammar work", source, 1, 3, "")
	if !strings.Contains(first, "do the grammar work") || !strings.Contains(first, "part 1 of 3") {
		t.Errorf("child body missing content/position:\n%s", first)
	}
	if strings.Contains(first, "Upstream dependency") {
		t.Errorf("the first child should have no upstream:\n%s", first)
	}
	second := replyChildBody("migrate", source, 2, 3, "coilyco-flight-deck/cli-guard#500")
	if !strings.Contains(second, "Upstream dependency: coilyco-flight-deck/cli-guard#500") {
		t.Errorf("later child should cross-link its upstream:\n%s", second)
	}
}

func TestFanOutIndexComment(t *testing.T) {
	level, _ := parseReplyThoroughness("deep")
	created := []createdReplyChild{
		{Ref: agentIssueRef{Owner: "coilyco-flight-deck", Repo: "cli-guard", Number: 501}, Title: "grammar\nsupport"},
		{Ref: agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 425}, Title: "ward-kdl migration"},
	}
	dropped := []droppedReplySpec{{Repo: "evilcorp/pwn", Reason: "untrusted owner \"evilcorp\""}}
	got := fanOutIndexComment(modeClaude, level, "what would it take?", "the overview", created, dropped)
	for _, want := range []string{
		"cross-repo fan-out",
		"> what would it take?",
		"the overview",
		"coilyco-flight-deck/cli-guard#501",
		"grammar support", // title flattened to one line
		"coilyco-flight-deck/ward#425",
		"Dropped (not filed)",
		"evilcorp/pwn",
		replyReplyMarker,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fan-out index missing %q\n---\n%s", want, got)
		}
	}
}
