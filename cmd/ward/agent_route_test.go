package main

import (
	"strings"
	"testing"
)

func TestClassifyTaskInvocation(t *testing.T) {
	cases := []struct {
		name      string
		arg       string
		inline    string
		file      string
		wantRoute bool
		wantRepo  string
		wantErr   bool
	}{
		// A freeform positional with no instruction flag is ROUTE mode.
		{"freeform task", "do the dishes", "", "", true, "", false},
		{"freeform with spaces trimmed", "  clean up the logs  ", "", "", true, "", false},
		// An explicit owner/repo positional stays DIRECT (today's behavior).
		{"explicit repo", "coilyco-flight-deck/ward", "fix the thing", "", false, "coilyco-flight-deck/ward", false},
		{"explicit repo no flag", "coilyco-flight-deck/ward", "", "", false, "coilyco-flight-deck/ward", false},
		// An issue URL / owner/repo#N positional is DIRECT, normalized to its slug (ward#234).
		{"issue url", forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98", "fix the thing", "", false, "coilyco-flight-deck/ward", false},
		{"owner/repo#N", "coilyco-flight-deck/ward#98", "fix the thing", "", false, "coilyco-flight-deck/ward", false},
		// A non-issue URL is freeform content, no phantom owner (ward#234).
		{"actions run url is freeform", forgejoBaseURL + "/coilyco-flight-deck/ward/actions/runs/301", "", "", true, "", false},
		{"prose with embedded url is freeform", "fix CI: " + forgejoBaseURL + "/coilyco-flight-deck/ward/actions/runs/301/jobs/0/attempt/1", "", "", true, "", false},
		// No positional + instructions is DIRECT with cwd inference.
		{"cwd inference", "", "fix the flaky test", "", false, "", false},
		{"cwd inference via file", "", "", "task.md", false, "", false},
		// A freeform positional AND an instruction flag is a contradiction.
		{"freeform plus inline", "do the dishes", "also this", "", false, "", true},
		{"freeform plus file", "do the dishes", "", "task.md", false, "", true},
		// Nothing at all is an error.
		{"empty", "", "", "", false, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			route, repo, err := classifyTaskInvocation(c.arg, c.inline, c.file)
			if c.wantErr {
				if err == nil {
					t.Fatalf("classifyTaskInvocation(%q,%q,%q): want error, got route=%v repo=%q", c.arg, c.inline, c.file, route, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("classifyTaskInvocation(%q,%q,%q): unexpected error %v", c.arg, c.inline, c.file, err)
			}
			if route != c.wantRoute || repo != c.wantRepo {
				t.Errorf("classifyTaskInvocation(%q,%q,%q) = route=%v repo=%q, want route=%v repo=%q",
					c.arg, c.inline, c.file, route, repo, c.wantRoute, c.wantRepo)
			}
		})
	}
}

func TestTaskRepoRef(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		wantSlug string
		wantOK   bool
	}{
		// The two strict ref shapes coerce to a canonical owner/repo slug.
		{"bare ref", "coilyco-flight-deck/ward", "coilyco-flight-deck/ward", true},
		{"bare ref trimmed", "  coilyco-flight-deck/ward  ", "coilyco-flight-deck/ward", true},
		{"bare ref dot-git", "coilyco-flight-deck/ward.git", "coilyco-flight-deck/ward", true},
		{"owner/repo#N", "coilyco-flight-deck/ward#98", "coilyco-flight-deck/ward", true},
		{"issue url", forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98", "coilyco-flight-deck/ward", true},
		// Every other shape is left for ROUTE - no phantom owner lifted (ward#234).
		{"actions run url", forgejoBaseURL + "/coilyco-flight-deck/ward/actions/runs/301", "", false},
		{"jobs attempt url", forgejoBaseURL + "/coilyco-flight-deck/ward/actions/runs/301/jobs/0/attempt/1", "", false},
		{"bare repo url no issue", forgejoBaseURL + "/coilyco-flight-deck/ward", "", false},
		{"clone url", forgejoBaseURL + "/coilyco-flight-deck/ward.git", "", false},
		{"prose with embedded url", "fix CI: " + forgejoBaseURL + "/coilyco-flight-deck/ward/actions/runs/301", "", false},
		{"bare prose", "do the dishes", "", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slug, ok := taskRepoRef(c.arg)
			if ok != c.wantOK || slug != c.wantSlug {
				t.Errorf("taskRepoRef(%q) = %q,%v; want %q,%v", c.arg, slug, ok, c.wantSlug, c.wantOK)
			}
		})
	}
}

func TestRenderRepoCatalog(t *testing.T) {
	got := renderRepoCatalog([]repoCatalogEntry{
		{Slug: "coilyco-flight-deck/ward", Description: "contributor-facing cli-guard consumer"},
		{Slug: "coilysiren/inbox", Description: "  "},
	})
	if !strings.Contains(got, "coilyco-flight-deck/ward — contributor-facing cli-guard consumer") {
		t.Errorf("catalog missing the described repo line\n got: %s", got)
	}
	if !strings.Contains(got, "coilysiren/inbox — (no description)") {
		t.Errorf("a blank description should render a placeholder\n got: %s", got)
	}
	if renderRepoCatalog(nil) != "" {
		t.Errorf("nil catalog should render empty, got %q", renderRepoCatalog(nil))
	}
}

func TestRouteSurveyPrompt(t *testing.T) {
	got := routeSurveyPrompt("  add a --foo flag  ", "- coilyco-flight-deck/ward — the cli")
	for _, want := range []string{
		"add a --foo flag",                   // the task, trimmed
		"coilyco-flight-deck/ward — the cli", // the catalog, embedded
		"REPO: owner/repo",                   // the confident verdict shape
		"UNCLEAR:",                           // the bounce verdict shape
		"copied exactly from the catalog",    // the no-hallucinated-repo guard
	} {
		if !strings.Contains(got, want) {
			t.Errorf("route survey prompt missing %q\n got: %s", want, got)
		}
	}
	// An empty task degrades to a placeholder, never blank.
	if !strings.Contains(routeSurveyPrompt("   ", "cat"), "(no task given)") {
		t.Error("empty task should render a placeholder")
	}
}

func TestParseRouteVerdict(t *testing.T) {
	cases := []struct {
		name     string
		read     string
		want     routeVerdict
		wantRepo string
		wantNote string
	}{
		{"repo with note", "It's a CLI change.\nREPO: coilyco-flight-deck/ward - add the flag", routeRepo, "coilyco-flight-deck/ward", "add the flag"},
		{"repo bare", "REPO: coilyco-flight-deck/ward", routeRepo, "coilyco-flight-deck/ward", ""},
		{"repo no colon", "REPO coilyco-bridge/coily move the ops verb", routeRepo, "coilyco-bridge/coily", "move the ops verb"},
		{"repo markdown bold", "**REPO: coilysiren/site - tweak the homepage**", routeRepo, "coilysiren/site", "tweak the homepage"},
		{"repo bulleted", "- REPO: coilyco-gaming/eco - balance pass", routeRepo, "coilyco-gaming/eco", "balance pass"},
		{"unclear with reason", "Could be two repos.\nUNCLEAR: ward and cli-guard both fit", routeUnclear, "", "ward and cli-guard both fit"},
		{"unclear bare", "UNCLEAR", routeUnclear, "", ""},
		{"last verdict wins", "UNCLEAR: hmm\nOn reflection it's clear.\nREPO: coilyco-flight-deck/ward - do it", routeRepo, "coilyco-flight-deck/ward", "do it"},
		{"repo without a slash is not a verdict", "REPO: ward", routeUnknown, "", ""},
		{"prose only", "I think this belongs somewhere in the fleet.", routeUnknown, "", ""},
		{"empty", "", routeUnknown, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseRouteVerdict(c.read)
			if got.Verdict != c.want {
				t.Errorf("parseRouteVerdict(%q) verdict = %v, want %v", c.read, got.Verdict, c.want)
			}
			if got.Repo != c.wantRepo {
				t.Errorf("parseRouteVerdict(%q) repo = %q, want %q", c.read, got.Repo, c.wantRepo)
			}
			if got.Note != c.wantNote {
				t.Errorf("parseRouteVerdict(%q) note = %q, want %q", c.read, got.Note, c.wantNote)
			}
		})
	}
}

