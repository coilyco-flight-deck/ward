package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func TestDirectorHelpNamesInteractiveStartup(t *testing.T) {
	description := agentDirectorCommand().Description
	for _, want := range []string{
		"Use --burndown to run the autonomous heartbeat",
		"warded director --burndown --repo coilyco-flight-deck/ward",
		"press Enter during the sleep offer",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("director help missing %q:\n%s", want, description)
		}
	}

	rootDescription := agentCommand().Description
	if !strings.Contains(rootDescription, "add --burndown when you want autonomous dispatch") {
		t.Errorf("ward agent help should name the director interactive startup path:\n%s", rootDescription)
	}
}

func TestDirectorNeedsLiveBacklog(t *testing.T) {
	ref := &agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 5}
	cases := []struct {
		name string
		cfg  backlogConfig
		want bool
	}{
		{"plain interactive", backlogConfig{}, false},
		{"burndown", backlogConfig{burndown: true}, true},
		{"startup triage", backlogConfig{triage: true}, true},
		{"issue scoped", backlogConfig{issueRef: ref}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := directorNeedsLiveBacklog(tc.cfg); got != tc.want {
				t.Fatalf("directorNeedsLiveBacklog(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}

func TestDirectorStartupBannerPlain(t *testing.T) {
	steps := []directorStartupStep{
		{Category: "inventory", Detail: "print the current backlog snapshot"},
		{Category: "refresh", Detail: "refresh the live backlog before opening the surface"},
		{Category: "surface", Detail: "open the read-only director session"},
	}
	got := directorStartupBanner("ward agent director", steps, false)
	for _, want := range []string{
		"ward agent director startup:",
		"INVENTORY:",
		"REFRESH:",
		"SURFACE:",
		"print the current backlog snapshot",
		"refresh the live backlog before opening the surface",
		"open the read-only director session",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("startup banner missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("plain startup banner should not contain ANSI escapes:\n%s", got)
	}
}

func TestDirectorStartupBannerColorsCategories(t *testing.T) {
	got := directorStartupCategoryLabel("refresh", true)
	if !strings.Contains(got, "\x1b[34;1mREFRESH:\x1b[0m") {
		t.Fatalf("colored refresh label = %q, want cyan bold ANSI", got)
	}
}

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

	// Open-PR backpressure is also a retryable deferral.
	backpressure := newOpenPRBackpressureError("ward agent engineer", 7, 6)
	state, outcome, deferred = directorDispatchDisposition(backpressure)
	if !deferred {
		t.Error("open-PR backpressure must defer, not fail")
	}
	if state != "queued" {
		t.Errorf("backpressure state = %q, want queued", state)
	}
	if outcome == nil || outcome.Status != "deferred" {
		t.Errorf("backpressure outcome = %+v, want status=deferred", outcome)
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
// engineer argv; --override-reservation only when the operator opted in (ward#1045).
func TestDispatchEngineerArgv(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42}

	// A bare dispatch: harness + headless detach + --quiet-seed (keeps the seed dump off
	// the director console; ward#519) + --skip-host-preflight, with no escalations.
	bare := dispatchEngineer{harness: modeClaude}.engineerArgv(ref)
	wantBare := []string{"engineer", "coilyco-flight-deck/ward#42", "--harness", "claude", "--quiet-seed", "--skip-host-preflight"}
	if !reflect.DeepEqual(bare, wantBare) {
		t.Errorf("bare argv = %v, want %v", bare, wantBare)
	}
	for _, unwanted := range []string{"--override-reservation", "--override-capacity", "--ward-version"} {
		if containsArg(bare, unwanted) {
			t.Errorf("bare argv should not carry %q: %v", unwanted, bare)
		}
	}
	hostDefault := dispatchEngineer{harness: modeClaude, wardVersion: "v0.463.0", wardVersionSource: wardVersionSourceHost}.engineerArgv(ref)
	if containsArg(hostDefault, "--ward-version") {
		t.Errorf("inherited host ward-version must not be forwarded: %v", hostDefault)
	}

	// A fully-loaded dispatch forwards only the explicit launch knobs.
	full := dispatchEngineer{
		harness: modeGoose, image: "ghcr.io/x/dev", tag: "v9", wardVersion: "v0.58.0", wardVersionSource: wardVersionSourceExplicit,
		overrideReservation: true,
	}.engineerArgv(ref)
	for _, want := range [][2]string{
		{"--harness", "goose"}, {"--image", "ghcr.io/x/dev"}, {"--tag", "v9"},
		{"--ward-version", "v0.58.0"},
	} {
		if !argFollowedBy(full, want[0], want[1]) {
			t.Errorf("argv missing %s %s: %v", want[0], want[1], full)
		}
	}
	for _, want := range []string{"--override-reservation", "--quiet-seed"} {
		if !containsArg(full, want) {
			t.Errorf("argv missing %q: %v", want, full)
		}
	}
	// The dispatched engineer never inherits the deprecated spelling or a
	// capacity override (ward#1045): capacity is per-launch, not a director knob.
	for _, unwanted := range []string{"--override-capacity"} {
		if containsArg(full, unwanted) {
			t.Errorf("argv must not carry %q: %v", unwanted, full)
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

// TestBacklogPrintDirectorPlanIncludesCLIConfig covers the print path's explicit
// CLI/config summary and the defaults that previously vanished from the launch plan.
func TestBacklogPrintDirectorPlanIncludesCLIConfig(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+t.TempDir())

	var out bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stdout: &out, Stderr: io.Discard}}
	cfg := backlogConfig{
		mode:         modeClaude,
		maxParallel:  4,
		limit:        17,
		pollInterval: 45 * time.Second,
		maxCycles:    12,
		dispatch: dispatchEngineer{
			harness:             modeGoose,
			image:               "img",
			tag:                 "tag",
			overrideReservation: true,
		},
		withRepo:   []string{"a/b"},
		noPull:     true,
		wardSource: "/src/ward",
	}

	if err := r.backlogPrintDirectorPlan("ward agent director", []string{"coilyco-flight-deck/ward"}, cfg); err != nil {
		t.Fatalf("backlogPrintDirectorPlan: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"config source:   file://",
		"limit:           17",
		"max-parallel:    4",
		"poll-interval:   45s",
		"max-cycles:      12",
		"engineer-harness: goose",
		"ward-source:     /src/ward",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("print output missing %q\n---\n%s", want, got)
		}
	}
}

// TestDirectorEngineerHarness covers the two-level precedence (ward#355):
// --engineer-harness wins and falls back to the director mode otherwise.
func TestDirectorEngineerHarness(t *testing.T) {
	inherit := directorFlagSet(t, map[string]string{})
	if got, err := directorEngineerHarness(inherit, modeGoose); err != nil || got != modeGoose {
		t.Errorf("unset --engineer-harness should inherit director's mode: got %q err %v", got, err)
	}
	override := directorFlagSet(t, map[string]string{"engineer-harness": "codex"})
	if got, err := directorEngineerHarness(override, modeGoose); err != nil || got != modeCodex {
		t.Errorf("--engineer-harness codex should override: got %q err %v", got, err)
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
		"image", "tag", "ward-source", "ward-version",
		"no-pull", "print", "with-repo", "override-reservation", "engineer-harness",
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
		}
	}
	for _, unwanted := range []string{"branch", "no-preflight", "watch", "detach", "override-capacity"} {
		if commandHasFlag(cmd, unwanted) {
			t.Errorf("ward agent director must NOT add --%s (ward#355, ward#1045)", unwanted)
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
		for _, want := range []string{"--no-pull"} {
			if !containsArg(argv, want) {
				t.Errorf("surface argv missing %q: %v", want, argv)
			}
		}
		if containsArg(argv, "--ward-version") {
			t.Errorf("surface argv must not forward an inherited host ward-version pin: %v", argv)
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

func TestResolveDirectorScopeUsesConfigWithoutGitCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("COILY_INVOKE_CWD", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, ".ward"), 0o750); err != nil {
		t.Fatalf("mkdir ~/.ward: %v", err)
	}
	yaml := "director:\n  default-scope: [coilyco-flight-deck/ward, some/repo]\n"
	if err := os.WriteFile(filepath.Join(home, ".ward", "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := &cli.Command{Name: "director", Flags: directorFlags(), Action: func(context.Context, *cli.Command) error { return nil }}
	if err := cmd.Run(t.Context(), []string{"director"}); err != nil {
		t.Fatalf("parse director args: %v", err)
	}
	r := &Runner{Runner: &shell.Runner{Resolve: func(bin string) (string, error) {
		t.Fatalf("resolveDirectorScope must not probe %q when config default-scope is present", bin)
		return "", fmt.Errorf("unexpected binary %q", bin)
	}}}
	got, err := r.resolveDirectorScope(t.Context(), cmd, "ward agent director")
	if err != nil {
		t.Fatalf("resolveDirectorScope: %v", err)
	}
	want := []string{"coilyco-flight-deck/ward", "some/repo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved scope = %v, want %v", got, want)
	}
}

func TestResolveDirectorScopeWithoutConfigDoesNotProbeGit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("COILY_INVOKE_CWD", t.TempDir())

	cmd := &cli.Command{Name: "director", Flags: directorFlags(), Action: func(context.Context, *cli.Command) error { return nil }}
	if err := cmd.Run(t.Context(), []string{"director"}); err != nil {
		t.Fatalf("parse director args: %v", err)
	}
	r := &Runner{Runner: &shell.Runner{Resolve: func(bin string) (string, error) {
		t.Fatalf("resolveDirectorScope must not probe %q when no scope is configured", bin)
		return "", fmt.Errorf("unexpected binary %q", bin)
	}}}
	_, err := r.resolveDirectorScope(t.Context(), cmd, "ward agent director")
	if err == nil || !strings.Contains(err.Error(), "no --repo/--org given and no director.default-scope in ~/.ward/config.yaml") {
		t.Fatalf("resolveDirectorScope error = %v, want missing config/default-scope message", err)
	}
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

type closedIssueRefreshTracker struct {
	issues   map[int]*Issue
	comments map[int][]issueComment
}

func (f *closedIssueRefreshTracker) GetIssue(_ context.Context, _, _ string, number int) (*Issue, error) {
	if issue, ok := f.issues[number]; ok {
		return issue, nil
	}
	return nil, fmt.Errorf("missing issue %d", number)
}

func (f *closedIssueRefreshTracker) ListIssueComments(_ context.Context, _, _ string, number int) ([]issueComment, error) {
	return append([]issueComment(nil), f.comments[number]...), nil
}

func (f *closedIssueRefreshTracker) CreateIssue(context.Context, string, string, string, string) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (f *closedIssueRefreshTracker) CommentIssue(context.Context, string, string, int, string) error {
	return fmt.Errorf("not implemented")
}

func (f *closedIssueRefreshTracker) DeleteIssueComment(context.Context, string, string, int) error {
	return fmt.Errorf("not implemented")
}

func (f *closedIssueRefreshTracker) CloseIssue(context.Context, string, string, int) error {
	return fmt.Errorf("not implemented")
}

func (f *closedIssueRefreshTracker) ReopenIssue(context.Context, string, string, int) error {
	return fmt.Errorf("not implemented")
}

func (f *closedIssueRefreshTracker) LockIssue(context.Context, string, string, int) error {
	return fmt.Errorf("not implemented")
}

func (f *closedIssueRefreshTracker) UnlockIssue(context.Context, string, string, int) error {
	return fmt.Errorf("not implemented")
}

func TestBacklogRefreshClosedIssueStates(t *testing.T) {
	r := &Runner{}
	led := &backlogLedger{Repo: "a/b", Issues: map[string]*backlogEntry{
		"5": {Num: 5, Kind: backlogKindIssue, Lane: "headless", State: "safe-to-redispatch"},
		"6": {Num: 6, Kind: backlogKindIssue, Lane: "headless", State: "queued"},
		"7": {Num: 7, Kind: backlogKindIssue, Lane: "headless", State: backlogReservationWaitingReaper},
	}}
	tracker := &closedIssueRefreshTracker{
		issues: map[int]*Issue{
			5: {Number: 5, State: "closed"},
			6: {Number: 6, State: "open"},
			7: {Number: 7, State: "closed"},
		},
	}

	if got := r.backlogRefreshClosedIssueStates(context.Background(), tracker, "a/b", led); !got {
		t.Fatal("closed issue refresh should report a change")
	}

	if e := led.Issues["5"]; e == nil || e.State != "blocked" || e.LastOutcome == nil || e.LastOutcome.Status != "issue-closed" {
		t.Fatalf("closed stale issue should be blocked, got %+v", e)
	}
	if e := led.Issues["6"]; e == nil || e.State != "queued" {
		t.Fatalf("open stale issue should stay queued, got %+v", e)
	}
	if e := led.Issues["7"]; e == nil || e.State != backlogReservationWaitingReaper {
		t.Fatalf("fresh reservation should stay waiting-reaper, got %+v", e)
	}
}

func TestBacklogPrintPlannedSkipsBlockedClosedIssue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	led := &backlogLedger{Repo: "a/b", Issues: map[string]*backlogEntry{
		"5": {Num: 5, Kind: backlogKindIssue, Lane: "headless", State: "blocked", Title: "closed stale issue", Tier: "P0"},
		"6": {Num: 6, Kind: backlogKindIssue, Lane: "headless", State: "queued", Title: "open queued issue", Tier: "P1"},
	}}
	if err := saveBacklogLedger(led); err != nil {
		t.Fatalf("saveBacklogLedger: %v", err)
	}

	var out bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stdout: &out}}
	if err := r.backlogPrintPlanned("ward agent director", []string{"a/b"}, 2); err != nil {
		t.Fatalf("backlogPrintPlanned: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "closed stale issue") {
		t.Fatalf("dry-run output should skip blocked closed issues:\n%s", got)
	}
	for _, want := range []string{"open queued issue", "ward agent engineer a/b#6"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, got)
		}
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
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/issues":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 11, "title": "private issue", "body": "body", "state": "open", "html_url": "https://f/issues/11", "labels": []map[string]any{}, "pull_request": nil},
				{"number": 12, "title": "private pr", "body": "closes #12", "state": "open", "html_url": "https://f/pulls/12", "labels": []map[string]any{}, "pull_request": map[string]any{"url": "https://f/pulls/12"}},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/12":
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

func TestPlainDirectorPrintDoesNotEnumerateIssues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FORGEJO_TOKEN", "secret")

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "repos.kdl"), []byte("repos {\n  repo-authority default=forgejo {\n    trusted-owner coilyco-flight-deck\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_CONFIG_REF", "file://"+bundleDir)

	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	sawIssueList := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "issues":
			sawIssueList = true
			t.Fatal("plain director startup must not enumerate the live issue backlog")
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "pulls":
			sawIssueList = true
			t.Fatal("plain director startup must not enumerate the live issue backlog")
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()
	forgejoBaseURL = srv.URL

	cmd := &cli.Command{Name: "director", Flags: directorFlags(), Action: func(context.Context, *cli.Command) error { return nil }}
	if err := cmd.Run(t.Context(), []string{"director", "--print", "--repo", "coilyco-flight-deck/ward"}); err != nil {
		t.Fatalf("parse print run: %v", err)
	}
	r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
	if err := r.runAgentBacklog(t.Context(), cmd, modeGoose); err != nil {
		t.Fatalf("runAgentBacklog: %v", err)
	}
	if sawIssueList {
		t.Fatal("plain director startup enumerated the issue backlog")
	}
}

func TestBacklogRefreshReservationStates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	now := time.Now().UTC()
	fresh := now.Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	stale := now.Add(-4 * time.Hour).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues" && r.URL.Query().Get("state") == "open":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 11, "title": "fresh reservation", "body": "body", "state": "open", "html_url": "https://f/issues/11", "labels": []map[string]any{{"name": "P0"}, {"name": "headless"}}},
				{"number": 12, "title": "stale reservation", "body": "body", "state": "open", "html_url": "https://f/issues/12", "labels": []map[string]any{{"name": "P1"}, {"name": "headless"}}},
			})
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/11/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"body": agentReservationMarker + "\nreserved", "created_at": fresh, "user": map[string]any{"login": "coilyco-ops"}},
			})
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/12/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"body": agentReservationMarker + "\nreserved", "created_at": stale, "user": map[string]any{"login": "coilyco-ops"}},
			})
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/11":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 11, "state": "open"})
		case r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/12":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 12, "state": "open"})
		default:
			t.Fatalf("unexpected path: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()
	forgejoBaseURL = srv.URL

	t.Setenv("WARD_CONFIG_REF", "")
	t.Setenv("WARD_AGENT_RESERVE_RECHECK", "off")
	r := &Runner{}
	if err := r.backlogRefresh(t.Context(), "director", []string{"coilyco-flight-deck/ward"}, 50); err != nil {
		t.Fatalf("backlogRefresh: %v", err)
	}
	led, err := loadBacklogLedger("coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	if got := led.Issues["11"]; got == nil || got.State != backlogReservationWaitingReaper {
		t.Fatalf("#11 state = %+v, want %q", got, backlogReservationWaitingReaper)
	}
	if got := led.Issues["12"]; got == nil || got.State != backlogReservationSafeToRedispatch {
		t.Fatalf("#12 state = %+v, want %q", got, backlogReservationSafeToRedispatch)
	}
}

