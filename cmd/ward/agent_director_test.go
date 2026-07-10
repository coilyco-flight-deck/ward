package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
	"github.com/urfave/cli/v3"
)

// TestDirectorDispatchDisposition covers ward#352 + ward#524: a coded per-issue decline
// parks `failed`; a reservation conflict or a launch-time infra failure stays `queued`.
func TestDirectorDispatchDisposition(t *testing.T) {
	// A reservation conflict defers (retryable once the holder finishes).
	conflict := newReservationConflict("issue a/b#5 is already reserved remotely")
	state, outcome, deferred := directorDispatchDisposition(conflict)
	if !deferred {
		t.Error("a reservation conflict must defer, not fail")
	}
	if state != "queued" {
		t.Errorf("deferred state = %q, want queued (eligible for a later tick)", state)
	}
	if outcome == nil || outcome.Status != "deferred" {
		t.Errorf("deferred outcome = %+v, want status=deferred", outcome)
	}

	// A launch-time infrastructure failure (the forgejo issue-fetch breakage that wedged
	// the director) must defer too - the issue was never judged and no run was spent.
	fetchErr := fmt.Errorf("forgejo: get issue a/b#5: %w", errors.New("502 bad gateway"))
	state, outcome, deferred = directorDispatchDisposition(fetchErr)
	if !deferred {
		t.Error("a launch-time infra failure must defer and retry, not park failed")
	}
	if state != "queued" {
		t.Errorf("infra-failure state = %q, want queued (retried on a later tick)", state)
	}
	if outcome == nil || outcome.Status != "deferred" {
		t.Errorf("infra-failure outcome = %+v, want status=deferred", outcome)
	}

	// An uncoded generic launch failure defers on the same reasoning.
	state, _, deferred = directorDispatchDisposition(errors.New("image pull failed"))
	if !deferred || state != "queued" {
		t.Errorf("generic launch failure: state=%q deferred=%v, want queued+deferred", state, deferred)
	}

	// Global engineer backpressure defers too. It is not a terminal dispatch failure.
	capacity := newEngineerCapacityError("ward agent engineer --harness codex", 10, 10)
	state, outcome, deferred = directorDispatchDisposition(capacity)
	if !deferred {
		t.Error("engineer capacity backpressure must defer, not fail")
	}
	if state != "queued" {
		t.Errorf("capacity state = %q, want queued", state)
	}
	if outcome == nil || outcome.Status != "deferred" {
		t.Errorf("capacity outcome = %+v, want status=deferred", outcome)
	}

	// A coded per-issue decline is a real verdict on the issue: park it terminal.
	noGo := dispatchDeclineErr(dispatchNoGo, "preflight_no_go", "issue a/b#5 is infeasible")
	state, outcome, deferred = directorDispatchDisposition(noGo)
	if deferred {
		t.Error("a coded NO-GO decline is a per-issue verdict, it must not defer")
	}
	if state != "failed" {
		t.Errorf("decline state = %q, want failed", state)
	}
	if outcome == nil || outcome.Status != "declined" {
		t.Errorf("decline outcome = %+v, want status=declined", outcome)
	}

	// Wrong-repo and untrusted-owner are likewise terminal per-issue/owner verdicts.
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"wrong-repo", dispatchDeclineErr(dispatchWrongRepo, "preflight_wrong_repo_routed", "routed to c/d")},
		{"untrusted-owner", exitcode.New(dispatchUntrustedOwner, "untrusted_owner", errors.New("owner not trusted"), "")},
	} {
		if _, _, def := directorDispatchDisposition(tc.err); def {
			t.Errorf("%s decline must park terminal, not defer", tc.name)
		}
	}
}

