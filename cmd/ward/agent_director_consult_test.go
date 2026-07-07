package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestCollectConsultCandidates covers the queue rule (ward#493): consult and untriaged
// tickets enter the interview; already-dispatchable headless/interactive ones don't.
func TestCollectConsultCandidates(t *testing.T) {
	issues := []backlogIssue{
		{Number: 3, Title: "untriaged", Labels: nil},
		{Number: 1, Title: "consult ticket", Labels: []string{"P2", "consult"}},
		{Number: 2, Title: "already headless", Labels: []string{"P1", "headless"}},
		{Number: 4, Title: "interactive", Labels: []string{"interactive"}},
		{Number: 5, Title: "tier only, no mode", Labels: []string{"P3"}},
	}
	got := collectConsultCandidates(issues)
	if len(got) != 3 {
		t.Fatalf("kept %d candidates, want 3 (consult + 2 untriaged): %+v", len(got), got)
	}
	// Ordered by number.
	if got[0].Num != 1 || got[1].Num != 3 || got[2].Num != 5 {
		t.Errorf("candidates not ordered by number: %+v", got)
	}
	by := map[int]consultCandidate{}
	for _, c := range got {
		by[c.Num] = c
	}
	if by[1].Mode != "consult" {
		t.Errorf("#1 should carry Mode=consult: %+v", by[1])
	}
	if by[3].Mode != "" || by[5].Mode != "" {
		t.Errorf("untriaged tickets should carry an empty Mode: %+v %+v", by[3], by[5])
	}
}

// TestParseConsultQuestions covers the delimited-block parse: fields attach to the open
// block, a header opens a new one, and a "#N" inside a field never mis-opens a block.
func TestParseConsultQuestions(t *testing.T) {
	read := strings.Join([]string{
		"=== #12 ===",
		"DECISION: pick a storage backend (relates to #99, do not split)",
		"OPTION: sqlite",
		"OPTION: postgres",
		"RECOMMEND: sqlite",
		"CONSEQUENCE: single-file, no ops burden",
		"",
		"=== #7 ===",
		"DECISION: verify the cron actually runs",
		"OPTION: it runs -> keep",
		"OPTION: it is dormant -> file a fix",
		"RECOMMENDATION: check first",
	}, "\n")
	got := parseConsultQuestions(read)
	if len(got) != 2 {
		t.Fatalf("parsed %d blocks, want 2: %+v", len(got), got)
	}
	q12 := got[12]
	if q12.Decision != "pick a storage backend (relates to #99, do not split)" {
		t.Errorf("#12 decision wrong: %q", q12.Decision)
	}
	if !reflect.DeepEqual(q12.Options, []string{"sqlite", "postgres"}) {
		t.Errorf("#12 options wrong: %+v", q12.Options)
	}
	if q12.Recommend != "sqlite" || q12.Consequence != "single-file, no ops burden" {
		t.Errorf("#12 recommend/consequence wrong: %+v", q12)
	}
	// The "#99" inside the decision line must not have opened a block.
	if _, ok := got[99]; ok {
		t.Errorf("a #99 mention in a DECISION line wrongly opened a block: %+v", got)
	}
	// RECOMMENDATION: (long form) maps to Recommend.
	if got[7].Recommend != "check first" {
		t.Errorf("#7 long-form RECOMMENDATION not parsed: %+v", got[7])
	}
}

// TestParseConsultQuestionsGarbled covers the fail-soft path: a field before any header
// is dropped, and empty output yields an empty map (a hand-driven interview).
func TestParseConsultQuestionsGarbled(t *testing.T) {
	if got := parseConsultQuestions(""); len(got) != 0 {
		t.Errorf("empty read should parse to no questions: %+v", got)
	}
	got := parseConsultQuestions("DECISION: orphan field with no header\nblah blah")
	if len(got) != 0 {
		t.Errorf("a field with no open block should be dropped: %+v", got)
	}
}

// TestParseConsultAction covers the disposition parse + inline remainder, including the
// empty-line skip and the letter/word aliases.
func TestParseConsultAction(t *testing.T) {
	cases := []struct {
		line   string
		want   consultAction
		remain string
	}{
		{"", consultSkip, ""},
		{"  ", consultSkip, ""},
		{"h", consultHeadless, ""},
		{"h we'll use option 2", consultHeadless, "we'll use option 2"},
		{"headless go with sqlite", consultHeadless, "go with sqlite"},
		{"k not enough info yet", consultKeep, "not enough info yet"},
		{"c superseded", consultClose, "superseded"},
		{"m 512 merged up", consultMerge, "512 merged up"},
		{"s", consultSkip, ""},
		{"q", consultQuit, ""},
		{"quit", consultQuit, ""},
		{"zzz", consultUnknown, ""},
	}
	for _, tc := range cases {
		got, rest := parseConsultAction(tc.line)
		if got != tc.want || rest != tc.remain {
			t.Errorf("parseConsultAction(%q) = (%v, %q), want (%v, %q)", tc.line, got, rest, tc.want, tc.remain)
		}
	}
}

// TestParseConsultMergeTarget covers pulling the target issue number out of a merge
// answer and keeping the rest as the note.
func TestParseConsultMergeTarget(t *testing.T) {
	cases := []struct {
		text string
		num  int
		note string
	}{
		{"512 superseded by the rewrite", 512, "superseded by the rewrite"},
		{"#77", 77, ""},
		{"merge into 40", 40, "merge into"},
		{"no number here", 0, ""},
		{"", 0, ""},
	}
	for _, tc := range cases {
		num, note := parseConsultMergeTarget(tc.text)
		if num != tc.num || note != tc.note {
			t.Errorf("parseConsultMergeTarget(%q) = (%d, %q), want (%d, %q)", tc.text, num, note, tc.num, tc.note)
		}
	}
}

// TestConsultDecisionCommentBody covers the DECISION comment: it carries the framed
// decision, the human's answer, the attribution date, and the flip note.
func TestConsultDecisionCommentBody(t *testing.T) {
	q := consultQuestion{Num: 5, Decision: "sqlite vs postgres"}
	body := consultDecisionCommentBody(q, "sqlite, we don't need HA", "2026-07-07")
	for _, want := range []string{
		"## DECISION",
		"**Blocking decision:** sqlite vs postgres",
		"**Resolved:** sqlite, we don't need HA",
		"2026-07-07",
		"headless-dispatchable",
		"`consult` → `headless`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("decision comment missing %q:\n%s", want, body)
		}
	}
	// With no framed decision, the block omits that line but keeps the answer.
	bare := consultDecisionCommentBody(consultQuestion{Num: 5}, "just do it", "2026-07-07")
	if strings.Contains(bare, "**Blocking decision:**") {
		t.Errorf("an unframed ticket should omit the blocking-decision line:\n%s", bare)
	}
	if !strings.Contains(bare, "**Resolved:** just do it") {
		t.Errorf("bare decision comment missing the answer:\n%s", bare)
	}
}

