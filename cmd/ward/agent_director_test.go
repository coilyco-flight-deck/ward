package main

import (
	"reflect"
	"testing"
	"time"
)

func TestParseScopeRepos(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		def  string
		want []string
	}{
		{"empty falls back to default", "", "owner/repo", []string{"owner/repo"}},
		{"single explicit slug", "a/b", "owner/repo", []string{"a/b"}},
		{"comma scope, trimmed", " a/b , c/d ", "", []string{"a/b", "c/d"}},
		{"de-dupes preserving order", "a/b,c/d,a/b", "", []string{"a/b", "c/d"}},
		{"blanks dropped", "a/b,,c/d,", "", []string{"a/b", "c/d"}},
		{"no raw, no default", "", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseScopeRepos(c.raw, c.def)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseScopeRepos(%q,%q) = %v, want %v", c.raw, c.def, got, c.want)
			}
		})
	}
}

func TestBacklogLaneForLabels(t *testing.T) {
	cases := []struct {
		labels []string
		tier   string
		mode   string
		lane   string
	}{
		{[]string{"P0", "headless"}, "P0", "headless", "headless"},
		{[]string{"P2", "interactive"}, "P2", "interactive", "interactive"},
		{[]string{"P1", "consult"}, "P1", "consult", "consult"},
		{[]string{"headless"}, "", "headless", "untriaged"},              // no tier -> untriaged
		{[]string{"P3"}, "P3", "", "untriaged"},                          // no mode -> untriaged
		{[]string{"unrelated", "label"}, "", "", "untriaged"},            // neither
		{[]string{"P4", "P0", "headless"}, "P0", "headless", "headless"}, // highest tier wins
	}
	for _, c := range cases {
		tier := backlogTierOf(c.labels)
		mode := backlogModeOf(c.labels)
		lane := backlogLaneForLabels(tier, mode)
		if tier != c.tier || mode != c.mode || lane != c.lane {
			t.Errorf("labels %v => tier=%q mode=%q lane=%q, want tier=%q mode=%q lane=%q",
				c.labels, tier, mode, lane, c.tier, c.mode, c.lane)
		}
	}
}

func TestRankBacklogIssues(t *testing.T) {
	issues := []backlogIssue{
		{Number: 10, Title: "untriaged", Labels: nil},
		{Number: 20, Title: "P2 headless", Labels: []string{"P2", "headless"}},
		{Number: 5, Title: "P0 headless", Labels: []string{"P0", "headless"}},
		{Number: 30, Title: "P0 interactive", Labels: []string{"P0", "interactive"}},
		{Number: 7, Title: "P1 headless", Labels: []string{"P1", "headless"}},
	}
	got := rankBacklogIssues(issues)
	wantOrder := []int{5, 7, 20, 30, 10} // headless by tier, then interactive, then untriaged
	var gotOrder []int
	for _, r := range got {
		gotOrder = append(gotOrder, r.Num)
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("rank order = %v, want %v", gotOrder, wantOrder)
	}
	if got[0].Lane != "headless" || got[3].Lane != "interactive" || got[4].Lane != "untriaged" {
		t.Errorf("lane assignment wrong: %+v", got)
	}
}

func TestRefreshBacklogLedger(t *testing.T) {
	led := &backlogLedger{Repo: "a/b", Issues: map[string]*backlogEntry{
		// already dispatched: must be preserved, not reset to queued
		"5": {Num: 5, Lane: "headless", State: "dispatched", Container: "ward-b-issue-5-claude-x"},
		// previously surfaced (interactive), now re-triaged into headless -> promote to queued
		"7": {Num: 7, Lane: "interactive", State: "surfaced"},
		// a done issue that has since closed (absent from the live set) -> dropped
		"9": {Num: 9, Lane: "headless", State: "done"},
	}}
	ranked := rankBacklogIssues([]backlogIssue{
		{Number: 5, Title: "five", Labels: []string{"P0", "headless"}},
		{Number: 7, Title: "seven", Labels: []string{"P1", "headless"}}, // promoted to headless
		{Number: 11, Title: "eleven", Labels: []string{"P2", "interactive"}},
		{Number: 12, Title: "twelve", Labels: nil}, // untriaged
	})
	refreshBacklogLedger(led, ranked)

	if e := led.Issues["5"]; e == nil || e.State != "dispatched" {
		t.Errorf("#5 should stay dispatched, got %+v", e)
	}
	if e := led.Issues["7"]; e == nil || e.State != "queued" || e.Lane != "headless" {
		t.Errorf("#7 should be promoted to queued/headless, got %+v", e)
	}
	if _, ok := led.Issues["9"]; ok {
		t.Errorf("#9 closed+done should be dropped, still present")
	}
	if e := led.Issues["11"]; e == nil || e.State != "surfaced" {
		t.Errorf("#11 new interactive should be surfaced, got %+v", e)
	}
	if e := led.Issues["12"]; e == nil || e.State != "skipped" {
		t.Errorf("#12 new untriaged should be skipped, got %+v", e)
	}
}