// TestDispatchEngineerArgv covers ward#355: each set flag is forwarded into the
// engineer argv, booleans only when true, --force only when the operator opted in.
func TestDispatchEngineerArgv(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42}

	// A bare dispatch: harness + headless detach + --quiet-seed (keeps the seed dump off
	// the director console; ward#519) + --skip-host-preflight, with no escalations.
	bare := dispatchEngineer{harness: modeClaude}.engineerArgv(ref)
	wantBare := []string{"engineer", "coilyco-flight-deck/ward#42", "--harness", "claude", "--quiet-seed", "--skip-host-preflight"}
	if !reflect.DeepEqual(bare, wantBare) {
		t.Errorf("bare argv = %v, want %v", bare, wantBare)
	}
	for _, unwanted := range []string{"--aws", "--tailnet", "--tailnet-mode", "--force", "--ward-version"} {
		if containsArg(bare, unwanted) {
			t.Errorf("bare argv should not carry %q: %v", unwanted, bare)
		}
	}
	hostDefault := dispatchEngineer{harness: modeClaude, wardVersion: "v0.463.0", wardVersionSource: wardVersionSourceHost}.engineerArgv(ref)
	if containsArg(hostDefault, "--ward-version") {
		t.Errorf("inherited host ward-version must not be forwarded: %v", hostDefault)
	}

	// A fully-loaded dispatch forwards the resolved container intent; a resolved host-net
	// route forwards as the consolidated --tailnet + an explicit --tailnet-mode (ward#362).
	full := dispatchEngineer{
		harness: modeGoose, image: "ghcr.io/x/dev", tag: "v9", wardVersion: "v0.58.0", wardVersionSource: wardVersionSourceExplicit,
		aws: true, hostNet: true, tsSidecar: false, force: true,
	}.engineerArgv(ref)
	for _, want := range [][2]string{
		{"--harness", "goose"}, {"--image", "ghcr.io/x/dev"}, {"--tag", "v9"},
		{"--ward-version", "v0.58.0"}, {"--tailnet-mode", "host-net"},
	} {
		if !argFollowedBy(full, want[0], want[1]) {
			t.Errorf("argv missing %s %s: %v", want[0], want[1], full)
		}
	}
	for _, want := range []string{"--aws", "--tailnet", "--force", "--quiet-seed"} {
		if !containsArg(full, want) {
			t.Errorf("argv missing %q: %v", want, full)
		}
	}
}

// TestDirectorDispatchQuietsSeedConsole covers ward#519: director forwards --quiet-seed
// so the in-process engineer's seed dump stays off the shared console, direct keeps it.
func TestDirectorDispatchQuietsSeedConsole(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 519}
	seed := agentSeedPrompt(ref, "quiet the seed", "the frozen task text", "", true, nil)

	// The director forwards --quiet-seed on every dispatch (ward#519).
	if !containsArg(dispatchEngineer{harness: modeClaude}.engineerArgv(ref), "--quiet-seed") {
		t.Fatal("director dispatch argv must carry --quiet-seed (ward#519)")
	}

	// Director auto-dispatch (quiet): nothing hits the shared console stream.
	var quiet strings.Builder
	maybeDumpSeed(&quiet, seed, true)
	if quiet.Len() != 0 {
		t.Errorf("quiet seed dump wrote to the shared console: %q", quiet.String())
	}

	// A direct `ward agent engineer <ref>` (not quiet): the ward#400 dump survives.
	var direct strings.Builder
	maybeDumpSeed(&direct, seed, false)
	for _, want := range []string{"----- seeded prompt -----", "the frozen task text", "closes #519"} {
		if !strings.Contains(direct.String(), want) {
			t.Errorf("direct seed dump missing %q\n got: %s", want, direct.String())
		}
	}
}

// TestDirectorEngineerHarness covers the two-level precedence (ward#355): set
// --engineer-harness wins; --engineer-driver remains a hidden fallback alias.
func TestDirectorEngineerHarness(t *testing.T) {
	inherit := directorFlagSet(t, map[string]string{})
	if got, err := directorEngineerHarness(inherit, modeGoose); err != nil || got != modeGoose {
		t.Errorf("unset --engineer-harness should inherit director's mode: got %q err %v", got, err)
	}
	override := directorFlagSet(t, map[string]string{"engineer-harness": "codex"})
	if got, err := directorEngineerHarness(override, modeGoose); err != nil || got != modeCodex {
		t.Errorf("--engineer-harness codex should override: got %q err %v", got, err)
	}
	alias := directorFlagSet(t, map[string]string{"engineer-driver": "codex"})
	if got, err := directorEngineerHarness(alias, modeGoose); err != nil || got != modeCodex {
		t.Errorf("hidden --engineer-driver codex should still resolve: got %q err %v", got, err)
	}
	both := directorFlagSet(t, map[string]string{"engineer-harness": "codex", "engineer-driver": "goose"})
	if got, err := directorEngineerHarness(both, modeGoose); err != nil || got != modeCodex {
		t.Errorf("canonical --engineer-harness should win over alias: got %q err %v", got, err)
	}
	bad := directorFlagSet(t, map[string]string{"engineer-harness": "nope"})
	if _, err := directorEngineerHarness(bad, modeClaude); err == nil {
		t.Error("an unknown --engineer-harness must error")
	}
}