func TestRouteIntakeBody(t *testing.T) {
	got := routeIntakeBody(modeClaude, "  do the dishes  ")
	for _, want := range []string{
		"do the dishes",                   // the literal task, trimmed
		"Task (verbatim)",                 // the verbatim section header
		"ward#164",                        // provenance to this feature
		"ward agent task --driver claude", // names the filing surface
		"surveys the fleet",               // explains what happens next
	} {
		if !strings.Contains(got, want) {
			t.Errorf("routeIntakeBody missing %q\n got: %s", want, got)
		}
	}
	// An empty task degrades to a placeholder, never dangles a blank section.
	if !strings.Contains(routeIntakeBody(modeClaude, "   "), "(no task given)") {
		t.Error("empty task should render a placeholder")
	}
}

func TestRouteChildBody(t *testing.T) {
	intake := agentIssueRef{Owner: inboxOwner, Repo: inboxRepo, Number: 7}
	got := routeChildBody(modeClaude, "add a --foo flag", "wire --foo through the bar command", intake)
	for _, want := range []string{
		"coilysiren/inbox#7",                 // names the intake record
		intake.url(),                         // cross-links back to it
		"wire --foo through the bar command", // the scoping note
		"Scoped for this repo",               // the scoping header
		"add a --foo flag",                   // the original task, verbatim
		"Original task (verbatim)",
		"ward#164",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("routeChildBody missing %q\n got: %s", want, got)
		}
	}
	// With no scoping note the scoped header is omitted, never left dangling.
	if strings.Contains(routeChildBody(modeClaude, "task", "  ", intake), "Scoped for this repo") {
		t.Error("empty scoping note should omit the scoped header")
	}
}

