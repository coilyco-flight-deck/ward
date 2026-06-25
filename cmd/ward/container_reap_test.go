package main

import (
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/scan"
)

// TestReadReapEnvIssueAndLaunched asserts the reaper reads the ward#264 signals
// (WARD_TARGET_ISSUE, WARD_AGENT_LAUNCHED) and gates the release on them.
func TestReadReapEnvIssueAndLaunched(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")

	// Pre-launch death carrying an issue: releasable.
	t.Setenv("WARD_TARGET_ISSUE", "264")
	t.Setenv(envAgentLaunched, "")
	e, err := readReapEnv()
	if err != nil {
		t.Fatalf("readReapEnv: %v", err)
	}
	if e.Issue != 264 || e.Launched {
		t.Fatalf("want Issue=264 Launched=false, got Issue=%d Launched=%v", e.Issue, e.Launched)
	}
	if !e.reservationReleasable() {
		t.Error("a pre-launch death carrying an issue should be releasable")
	}

	// Agent launched: not releasable even with an issue.
	t.Setenv(envAgentLaunched, "1")
	e, _ = readReapEnv()
	if !e.Launched || e.reservationReleasable() {
		t.Errorf("a launched run must keep its hold, got Launched=%v releasable=%v", e.Launched, e.reservationReleasable())
	}

	// No issue (bare `container up`): nothing to release, garbage parses to 0.
	t.Setenv("WARD_TARGET_ISSUE", "not-a-number")
	t.Setenv(envAgentLaunched, "")
	e, _ = readReapEnv()
	if e.Issue != 0 || e.reservationReleasable() {
		t.Errorf("a garbage/absent issue must be 0 and not releasable, got Issue=%d releasable=%v", e.Issue, e.reservationReleasable())
	}
}

// TestReadReapEnvParsesExtraRepos covers ward#291: the reaper reads WARD_EXTRA_REPOS
// so it can verify each --repo grant landed, dropping the target and malformed entries.
func TestReadReapEnvParsesExtraRepos(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")
	t.Setenv("WARD_TARGET_ISSUE", "291")
	// The target itself, a blank, and a malformed token all drop out; two grants stay.
	t.Setenv("WARD_EXTRA_REPOS", "coilyco-bridge/agentic-os-kai  garbage coilyco-flight-deck/ward coilyco-flight-deck/cli-guard")
	e, err := readReapEnv()
	if err != nil {
		t.Fatalf("readReapEnv: %v", err)
	}
	got := make([]string, len(e.ExtraRepos))
	for i, r := range e.ExtraRepos {
		got[i] = r.slug()
	}
	want := []string{"coilyco-bridge/agentic-os-kai", "coilyco-flight-deck/cli-guard"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ExtraRepos = %v, want %v", got, want)
	}
}

// TestExtraRepoWorkDir pins the granted-repo working-copy layout the reaper verifies.
func TestExtraRepoWorkDir(t *testing.T) {
	got := extraRepoWorkDir(targetRepo{Owner: "coilyco-bridge", Name: "agentic-os-kai"})
	if got != "/workspace/agentic-os-kai" {
		t.Errorf("extraRepoWorkDir = %q, want /workspace/agentic-os-kai", got)
	}
}

// TestUnlandedExtraReposComment covers ward#291: the reopen comment names each
// un-landed grant, renders a recover block, and degrades loudly on a failed push.
func TestUnlandedExtraReposComment(t *testing.T) {
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me"}
	reports := []extraRepoUnlanded{
		{Repo: targetRepo{Owner: "coilyco-bridge", Name: "agentic-os-kai"}, Ahead: 2, Branch: "ward-salvage/agentic-os-kai-abc123"},
		{Repo: targetRepo{Owner: "coilyco-flight-deck", Name: "cli-guard"}, NoMain: true, PushErr: "remote: forbidden\nfatal: unable to access"},
	}
	got := unlandedExtraReposComment(env, reports)
	for _, want := range []string{
		"Reopened",                                // the headline undoing the close
		"coilyco-flight-deck/ward",                // the issue's own repo, named
		"coilyco-bridge/agentic-os-kai",           // the un-landed grant
		"2 local commit(s) never reached",         // the ahead count
		"ward-salvage/agentic-os-kai-abc123",      // the salvage branch
		"git fetch https://forgejo.coilysiren.me", // the recover command
		"no `main` branch to compare",             // the no-main verdict
		"salvage-branch push also failed",         // the degraded preservation
		"remote: forbidden",                       // the push error's first line
		"native issue in the granted",             // the ward#291 guidance
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment missing %q\n got: %s", want, got)
		}
	}
	// The multi-line push error collapses to its first line only.
	if strings.Contains(got, "unable to access") {
		t.Errorf("push error should collapse to its first line\n got: %s", got)
	}
}