// TestDirectorFlagsParity covers ward#355's acceptance: director carries the shared
// container/harness flags at parity, but never the engineer / detach specifics.
func TestDirectorFlagsParity(t *testing.T) {
	cmd := agentDirectorCommand()
	for _, want := range []string{
		"image", "tag", "ward-source", "ward-version", "aws", "tailnet", "tailnet-mode",
		"no-pull", "print", "with-repo", "force", "engineer-harness", "engineer-driver",
	} {
		if !commandHasFlag(cmd, want) {
			t.Errorf("ward agent director missing --%s at parity (ward#355)", want)
		}
	}
	for _, f := range cmd.Flags {
		sf, ok := f.(*cli.StringFlag)
		if !ok {
			continue
		}
		switch sf.Name {
		case "engineer-harness":
			if sf.Hidden {
				t.Error("--engineer-harness is hidden; want it visible")
			}
		case "engineer-driver":
			if !sf.Hidden {
				t.Error("--engineer-driver alias is visible; want it hidden")
			}
		}
	}
	for _, unwanted := range []string{"branch", "no-preflight", "watch", "detach"} {
		if commandHasFlag(cmd, unwanted) {
			t.Errorf("ward agent director must NOT add --%s (ward#355)", unwanted)
		}
	}
}

// containsArg reports whether argv holds the literal flag token.
func containsArg(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}

// argFollowedBy reports whether flag appears immediately before val in argv.
func argFollowedBy(argv []string, flag, val string) bool {
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == flag && argv[i+1] == val {
			return true
		}
	}
	return false
}

// directorFlagSet parses director's flags with the given string flags set, so the harness
// resolvers can be exercised without a full run.
func directorFlagSet(t *testing.T, set map[string]string) *cli.Command {
	t.Helper()
	cmd := &cli.Command{Name: "director", Flags: directorFlags()}
	args := []string{"director"}
	for k, v := range set {
		args = append(args, "--"+k, v)
	}
	if err := cmd.Run(t.Context(), args); err != nil {
		// A nil Action means Run just parses; an error here is a real parse fault.
		t.Fatalf("parse director flags %v: %v", set, err)
	}
	return cmd
}