func TestDirectorIssueScopeUsesOnlyTheReferencedIssue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FORGEJO_TOKEN", "secret")

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "repos.kdl"), []byte("repos {\n  repo-authority default=forgejo {\n    trusted-owner coilyco-flight-deck\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_CONFIG_REF", "file://"+bundleDir)

	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	var sawIssue5, sawIssue6, sawIssueList, sawPullList bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/5":
			sawIssue5 = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 5, "title": "target issue", "body": "body", "state": "open",
				"html_url": "https://forgejo.example/coilyco-flight-deck/ward/issues/5",
				"labels":   []map[string]any{{"name": "P0"}, {"name": "headless"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/5/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/6":
			sawIssue6 = true
			t.Fatal("side-by-side issue #6 must not be fetched in issue-scoped director mode")
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "issues":
			sawIssueList = true
			t.Fatal("repo issue backlog must not be fetched in issue-scoped director mode")
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "pulls":
			sawPullList = true
			t.Fatal("repo pull backlog must not be fetched in issue-scoped director mode")
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()
	forgejoBaseURL = srv.URL

	run := func(t *testing.T, refArg string) {
		t.Helper()
		cmd := &cli.Command{Name: "director", Flags: directorFlags(), Action: func(context.Context, *cli.Command) error { return nil }}
		if err := cmd.Run(t.Context(), []string{"director", "--dry-run", "--no-triage", refArg}); err != nil {
			t.Fatalf("parse %s: %v", refArg, err)
		}
		r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
		if err := r.runAgentBacklog(t.Context(), cmd, modeGoose); err != nil {
			t.Fatalf("runAgentBacklog(%s): %v", refArg, err)
		}
		led, err := loadBacklogLedger("coilyco-flight-deck/ward")
		if err != nil {
			t.Fatalf("load ledger: %v", err)
		}
		if len(led.Issues) != 1 {
			t.Fatalf("issue-scoped ledger length = %d, want 1", len(led.Issues))
		}
		if got := led.Issues["5"]; got == nil || got.Title != "target issue" {
			t.Fatalf("ledger entry = %+v, want issue #5", got)
		}
	}

	t.Run("repo-slug", func(t *testing.T) {
		run(t, "coilyco-flight-deck/ward#5")
	})
	t.Run("forgejo-url", func(t *testing.T) {
		run(t, srv.URL+"/coilyco-flight-deck/ward/issues/5")
	})

	if !sawIssue5 {
		t.Fatal("referenced issue was not fetched")
	}
	if sawIssue6 || sawIssueList || sawPullList {
		t.Fatalf("issue-scoped director widened to backlog: issue6=%t issueList=%t pullList=%t", sawIssue6, sawIssueList, sawPullList)
	}
}

func TestIssueScopedDirectorRefreshStaysOnOneIssue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FORGEJO_TOKEN", "secret")

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "defaults.kdl"), []byte("defaults {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "repos.kdl"), []byte("repos {\n  repo-authority default=forgejo {\n    trusted-owner coilyco-flight-deck\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_CONFIG_REF", "file://"+bundleDir)

	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	var sawIssue5, sawIssueList, sawPullList bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/5":
			sawIssue5 = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 5, "title": "target issue", "body": "body", "state": "open",
				"html_url": "https://forgejo.example/coilyco-flight-deck/ward/issues/5",
				"labels":   []map[string]any{{"name": "P0"}, {"name": "headless"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/5/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "issues":
			sawIssueList = true
			t.Fatal("repo issue backlog must not be fetched during issue-scoped refresh")
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "pulls":
			sawPullList = true
			t.Fatal("repo pull backlog must not be fetched during issue-scoped refresh")
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()
	forgejoBaseURL = srv.URL

	refArg := srv.URL + "/coilyco-flight-deck/ward/issues/5"
	r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
	ref, err := r.resolveAgentIssueRef(t.Context(), refArg)
	if err != nil {
		t.Fatalf("resolveAgentIssueRef(%s): %v", refArg, err)
	}
	cfg := backlogConfig{mode: modeGoose, limit: 50, issueRef: &ref}

	if err := r.backlogRefreshForDirector(t.Context(), "ward agent director", cfg, []string{ref.repoSlug()}); err != nil {
		t.Fatalf("refresh 1: %v", err)
	}
	if err := r.backlogRefreshForDirector(t.Context(), "ward agent director", cfg, []string{ref.repoSlug()}); err != nil {
		t.Fatalf("refresh 2: %v", err)
	}
	if !sawIssue5 {
		t.Fatal("referenced issue was not fetched during refresh")
	}
	if sawIssueList || sawPullList {
		t.Fatalf("issue-scoped refresh widened to backlog: issueList=%t pullList=%t", sawIssueList, sawPullList)
	}
}

func TestDirectorScopeSkipsBurndownReposBeforeDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WARD_CONFIG_REF", "file://"+t.TempDir())

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "repos.kdl"), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner coilyco-flight-deck
    }
    burndown default=#true {
        repo "coilyco-flight-deck/infrastructure" #false
        repo "coilyco-bridge/deploy" #false
    }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_CONFIG_REF", "file://"+bundleDir)

	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/coilyco-flight-deck/repos":
			_ = json.NewEncoder(w).Encode([]repoBrief{
				{Name: "ward"},
				{Name: "infrastructure"},
				{Name: "agentic-os"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/coilyco-flight-deck/repos":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()
	forgejoBaseURL = srv.URL

	origStderr := os.Stderr
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = errW
	t.Cleanup(func() {
		os.Stderr = origStderr
	})

	var got []string
	cmd := &cli.Command{
		Name:  "director",
		Flags: directorFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
			var err error
			got, err = r.resolveDirectorScope(ctx, c, "ward agent director")
			return err
		},
	}
	if err := cmd.Run(t.Context(), []string{"director", "--burndown", "--org", "coilyco-flight-deck"}); err != nil {
		t.Fatalf("resolveDirectorScope: %v", err)
	}

	if err := errW.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	logs, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}

	if want := []string{"coilyco-flight-deck/ward", "coilyco-flight-deck/agentic-os"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered scope = %v, want %v", got, want)
	}
	for _, want := range []string{
		"burndown: skipping coilyco-flight-deck/infrastructure (filtered)",
	} {
		if !strings.Contains(string(logs), want) {
			t.Fatalf("stderr %q missing %q", string(logs), want)
		}
	}
	if strings.Contains(string(logs), "coilyco-bridge/deploy") {
		t.Fatalf("stderr %q unexpectedly mentioned the unrelated repo", string(logs))
	}
}

func TestDirectorPlainScopeDoesNotApplyBurndownFilter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "repos.kdl"), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner coilyco-flight-deck
    }
    burndown default=#true {
        repo "coilyco-flight-deck/infrastructure" #false
    }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_CONFIG_REF", "file://"+bundleDir)

	cmd := &cli.Command{
		Name:  "director",
		Flags: directorFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
			got, err := r.resolveDirectorScope(ctx, c, "ward agent director")
			if err != nil {
				return err
			}
			want := []string{"coilyco-flight-deck/infrastructure"}
			if !reflect.DeepEqual(got, want) {
				return fmt.Errorf("scope = %v, want %v", got, want)
			}
			return nil
		},
	}
	if err := cmd.Run(t.Context(), []string{"director", "--repo", "coilyco-flight-deck/infrastructure"}); err != nil {
		t.Fatalf("resolveDirectorScope: %v", err)
	}
}