func TestDecideReap(t *testing.T) {
	cases := []struct {
		name string
		in   reapInputs
		want reapAction
	}{
		{"clean tree does nothing", reapInputs{HasResidualWork: false}, reapNothing},
		{"clean integration + clean scan lands on main",
			reapInputs{HasResidualWork: true, IntegrationClean: true}, reapPushMain},
		{"conflict salvages",
			reapInputs{HasResidualWork: true, IntegrationClean: false}, reapSalvage},
		{"scan finding salvages even when integration is clean",
			reapInputs{HasResidualWork: true, IntegrationClean: true,
				Findings: []scan.Finding{{Path: "node_modules/x", Reason: "vendored"}}}, reapSalvage},
	}
	for _, c := range cases {
		if got := decideReap(c.in); got != c.want {
			t.Errorf("%s: decideReap = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSalvageBranchAndTitleStable(t *testing.T) {
	if got := salvageBranchName("eco-app-a1b2"); got != "ward-salvage/eco-app-a1b2" {
		t.Errorf("salvageBranchName = %q", got)
	}
	r := salvageReport{
		Repo:   targetRepo{Owner: "coilyco-gaming", Name: "eco-app"},
		Branch: "ward-salvage/eco-app-a1b2",
	}
	title := salvageIssueTitle(r)
	if !strings.HasPrefix(title, salvageIssueTitlePrefix) {
		t.Errorf("title %q missing dedupe prefix", title)
	}
	if !strings.Contains(title, "eco-app") || !strings.Contains(title, r.Branch) {
		t.Errorf("title %q missing repo/branch", title)
	}
}

func TestIsAuthFailure(t *testing.T) {
	auth := []string{
		"remote: Invalid username or password.\nfatal: Authentication failed for 'https://forgejo.example/x.git/'",
		"fatal: unable to access 'https://...': The requested URL returned error: 403 Forbidden",
		"remote: Forbidden\nfatal: unable to access",
		"error: 401 Unauthorized",
		"fatal: could not read Username for 'https://forgejo.example': terminal prompts disabled",
	}
	for _, o := range auth {
		if !isAuthFailure(o) {
			t.Errorf("expected auth failure for %q", o)
		}
	}
	notAuth := []string{
		"! [rejected]        HEAD -> main (non-fast-forward)\nerror: failed to push some refs",
		"hint: Updates were rejected because the remote contains work that you do not have locally.\nhint: fetch first",
		"fatal: unable to access 'https://...': Could not resolve host: forgejo.example",
		"",
	}
	for _, o := range notAuth {
		if isAuthFailure(o) {
			t.Errorf("did not expect auth failure for %q", o)
		}
	}
}

func TestFormatTokenAge(t *testing.T) {
	up := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		upAt string
		now  time.Time
		want string
		ok   bool
	}{
		{"hours and minutes", up.Format(time.RFC3339), up.Add(3*time.Hour + 42*time.Minute), "3h42m", true},
		{"days and hours", up.Format(time.RFC3339), up.Add(2*24*time.Hour + 3*time.Hour), "2d3h", true},
		{"minutes only", up.Format(time.RFC3339), up.Add(45 * time.Minute), "45m", true},
		{"sub-minute", up.Format(time.RFC3339), up.Add(30 * time.Second), "30s", true},
		{"empty stamp", "", up, "", false},
		{"unparseable stamp", "not-a-time", up, "", false},
		{"future stamp (clock skew)", up.Format(time.RFC3339), up.Add(-time.Hour), "", false},
	}
	for _, c := range cases {
		got, ok := formatTokenAge(c.upAt, c.now)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: formatTokenAge = (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestSalvageIssueBodyStampsAuthCauseAndAge(t *testing.T) {
	r := salvageReport{
		Repo:      targetRepo{Owner: "coilyco-flight-deck", Name: "ward"},
		Mode:      "claude",
		Branch:    "ward-salvage/ward-a1b2",
		Reason:    reasonAuthFail,
		AuthCause: true,
		TokenAge:  "5h12m",
		Base:      "https://forgejo.coilysiren.me",
	}
	body := salvageIssueBody(r)
	for _, want := range []string{
		"Container uptime at reap:",
		"5h12m",
		"dead/rotated PAT",
		"rebase and land cleanly",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("auth-cause body missing %q\n---\n%s", want, body)
		}
	}

	// A conflict salvage (no auth cause, no stamp) must NOT claim a dead PAT.
	conflict := salvageReport{
		Repo:   targetRepo{Owner: "coilyco-flight-deck", Name: "ward"},
		Mode:   "claude",
		Branch: "ward-salvage/ward-c3d4",
		Reason: reasonConflict,
		Base:   "https://forgejo.coilysiren.me",
	}
	cbody := salvageIssueBody(conflict)
	if strings.Contains(cbody, "dead/rotated PAT") {
		t.Errorf("conflict body should not mention a dead PAT\n---\n%s", cbody)
	}
	if strings.Contains(cbody, "Container uptime at reap:") {
		t.Errorf("conflict body should omit uptime when TokenAge is empty\n---\n%s", cbody)
	}
}

func TestSalvageIssueBodyRendersRecoveryAndFindings(t *testing.T) {
	r := salvageReport{
		Repo:     targetRepo{Owner: "coilyco-gaming", Name: "eco-app"},
		Mode:     "claude",
		Branch:   "ward-salvage/eco-app-a1b2",
		Reason:   reasonConflict,
		Findings: []scan.Finding{{Path: "node_modules/x/i.js", Reason: "vendored/generated tree (node_modules/)"}},
		Status:   " M src/main.go\n?? scratch.txt",
		Base:     "https://forgejo.coilysiren.me",
	}
	body := salvageIssueBody(r)
	for _, want := range []string{
		"claude",
		"ward-salvage/eco-app-a1b2",
		string(reasonConflict),
		"git fetch https://forgejo.coilysiren.me/coilyco-gaming/eco-app.git ward-salvage/eco-app-a1b2",
		"node_modules/x/i.js",
		"src/main.go",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body missing %q\n---\n%s", want, body)
		}
	}
}