// TestDirectorSurfaceArgv covers ward#355: director's drain surface inherits its
// container/harness flags and runs on director's OWN harness, never the engineer harness.
func TestDirectorSurfaceArgv(t *testing.T) {
	t.Run("inherited host version is not forwarded", func(t *testing.T) {
		cfg := backlogConfig{
			mode: modeClaude,
			dispatch: dispatchEngineer{
				harness:           modeGoose,
				image:             "img",
				tag:               "t1",
				wardVersion:       "v1",
				wardVersionSource: wardVersionSourceHost,
				aws:               true,
				tsSidecar:         true,
			},
			wardSource: "/src/ward",
			noPull:     true,
			withRepo:   []string{"a/b", "c/d"},
		}
		argv := directorSurfaceArgv("coilyco-flight-deck/ward", cfg)
		if argv[0] != directorSurfaceVerb {
			t.Errorf("surface argv[0] = %q, want %q", argv[0], directorSurfaceVerb)
		}
		if !argFollowedBy(argv, "--harness", "claude") {
			t.Errorf("surface must run on director's own harness (claude), not the engineer harness: %v", argv)
		}
		for _, want := range [][2]string{
			{"--repo", "coilyco-flight-deck/ward"}, {"--image", "img"}, {"--tag", "t1"},
			{"--ward-source", "/src/ward"},
			{"--with-repo", "a/b"}, {"--with-repo", "c/d"},
		} {
			if !argFollowedBy(argv, want[0], want[1]) {
				t.Errorf("surface argv missing %s %s: %v", want[0], want[1], argv)
			}
		}
		for _, want := range []string{"--aws", "--tailnet", "--no-pull"} {
			if !containsArg(argv, want) {
				t.Errorf("surface argv missing %q: %v", want, argv)
			}
		}
		if containsArg(argv, "--ward-version") {
			t.Errorf("surface argv must not forward an inherited host ward-version pin: %v", argv)
		}
		if !argFollowedBy(argv, "--tailnet-mode", "sidecar") {
			t.Errorf("surface argv must forward the resolved sidecar mechanism as --tailnet-mode sidecar: %v", argv)
		}
		if containsArg(argv, "goose") {
			t.Errorf("surface argv must not carry the engineer harness: %v", argv)
		}
	})

	t.Run("explicit pin is forwarded", func(t *testing.T) {
		cfg := backlogConfig{
			mode: modeClaude,
			dispatch: dispatchEngineer{
				harness:           modeGoose,
				wardVersion:       "v1",
				wardVersionSource: wardVersionSourceExplicit,
			},
		}
		argv := directorSurfaceArgv("coilyco-flight-deck/ward", cfg)
		if !argFollowedBy(argv, "--ward-version", "v1") {
			t.Fatalf("surface argv must forward an explicit ward-version pin: %v", argv)
		}
	})
}

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

func TestPartitionScopeEntries(t *testing.T) {
	cases := []struct {
		name      string
		entries   []string
		wantOrgs  []string
		wantRepos []string
	}{
		{"orgs only", []string{"coilyco-flight-deck", "coilyco-bridge"}, []string{"coilyco-flight-deck", "coilyco-bridge"}, nil},
		{"repos only", []string{"a/b", "c/d"}, nil, []string{"a/b", "c/d"}},
		{"mixed, order-preserving", []string{"a/b", "org", "c/d"}, []string{"org"}, []string{"a/b", "c/d"}},
		{"de-dupes and trims blanks", []string{" org ", "org", "", "a/b", "a/b"}, []string{"org"}, []string{"a/b"}},
		{"empty", nil, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotOrgs, gotRepos := partitionScopeEntries(c.entries)
			if !reflect.DeepEqual(gotOrgs, c.wantOrgs) {
				t.Errorf("partitionScopeEntries(%v) orgs = %v, want %v", c.entries, gotOrgs, c.wantOrgs)
			}
			if !reflect.DeepEqual(gotRepos, c.wantRepos) {
				t.Errorf("partitionScopeEntries(%v) repos = %v, want %v", c.entries, gotRepos, c.wantRepos)
			}
		})
	}
}

// TestLoadDirectorDefaultScope covers ward#398: the config-stored fallback scope is read
// from ~/.ward/config.yaml, partitioned into orgs vs repos; a missing file is no error.
func TestLoadDirectorDefaultScope(t *testing.T) {
	t.Run("missing file yields empties, no error", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		orgs, repos, err := loadDirectorDefaultScope()
		if err != nil {
			t.Fatalf("loadDirectorDefaultScope() unexpected error: %v", err)
		}
		if len(orgs) != 0 || len(repos) != 0 {
			t.Errorf("missing config should yield empty scope, got orgs=%v repos=%v", orgs, repos)
		}
	})
	t.Run("orgs and bare repos partitioned", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, ".ward"), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		yaml := "director:\n  default-scope: [coilyco-flight-deck, coilyco-bridge, some/repo]\n"
		if err := os.WriteFile(filepath.Join(home, ".ward", "config.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		orgs, repos, err := loadDirectorDefaultScope()
		if err != nil {
			t.Fatalf("loadDirectorDefaultScope() error: %v", err)
		}
		if want := []string{"coilyco-flight-deck", "coilyco-bridge"}; !reflect.DeepEqual(orgs, want) {
			t.Errorf("orgs = %v, want %v", orgs, want)
		}
		if want := []string{"some/repo"}; !reflect.DeepEqual(repos, want) {
			t.Errorf("repos = %v, want %v", repos, want)
		}
	})
}