func TestResolveDirectorIssueRefFailsClosedAndDoesNotWiden(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FORGEJO_TOKEN", "secret")

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "repos.kdl"), []byte("repos {\n  repo-authority default=forgejo {\n    trusted-owner coilyco-flight-deck\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_CONFIG_REF", "file://"+bundleDir)
	reservedAt := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)

	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	cases := []struct {
		name     string
		state    string
		labels   []string
		comments []map[string]any
		wantSub  string
		refArg   string
	}{
		{
			name:    "closed",
			state:   "closed",
			labels:  []string{"P0", "headless"},
			wantSub: "issue-closed",
			refArg:  "coilyco-flight-deck/ward#6",
		},
		{
			name:    "ineligible",
			state:   "open",
			labels:  []string{"P1", "interactive"},
			wantSub: "mode-ceiling",
			refArg:  "coilyco-flight-deck/ward#6",
		},
		{
			name:   "already-reserved",
			state:  "open",
			labels: []string{"P0", "headless"},
			comments: []map[string]any{
				{"body": agentReservationMarker + "\nreserved", "created_at": reservedAt, "user": map[string]any{"login": "coilyco-ops"}},
			},
			wantSub: "already reserved remotely",
			refArg:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sawIssueList, sawPullList bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/6":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"number": 6, "title": "target issue", "body": "body", "state": tc.state,
						"html_url": "https://forgejo.example/coilyco-flight-deck/ward/issues/6",
						"labels": func() []map[string]any {
							out := make([]map[string]any, 0, len(tc.labels))
							for _, l := range tc.labels {
								out = append(out, map[string]any{"name": l})
							}
							return out
						}(),
					})
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/6/comments":
					_ = json.NewEncoder(w).Encode(tc.comments)
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "issues":
					sawIssueList = true
					t.Fatal("repo issue backlog must not be fetched in fail-closed issue scope")
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues") && r.URL.Query().Get("type") == "pulls":
					sawPullList = true
					t.Fatal("repo pull backlog must not be fetched in fail-closed issue scope")
				default:
					t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
				}
			}))
			defer srv.Close()
			forgejoBaseURL = srv.URL

			refArg := tc.refArg
			if refArg == "" {
				refArg = srv.URL + "/coilyco-flight-deck/ward/issues/6"
			}

			cmd := &cli.Command{Name: "director", Flags: directorFlags(), Action: func(context.Context, *cli.Command) error { return nil }}
			if err := cmd.Run(t.Context(), []string{"director", "--burndown"}); err != nil {
				t.Fatalf("parse director flags: %v", err)
			}
			r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
			ref, err := r.resolveDirectorIssueRef(t.Context(), cmd, "ward agent director", modeGoose, refArg)
			if err == nil {
				t.Fatalf("resolveDirectorIssueRef(%s) = %+v, want error", tc.name, ref)
			}
			if !strings.Contains(err.Error(), tc.wantSub) && !strings.Contains(err.Error(), "not open") && !strings.Contains(err.Error(), "not headless/autonomous eligible") {
				t.Fatalf("resolveDirectorIssueRef(%s) error = %v, want %q", tc.name, err, tc.wantSub)
			}
			if sawIssueList || sawPullList {
				t.Fatalf("fail-closed issue scope widened to backlog: issueList=%t pullList=%t", sawIssueList, sawPullList)
			}
		})
	}
}