func TestParseBacklogOutcome(t *testing.T) {
	at := func(s string) time.Time {
		ts, _ := time.Parse(time.RFC3339, s)
		return ts
	}
	cases := []struct {
		name       string
		comments   []issueComment
		wantStatus string
		wantText   string
		wantNil    bool
	}{
		{
			name:     "no marker anywhere",
			comments: []issueComment{{Body: "just a chat comment", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantNil:  true,
		},
		{
			name:       "done leading line",
			comments:   []issueComment{{Body: "WARD-OUTCOME: done - merged and pushed\n\nfelt smooth", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "done",
			wantText:   "merged and pushed",
		},
		{
			name:       "blocked with reason after bullet/quote markers",
			comments:   []issueComment{{Body: "> - WARD-OUTCOME: blocked - need the API key", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "blocked",
			wantText:   "need the API key",
		},
		{
			name: "latest comment wins",
			comments: []issueComment{
				{Body: "WARD-OUTCOME: blocked - earlier", CreatedAt: at("2026-06-25T10:00:00Z")},
				{Body: "WARD-OUTCOME: done - later", CreatedAt: at("2026-06-25T12:00:00Z")},
			},
			wantStatus: "done",
			wantText:   "later",
		},
		{
			name:       "unknown status falls through",
			comments:   []issueComment{{Body: "WARD-OUTCOME: maybe - unsure", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "unknown",
			wantText:   "maybe - unsure",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseBacklogOutcome(c.comments)
			if c.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %s/%q, got nil", c.wantStatus, c.wantText)
			}
			if got.Status != c.wantStatus || got.Text != c.wantText {
				t.Errorf("got %s/%q, want %s/%q", got.Status, got.Text, c.wantStatus, c.wantText)
			}
		})
	}
}

func TestBacklogOutcomeState(t *testing.T) {
	cases := map[string]string{
		"done":    "done",
		"failed":  "failed",
		"blocked": "blocked",
		"unknown": "blocked", // unrecognized parks as blocked
		"weird":   "blocked",
	}
	for in, want := range cases {
		if got := backlogOutcomeState(in); got != want {
			t.Errorf("backlogOutcomeState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBacklogLaneCountsAndPicks(t *testing.T) {
	entries := []*backlogEntry{
		{Num: 1, State: "queued", Tier: "P1", repo: "a/b"},
		{Num: 2, State: "dispatched", Tier: "P0", repo: "a/b"},
		{Num: 3, State: "queued", Tier: "P0", repo: "c/d"},
		{Num: 4, State: "done", Tier: "P0", repo: "a/b"},
		{Num: 5, State: "blocked", Tier: "P0", repo: "a/b"},
	}
	queued, inflight := backlogLaneCounts(entries)
	if queued != 2 || inflight != 1 {
		t.Fatalf("counts = queued %d inflight %d, want 2/1", queued, inflight)
	}
	picks := backlogQueuedPicks(entries)
	if len(picks) != 2 {
		t.Fatalf("want 2 picks, got %d", len(picks))
	}
	// P0 (#3) ranks ahead of P1 (#1)
	if picks[0].Num != 3 || picks[1].Num != 1 {
		t.Errorf("pick order = %d,%d, want 3,1", picks[0].Num, picks[1].Num)
	}
}

func TestBacklogLedgerRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := "coilyco-flight-deck/ward"

	// absent ledger loads empty
	led, err := loadBacklogLedger(repo)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if led.Repo != repo || len(led.Issues) != 0 {
		t.Fatalf("fresh ledger = %+v", led)
	}

	led.Issues["42"] = &backlogEntry{Num: 42, Lane: "headless", State: "dispatched", Title: "carry me"}
	if err := saveBacklogLedger(led); err != nil {
		t.Fatalf("save: %v", err)
	}
	if led.Updated == "" {
		t.Errorf("save should stamp Updated")
	}

	// reload sees the persisted entry - the kill+resume path
	got, err := loadBacklogLedger(repo)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	e := got.Issues["42"]
	if e == nil || e.Num != 42 || e.State != "dispatched" || e.Title != "carry me" {
		t.Errorf("reloaded entry = %+v", e)
	}
}