// TestDirectorHasOrgFlag covers ward#370: director takes a repeatable --org scope flag.
func TestDirectorHasOrgFlag(t *testing.T) {
	if !commandHasFlag(agentDirectorCommand(), "org") {
		t.Errorf("ward agent director missing --org scope flag (ward#370)")
	}
}

func TestMergeScopeRepos(t *testing.T) {
	cases := []struct {
		name  string
		lists [][]string
		want  []string
	}{
		{"explicit then org, union", [][]string{{"a/b"}, {"a/c", "a/d"}}, []string{"a/b", "a/c", "a/d"}},
		{"de-dupes across lists, order-preserving", [][]string{{"a/b", "a/c"}, {"a/c", "a/b", "a/e"}}, []string{"a/b", "a/c", "a/e"}},
		{"trims blanks", [][]string{{" a/b ", ""}, {"a/c"}}, []string{"a/b", "a/c"}},
		{"all empty", [][]string{nil, {}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mergeScopeRepos(c.lists...)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("mergeScopeRepos(%v) = %v, want %v", c.lists, got, c.want)
			}
		})
	}
}

func TestOrgReposToSlugs(t *testing.T) {
	repos := []repoBrief{
		{Name: "live"},
		{Name: "stale", Archived: true},
		{Name: "blank", Empty: true},
		{Name: "ward"},
	}
	got := orgReposToSlugs("coilyco-flight-deck", repos)
	want := []string{"coilyco-flight-deck/live", "coilyco-flight-deck/ward"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("orgReposToSlugs dropped/kept wrong repos = %v, want %v", got, want)
	}
	if s := orgReposToSlugs("org", []repoBrief{{Name: "x", Archived: true}}); len(s) != 0 {
		t.Errorf("orgReposToSlugs(all archived) = %v, want empty", s)
	}
}