func TestRouteRoutedComment(t *testing.T) {
	child := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 200}
	got := routeRoutedComment(modeClaude, child, "add the flag", "It's a CLI change.\nREPO: coilyco-flight-deck/ward - add the flag")
	for _, want := range []string{
		"coilyco-flight-deck/ward#200", // names the child
		child.url(),                    // links it
		"add the flag",                 // the scoping note
		"Closing this intake record",   // explains the close
		"<details>",                    // folds the read away
		"It's a CLI change.",           // the read verbatim
		"ward#164",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("routeRoutedComment missing %q\n got: %s", want, got)
		}
	}
	// An empty read omits the details block.
	if strings.Contains(routeRoutedComment(modeClaude, child, "", ""), "<details>") {
		t.Error("an empty read should omit the details block")
	}
}

func TestRouteUnclearComment(t *testing.T) {
	got := routeUnclearComment(modeClaude, "ward and cli-guard both fit", "Could be two repos.\nUNCLEAR: ward and cli-guard both fit")
	for _, want := range []string{
		"UNCLEAR",                     // names the verdict
		"ward and cli-guard both fit", // carries the reason
		"could not confidently route", // explains the bounce
		"stays open for a human",
		"ward agent task --driver claude <owner/repo>", // how to re-dispatch DIRECT
		"ward agent headless --driver claude",          // the by-hand alternative
		"<details>",                                    // folds the read away
		"Could be two repos.",                          // the read verbatim
		"ward#164",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("routeUnclearComment missing %q\n got: %s", want, got)
		}
	}
	// An empty reason degrades to a placeholder; an empty read omits the details.
	empty := routeUnclearComment(modeClaude, "  ", "")
	if !strings.Contains(empty, "(no reason given)") {
		t.Errorf("empty reason should render a placeholder; got: %s", empty)
	}
	if strings.Contains(empty, "<details>") {
		t.Error("an empty read should omit the details block")
	}
}