func TestValidateDirectorIssueTargetPlainSurfaceAcceptsInteractiveIssue(t *testing.T) {
	parse := func(args ...string) *cli.Command {
		cmd := &cli.Command{Name: "director", Flags: directorFlags(), Action: func(context.Context, *cli.Command) error { return nil }}
		if err := cmd.Run(t.Context(), append([]string{"director"}, args...)); err != nil {
			t.Fatalf("parse director flags: %v", err)
		}
		return cmd
	}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 6}
	interactive := &Issue{Number: 6, Title: "interactive issue", State: "open", Labels: []string{"P1", "interactive"}}

	if err := validateDirectorIssueTarget(parse(), "ward agent director", ref, interactive); err != nil {
		t.Fatalf("plain director surface should accept an open interactive issue: %v", err)
	}
	if err := validateDirectorIssueTarget(parse("--burndown"), "ward agent director", ref, interactive); err == nil || !strings.Contains(err.Error(), "not headless/autonomous eligible") {
		t.Fatalf("burndown should reject an interactive issue, got %v", err)
	}
	closed := &Issue{Number: 6, Title: "closed issue", State: "closed", Labels: []string{"P0", "headless"}}
	if err := validateDirectorIssueTarget(parse(), "ward agent director", ref, closed); err == nil || !strings.Contains(err.Error(), "not open") {
		t.Fatalf("plain director should still reject a closed issue, got %v", err)
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
		wantPRURL  string
		wantPRNum  int
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
			comments:   []issueComment{{Body: "WARDED_WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729\n\nfelt calm", CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "submitted",
			wantText:   "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729",
			wantPRURL:  "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729",
			wantPRNum:  729,
		},
		{
			name: "reviewed pr url leading line",
			comments: []issueComment{{Body: strings.Join([]string{
				"WARDED_WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729",
				"",
				"<details><summary>details</summary>",
				"",
				"director merge authorization: reviewed-and-ready",
				"",
				"</details>",
			}, "\n"), CreatedAt: at("2026-06-25T10:00:00Z")}},
			wantStatus: "merge-ready",
			wantText:   "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729",
			wantPRURL:  "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729",
			wantPRNum:  729,
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
			if got.PRURL != c.wantPRURL || got.PRNumber != c.wantPRNum {
				t.Errorf("got PRURL/PRNumber %q/%d, want %q/%d", got.PRURL, got.PRNumber, c.wantPRURL, c.wantPRNum)
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
		{Num: 3, State: backlogReservationSafeToRedispatch, Tier: "P0", repo: "c/d"},
		{Num: 4, State: "done", Tier: "P0", repo: "a/b"},
		{Num: 5, State: "blocked", Tier: "P0", repo: "a/b"},
		{Num: 6, State: backlogReservationWaitingReaper, Tier: "P2", repo: "a/b"},
	}
	queued, inflight, held := backlogLaneCounts(entries)
	if queued != 2 || inflight != 1 || held != 1 {
		t.Fatalf("counts = queued %d inflight %d held %d, want 2/1/1", queued, inflight, held)
	}
	picks := backlogQueuedPicks(entries)
	if len(picks) != 2 {
		t.Fatalf("want 2 picks, got %d", len(picks))
	}
	// P0 (#3) ranks ahead of P1 (#1); the waiting-reaper hold stays out of picks.
	if picks[0].Num != 3 || picks[1].Num != 1 {
		t.Errorf("pick order = %d,%d, want 3,1", picks[0].Num, picks[1].Num)
	}
}

func TestBacklogReservationState(t *testing.T) {
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	fresh := issueComment{Body: agentReservationMarker + "\nreserved", CreatedAt: now.Add(-10 * time.Minute)}
	stale := issueComment{Body: agentReservationMarker + "\nreserved", CreatedAt: now.Add(-2 * time.Hour)}
	released := issueComment{Body: agentReservationMarker + "\nreserved", CreatedAt: now.Add(-2 * time.Hour)}
	release := issueComment{Body: agentReservationReleaseMarker, CreatedAt: now.Add(-time.Minute)}

	if got := backlogReservationState([]issueComment{fresh}, now, time.Hour); got != backlogReservationWaitingReaper {
		t.Fatalf("fresh reservation = %q, want %q", got, backlogReservationWaitingReaper)
	}
	if got := backlogReservationState([]issueComment{stale}, now, time.Hour); got != backlogReservationSafeToRedispatch {
		t.Fatalf("stale reservation = %q, want %q", got, backlogReservationSafeToRedispatch)
	}
	if got := backlogReservationState([]issueComment{released, release}, now, time.Hour); got != backlogReservationSafeToRedispatch {
		t.Fatalf("released reservation = %q, want %q", got, backlogReservationSafeToRedispatch)
	}
	if got := backlogReservationState(nil, now, time.Hour); got != "" {
		t.Fatalf("no reservation markers = %q, want empty", got)
	}
}

// TestBacklogRedispatchSweepTracked pins which parked entries the ward#1149 marker
// sweep considers: terminal-but-not-done headless issues.
func TestBacklogRedispatchSweepTracked(t *testing.T) {
	for state, want := range map[string]bool{
		"submitted":                        true,
		"merge-ready":                      true,
		"blocked":                          true,
		"failed":                           true,
		"done":                             false,
		"queued":                           false,
		"dispatched":                       false,
		backlogReservationWaitingReaper:    false,
		backlogReservationSafeToRedispatch: false,
	} {
		e := &backlogEntry{Kind: backlogKindIssue, Lane: "headless", State: state}
		if got := backlogRedispatchSweepTracked(e); got != want {
			t.Errorf("state %q tracked = %t, want %t", state, got, want)
		}
	}
	if backlogRedispatchSweepTracked(&backlogEntry{Kind: backlogKindPullRequest, Lane: "headless", State: "merge-ready"}) {
		t.Error("a pull-request entry must not be swept")
	}
	if backlogRedispatchSweepTracked(&backlogEntry{Kind: backlogKindIssue, Lane: "interactive", State: "merge-ready"}) {
		t.Error("a non-headless entry must not be swept")
	}
}

// TestBacklogSweepNeedsRedispatch drives the ward#1149 sweep: an unhandled marker
// re-queues (bounded), a newer outcome leaves parked, the cap parks blocked.
func TestBacklogSweepNeedsRedispatch(t *testing.T) {
	now := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	r := &Runner{}
	tr := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}
	marker := issueComment{
		Body:      agentReservationReleaseMarker + "\n" + agentNeedsRedispatchMarker + "\nWARD-DISPATCH: failed ❌",
		CreatedAt: now.Add(-time.Minute),
	}
	outcome := issueComment{Body: "WARD-OUTCOME: merge-ready", CreatedAt: now.Add(-30 * time.Minute)}

	e := &backlogEntry{
		Num: 927, Kind: backlogKindIssue, Lane: "headless", State: "merge-ready",
		Container: "engineer-codex-ward-927", DispatchedAt: now.Add(-time.Hour).Format(time.RFC3339),
	}
	if !r.backlogSweepNeedsRedispatch(context.Background(), "coilyco-flight-deck/ward", tr, e, []issueComment{outcome, marker}) {
		t.Fatal("sweep did not re-queue the unhandled marker")
	}
	if e.State != "queued" || e.Container != "" || e.DispatchedAt != "" {
		t.Fatalf("swept entry = %+v, want queued with a cleared dispatch record", e)
	}
	if e.RedispatchAttempts != 1 {
		t.Fatalf("attempts = %d, want 1", e.RedispatchAttempts)
	}
	if e.LastOutcome == nil || e.LastOutcome.Status != "needs-redispatch-requeued" {
		t.Fatalf("outcome = %+v, want needs-redispatch-requeued", e.LastOutcome)
	}

	// A newer outcome means the marker was handled: no re-queue.
	handled := issueComment{Body: "WARD-OUTCOME: merge-ready", CreatedAt: now}
	e2 := &backlogEntry{Num: 928, Kind: backlogKindIssue, Lane: "headless", State: "merge-ready"}
	if r.backlogSweepNeedsRedispatch(context.Background(), "coilyco-flight-deck/ward", tr, e2, []issueComment{marker, handled}) {
		t.Fatal("sweep re-queued a marker an outcome already superseded")
	}

	// The cap parks it blocked instead of looping.
	e3 := &backlogEntry{Num: 929, Kind: backlogKindIssue, Lane: "headless", State: "failed", RedispatchAttempts: redispatchAttemptCap}
	if !r.backlogSweepNeedsRedispatch(context.Background(), "coilyco-flight-deck/ward", tr, e3, []issueComment{marker}) {
		t.Fatal("capped sweep did not park the entry")
	}
	if e3.State != "blocked" || e3.LastOutcome == nil || e3.LastOutcome.Status != "redispatch-cap-reached" {
		t.Fatalf("capped entry = %+v (outcome %+v), want blocked/redispatch-cap-reached", e3, e3.LastOutcome)
	}
	// Already blocked at cap: no churn.
	if r.backlogSweepNeedsRedispatch(context.Background(), "coilyco-flight-deck/ward", tr, e3, []issueComment{marker}) {
		t.Fatal("capped+blocked entry must not keep reporting changes")
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

func TestBacklogDispatchContainerName(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 900}
	got := backlogDispatchContainerName(dispatchEngineer{harness: modeCodex}, ref)
	want := issueScopedContainerName(roleEngineer, modeCodex, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	if got != want {
		t.Fatalf("backlogDispatchContainerName() = %q, want %q", got, want)
	}
}

func TestBacklogReconcileKeepsDispatchedWhenDockerUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 900}
	entry := &backlogEntry{
		Num:          ref.Number,
		Title:        "broker-forwarded run",
		Lane:         "headless",
		State:        "dispatched",
		Container:    backlogDispatchContainerName(dispatchEngineer{harness: modeCodex}, ref),
		DispatchedAt: time.Now().UTC().Format(time.RFC3339),
		repo:         ref.repoSlug(),
	}
	r := &Runner{Runner: &shell.Runner{Resolve: func(bin string) (string, error) {
		if bin == "docker" {
			return "", errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
		}
		return "/bin/true", nil
	}}}
	changed := r.backlogReconcile(context.Background(), &fakeLockForge{}, ref.repoSlug(), targetRepo{Owner: ref.Owner, Name: ref.Repo}, entry)
	if changed {
		t.Fatal("docker lookup error must not mark a broker-forwarded dispatched run failed")
	}
	if entry.State != "dispatched" {
		t.Fatalf("entry state = %q, want dispatched", entry.State)
	}
}