func TestBacklogLaneForLabels(t *testing.T) {
	cases := []struct {
		kind   string
		labels []string
		tier   string
		mode   string
		lane   string
	}{
		{backlogKindIssue, []string{"P0", "headless"}, "P0", "headless", "headless"},
		{backlogKindIssue, []string{"P2", "interactive"}, "P2", "interactive", "interactive"},
		{backlogKindIssue, []string{"P1", "consult"}, "P1", "consult", "consult"},
		{backlogKindIssue, []string{"headless"}, "", "headless", "untriaged"},              // no tier -> untriaged
		{backlogKindIssue, []string{"P3"}, "P3", "", "untriaged"},                          // no mode -> untriaged
		{backlogKindIssue, []string{"unrelated", "label"}, "", "", "untriaged"},            // neither
		{backlogKindIssue, []string{"P4", "P0", "headless"}, "P0", "headless", "headless"}, // highest tier wins
		{backlogKindPullRequest, []string{"P0", "headless"}, "P0", "headless", backlogKindPullRequest},
	}
	for _, c := range cases {
		tier := backlogTierOf(c.labels)
		mode := backlogModeOf(c.labels)
		lane := backlogLaneForKind(c.kind, tier, mode)
		if tier != c.tier || mode != c.mode || lane != c.lane {
			t.Errorf("kind=%q labels %v => tier=%q mode=%q lane=%q, want tier=%q mode=%q lane=%q",
				c.kind, c.labels, tier, mode, lane, c.tier, c.mode, c.lane)
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
		{Number: 40, Kind: backlogKindPullRequest, Title: "P0 PR", Labels: []string{"P0", "headless"}},
	}
	got := rankBacklogIssues(issues)
	wantOrder := []int{5, 7, 20, 40, 30, 10} // headless by tier, then PRs, then interactive, then untriaged
	var gotOrder []int
	for _, r := range got {
		gotOrder = append(gotOrder, r.Num)
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("rank order = %v, want %v", gotOrder, wantOrder)
	}
	if got[0].Lane != "headless" || got[3].Lane != backlogKindPullRequest || got[4].Lane != "interactive" || got[5].Lane != "untriaged" {
		t.Errorf("lane assignment wrong: %+v", got)
	}
}

func TestRefreshBacklogLedger(t *testing.T) {
	led := &backlogLedger{Repo: "a/b", Issues: map[string]*backlogEntry{
		// already dispatched: must be preserved, not reset to queued
		"5": {Num: 5, Lane: "headless", State: "dispatched", Container: "engineer-claude-b-5"},
		// previously surfaced (interactive), now re-triaged into headless -> promote to queued
		"7": {Num: 7, Lane: "interactive", State: "surfaced"},
		// PRs should stay visible as surfaced follow-up work.
		"8": {Num: 8, Kind: backlogKindPullRequest, Lane: backlogKindPullRequest, State: "surfaced"},
		// a done issue that has since closed (absent from the live set) -> dropped
		"9": {Num: 9, Lane: "headless", State: "done"},
		// ward#527: a pre-#524 dispatch-error stranding -> re-queued, outcome cleared
		"13": {Num: 13, Lane: "headless", State: "failed", LastOutcome: &backlogOutcome{Status: "dispatch-error", Text: "forgejo: get issue a/b#13: 502"}},
		// a genuine per-issue decline (declined) must NOT be re-queued
		"14": {Num: 14, Lane: "headless", State: "failed", LastOutcome: &backlogOutcome{Status: "declined", Text: "infeasible"}},
	}}
	ranked := rankBacklogIssues([]backlogIssue{
		{Number: 5, Title: "five", Labels: []string{"P0", "headless"}},
		{Number: 7, Title: "seven", Labels: []string{"P1", "headless"}}, // promoted to headless
		{Number: 8, Kind: backlogKindPullRequest, Title: "eight", Labels: []string{"P0", "headless"}},
		{Number: 11, Title: "eleven", Labels: []string{"P2", "interactive"}},
		{Number: 12, Title: "twelve", Labels: nil}, // untriaged
		{Number: 13, Title: "thirteen", Labels: []string{"P0", "headless"}},
		{Number: 14, Title: "fourteen", Labels: []string{"P0", "headless"}},
	})
	refreshBacklogLedger(led, ranked)

	if e := led.Issues["5"]; e == nil || e.State != "dispatched" {
		t.Errorf("#5 should stay dispatched, got %+v", e)
	}
	if e := led.Issues["7"]; e == nil || e.State != "queued" || e.Lane != "headless" {
		t.Errorf("#7 should be promoted to queued/headless, got %+v", e)
	}
	if e := led.Issues["8"]; e == nil || e.State != "surfaced" || e.Lane != backlogKindPullRequest || e.Kind != backlogKindPullRequest {
		t.Errorf("#8 PR should stay surfaced/pull-request, got %+v", e)
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
	if e := led.Issues["13"]; e == nil || e.State != "queued" || e.LastOutcome != nil {
		t.Errorf("#13 stranded dispatch-error should be re-queued with a cleared outcome, got %+v", e)
	}
	if e := led.Issues["14"]; e == nil || e.State != "failed" {
		t.Errorf("#14 genuine decline must stay failed, got %+v", e)
	}
}

// TestBacklogRefreshUsesForgejoTokenForPrivateRepos covers the director startup
// refresh path for private Forgejo repos.
func TestBacklogRefreshUsesForgejoTokenForPrivateRepos(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FORGEJO_TOKEN", "secret")

	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token secret" {
			t.Fatalf("auth header = %q, want token secret", got)
		}
		switch {
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues" && r.URL.Query().Get("type") == "issues":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 11, "title": "private issue", "body": "body", "state": "open", "html_url": "https://f/issues/11", "labels": []map[string]any{}},
			})
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues" && r.URL.Query().Get("type") == "pulls":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 12, "title": "private pr", "body": "closes #12", "state": "open", "html_url": "https://f/pulls/12", "labels": []map[string]any{}},
			})
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/pulls/12":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 12, "mergeable": true})
		default:
			t.Fatalf("unexpected path: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()
	forgejoBaseURL = srv.URL

	if err := (&Runner{}).backlogRefresh(t.Context(), "director", []string{"coilyco-flight-deck/ward"}, 50); err != nil {
		t.Fatalf("backlogRefresh: %v", err)
	}
}