// TestConsultOtherCommentBodies covers the keep / close / merge comment shapes.
func TestConsultOtherCommentBodies(t *testing.T) {
	keep := consultKeepCommentBody("waiting on the security review", "2026-07-07")
	if !strings.Contains(keep, "Kept consult") || !strings.Contains(keep, "waiting on the security review") {
		t.Errorf("keep comment wrong:\n%s", keep)
	}
	closed := consultCloseCommentBody("already done in #400", "2026-07-07")
	if !strings.Contains(closed, "moot") || !strings.Contains(closed, "already done in #400") {
		t.Errorf("close comment wrong:\n%s", closed)
	}
	merge := consultMergeCommentBody(512, "same root cause", "2026-07-07")
	if !strings.Contains(merge, "Merged into #512") || !strings.Contains(merge, "same root cause") {
		t.Errorf("merge comment wrong:\n%s", merge)
	}
	// A merge with no note still names the target and closes.
	bare := consultMergeCommentBody(512, "", "2026-07-07")
	if !strings.Contains(bare, "Superseded by #512.") {
		t.Errorf("note-less merge comment wrong:\n%s", bare)
	}
}

// TestConsultTallyRecord covers the disposition tally that backs the summary + the
// done-condition (every ticket ends in one of the terminal states, or skipped).
func TestConsultTallyRecord(t *testing.T) {
	var tally consultTally
	for _, d := range []string{"headless", "headless", "merged", "closed", "kept", "skipped", "anything-else"} {
		tally.record(d)
	}
	want := consultTally{Headless: 2, Merged: 1, Closed: 1, Kept: 1, Skipped: 2}
	if tally != want {
		t.Errorf("tally = %+v, want %+v", tally, want)
	}
}

// TestConsultInterviewPromptShape covers that the prompt encodes both interrogation
// moves (human decision + fact to verify) and the delimited output contract.
func TestConsultInterviewPromptShape(t *testing.T) {
	prompt := consultInterviewPrompt([]consultCandidate{{Num: 9, Title: "vague ask", Body: "fill the substrate"}})
	for _, want := range []string{"DECISION only a human holds", "FACT a human might misremember", "=== #<num> ===", "#9"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("interview prompt missing %q", want)
		}
	}
}