func TestCombineOpenBacklogIssues(t *testing.T) {
	issues := []backlogIssue{{Number: 5, Kind: backlogKindIssue, Title: "issue"}}
	prs := []backlogIssue{{Number: 8, Title: "pr"}}

	got := combineOpenBacklogIssues(issues, prs)
	if len(got) != 2 {
		t.Fatalf("combined length = %d, want 2", len(got))
	}
	if got[0].Kind != backlogKindIssue || got[0].Number != 5 {
		t.Fatalf("issue row was not preserved: %+v", got[0])
	}
	if got[1].Kind != backlogKindPullRequest || got[1].Number != 8 {
		t.Fatalf("PR row was not tagged/folded in: %+v", got[1])
	}
}

func TestBacklogPrintStatusDistinguishesPRs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := "a/b"
	led := &backlogLedger{Repo: repo, Issues: map[string]*backlogEntry{
		"5": {Num: 5, Kind: backlogKindIssue, Lane: "headless", State: "queued", Title: "issue work"},
		"8": {Num: 8, Kind: backlogKindPullRequest, Lane: backlogKindPullRequest, State: "surfaced", Title: "PR follow-up"},
	}}
	if err := saveBacklogLedger(led); err != nil {
		t.Fatalf("save ledger: %v", err)
	}
	out := &bytes.Buffer{}
	r := &Runner{Runner: &shell.Runner{Stdout: out}}
	if err := r.backlogPrintStatus([]string{repo}); err != nil {
		t.Fatalf("backlogPrintStatus: %v", err)
	}
	got := out.String()
	for _, want := range []string{"issue a/b#5", "PR a/b#8", "pull-request lane"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q\n%s", want, got)
		}
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
			name:       "submitted leading line",
			comments:   []issueComment{{Body: "WARD-OUTCOME: submitted - PR opened, waiting for human merge\n\nfelt calm", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "submitted",
			wantText:   "PR opened, waiting for human merge",
		},
		{
			name:       "merge-ready leading line",
			comments:   []issueComment{{Body: "WARD-OUTCOME: merge-ready - review passed, handoff to director\n\nfelt calm", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "merge-ready",
			wantText:   "review passed, handoff to director",
		},
		{
			name:       "pending aliases to submitted",
			comments:   []issueComment{{Body: "WARD-OUTCOME: pending - PR opened, waiting for human merge\n\nfelt calm", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "submitted",
			wantText:   "PR opened, waiting for human merge",
		},
		{
			name:       "ready-for-merge aliases to merge-ready",
			comments:   []issueComment{{Body: "WARD-OUTCOME: ready-for-merge - review passed, handoff to director\n\nfelt calm", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "merge-ready",
			wantText:   "review passed, handoff to director",
		},
		{
			name:       "bare emoji line",
			comments:   []issueComment{{Body: "WARD-OUTCOME: done ✅\n\n<details><summary>details</summary>\n\nmerged and pushed\n\n</details>", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "done",
			wantText:   "",
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
		"done":            "done",
		"failed":          "failed",
		"blocked":         "blocked",
		"submitted":       "submitted",
		"merge-ready":     "merge-ready",
		"pending":         "submitted",
		"ready-for-merge": "merge-ready",
		"unknown":         "blocked", // unrecognized parks as blocked
		"weird":           "blocked",
	}
	for in, want := range cases {
		if got := backlogOutcomeState(in); got != want {
			t.Errorf("backlogOutcomeState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBacklogSummarySurfacesPRStates(t *testing.T) {
	repo := "a/b"
	led := &backlogLedger{
		Repo: repo,
		Issues: map[string]*backlogEntry{
			"1": {Num: 1, Lane: "headless", State: "submitted", Title: "submitted issue", repo: repo},
			"2": {Num: 2, Lane: "headless", State: "merge-ready", Title: "merge-ready issue", repo: repo},
		},
	}
	if err := saveBacklogLedger(led); err != nil {
		t.Fatalf("save ledger: %v", err)
	}
	var out bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stdout: &out}}
	if err := r.backlogPrintSummary([]string{repo}); err != nil {
		t.Fatalf("backlogPrintSummary: %v", err)
	}
	got := out.String()
	for _, want := range []string{"submitted", "merge-ready"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q\n%s", want, got)
		}
	}
}

// TestReconcileNoOutcome pins the ward#595 pre-launch-death classification: a released
// reservation re-queues (bounded), a bare no-outcome exit stays terminal `failed`.
func TestReconcileNoOutcome(t *testing.T) {
	at := func(s string) time.Time {
		ts, _ := time.Parse(time.RFC3339, s)
		return ts
	}
	dispatched := at("2026-07-04T04:39:45Z")
	release := issueComment{Body: agentReservationReleaseMarker + "\n" + agentNeedsRedispatchMarker + "\nrun never started", CreatedAt: at("2026-07-04T04:39:52Z")}
	chatter := issueComment{Body: "just a comment", CreatedAt: at("2026-07-04T04:40:00Z")}

	// No release marker: the agent launched and vanished - stays terminal failed.
	state, oc, attempts := reconcileNoOutcome([]issueComment{chatter}, dispatched, 0)
	if state != "failed" || oc.Status != "exited-no-outcome" || attempts != 0 {
		t.Fatalf("no-release: got %s/%s attempts=%d, want failed/exited-no-outcome/0", state, oc.Status, attempts)
	}

	// A release stamped at/after dispatch is a pre-launch death: re-queue, count the try.
	state, oc, attempts = reconcileNoOutcome([]issueComment{release}, dispatched, 0)
	if state != "queued" || oc.Status != "prelaunch-death-requeued" || attempts != 1 {
		t.Fatalf("first death: got %s/%s attempts=%d, want queued/prelaunch-death-requeued/1", state, oc.Status, attempts)
	}

	// A release stamped BEFORE this dispatch is a stale marker from a prior attempt, not
	// this run's death: it must not re-queue a run that actually launched.
	stale := issueComment{Body: agentReservationReleaseMarker, CreatedAt: at("2026-07-04T04:00:00Z")}
	if state, _, _ := reconcileNoOutcome([]issueComment{stale}, dispatched, 0); state != "failed" {
		t.Fatalf("stale release: got %s, want failed", state)
	}

	// The cap is bounded: the attempt that reaches redispatchAttemptCap parks blocked
	// with the explicit orphaned marker instead of re-queuing forever.
	state, oc, attempts = reconcileNoOutcome([]issueComment{release}, dispatched, redispatchAttemptCap-1)
	if state != "blocked" || oc.Status != "orphaned-needs-redispatch" || attempts != redispatchAttemptCap {
		t.Fatalf("cap: got %s/%s attempts=%d, want blocked/orphaned-needs-redispatch/%d", state, oc.Status, attempts, redispatchAttemptCap)
	}
}

// TestPrelaunchDeathRelease covers the marker/timestamp fingerprint, including the
// unknown-dispatch-time (zero) case where any release marker counts.
func TestPrelaunchDeathRelease(t *testing.T) {
	at := func(s string) time.Time {
		ts, _ := time.Parse(time.RFC3339, s)
		return ts
	}
	rel := func(ts string) issueComment {
		return issueComment{Body: agentReservationReleaseMarker, CreatedAt: at(ts)}
	}
	if prelaunchDeathRelease([]issueComment{{Body: "no marker", CreatedAt: at("2026-07-04T05:00:00Z")}}, at("2026-07-04T04:00:00Z")) {
		t.Error("a thread with no release marker is not a pre-launch death")
	}
	if !prelaunchDeathRelease([]issueComment{rel("2026-07-04T05:00:00Z")}, at("2026-07-04T04:00:00Z")) {
		t.Error("a release after dispatch is a pre-launch death")
	}
	if prelaunchDeathRelease([]issueComment{rel("2026-07-04T03:00:00Z")}, at("2026-07-04T04:00:00Z")) {
		t.Error("a release before dispatch is stale, not this run's death")
	}
	// Unknown dispatch time (zero): any release marker still counts as a pre-launch death.
	if !prelaunchDeathRelease([]issueComment{rel("2026-07-04T05:00:00Z")}, time.Time{}) {
		t.Error("with an unknown dispatch time, a release marker still counts")
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
