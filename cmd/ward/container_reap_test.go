package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/scan"
)

// TestReapEnvContainerCorrelation asserts the reaper reads WARD_CONTAINER_NAME and
// leads its start marker with container=<name> (ward#517), the run correlation id.
func TestReapEnvContainerCorrelation(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")
	t.Setenv("WARD_CONTAINER_NAME", "ward-agent-517-abc")
	t.Setenv("WARD_TARGET_ISSUE", "517")

	e, err := readReapEnv()
	if err != nil {
		t.Fatalf("readReapEnv: %v", err)
	}
	if e.Container != "ward-agent-517-abc" {
		t.Fatalf("Container = %q, want the WARD_CONTAINER_NAME value", e.Container)
	}
	line := e.reapStartLine()
	for _, want := range []string{
		"container=ward-agent-517-abc",
		"repo=coilyco-flight-deck/ward",
		"issue=517",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("reap start line missing %q:\n%s", want, line)
		}
	}
}

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

// TestRenderReapDiagnosticsFalseSalvage covers ward#531: the block renders the ward
// version and, for a HEAD-on-main input, names the outcome a FALSE salvage.
func TestRenderReapDiagnosticsFalseSalvage(t *testing.T) {
	d := reapDiagnostics{
		WardVersion:   "v0.297.0",
		VersionSource: "releases/latest (resolved in-container)",
		Head:          "abc123def456",
		OriginMain:    "abc123def456",
		HeadOnMain:    true, // HEAD already contained in origin/main -> false salvage
		Gate:          "no run-owned landed commit after dispatch",
		Reason:        reasonConflict,
		ProvState:     "present",
		Landed:        false,
		Status:        "",
		TokenAge:      "6h03m",
	}
	block := renderReapDiagnostics(d)
	for _, want := range []string{
		reapDiagHeader,               // greppable header
		reapDiagFooter,               // greppable footer
		"v0.297.0",                   // the reaper's ward version - the #504 key field
		"releases/latest",            // how it resolved
		"FALSE salvage",              // the ancestry verdict in plain words
		"ward#504",                   // the false-salvage signature reference
		"no run-owned landed commit", // the decision gate that fired
		"run-owned landed:  no",      // the landed verdict
		"6h03m",                      // container uptime / PAT-age proxy
	} {
		if !strings.Contains(block, want) {
			t.Errorf("diagnostics block missing %q\n---\n%s", want, block)
		}
	}

	// A HEAD-not-on-main input must NOT read as a false salvage.
	d.HeadOnMain = false
	d.OriginMain = "999fedcba000"
	if strings.Contains(renderReapDiagnostics(d), "FALSE salvage") {
		t.Error("a HEAD-not-on-main input must not render a FALSE salvage verdict")
	}

	// No origin/main at all: the block says so rather than implying an ancestry.
	d.OriginMain = ""
	if !strings.Contains(renderReapDiagnostics(d), "origin/main absent") {
		t.Error("an absent origin/main should render the absent-ancestry verdict")
	}
}

// TestSalvageIssueBodyFoldsDiagnostics covers ward#531 acceptance 2: the same
// diagnostics facts appear in the durable salvage issue body, not only on stderr.
func TestSalvageIssueBodyFoldsDiagnostics(t *testing.T) {
	r := salvageReport{
		Repo:   targetRepo{Owner: "coilyco-flight-deck", Name: "ward"},
		Mode:   "claude",
		Branch: "ward-salvage/ward-a1b2",
		Reason: reasonConflict,
		Base:   "https://forgejo.coilysiren.me",
		Diagnostics: reapDiagnostics{
			WardVersion:   "v0.297.0",
			VersionSource: "releases/latest (resolved in-container)",
			Head:          "abc123def456",
			OriginMain:    "abc123def456",
			HeadOnMain:    true,
			Gate:          "no run-owned landed commit after dispatch",
			Reason:        reasonConflict,
			ProvState:     "present",
		},
	}
	body := salvageIssueBody(r)
	for _, want := range []string{"## Reap diagnostics", reapDiagHeader, "v0.297.0", "FALSE salvage"} {
		if !strings.Contains(body, want) {
			t.Errorf("salvage issue body missing folded diagnostic %q\n---\n%s", want, body)
		}
	}

	// A report with no diagnostics gathered omits the section entirely (no empty block).
	bare := salvageIssueBody(salvageReport{Repo: r.Repo, Mode: "claude", Branch: r.Branch, Reason: reasonConflict, Base: r.Base})
	if strings.Contains(bare, "## Reap diagnostics") {
		t.Error("a report with no diagnostics must omit the diagnostics section")
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

func TestIssueClosingReferencePresent(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "branch", "-M", "main")
	runGit(t, repo, "remote", "add", "origin", repo)
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	if r.issueClosingReferencePresent(t.Context(), repo, 511) {
		t.Fatal("issueClosingReferencePresent should be false when no commit mentions closes #511")
	}

	if err := os.WriteFile(filepath.Join(repo, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feat.txt")
	runGit(t, repo, "commit", "-m", "ward work\n\ncloses #511")
	if !r.issueClosingReferencePresent(t.Context(), repo, 511) {
		t.Fatal("issueClosingReferencePresent should be true when a commit mentions closes #511")
	}
}

func TestRunProvenanceLandedRejectsPreexistingCommit(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGitCommitAt(t, repo, "2026-07-02T05:19:32Z", "base.txt", "base\n", "stale carry\n\ncloses #512")
	runGit(t, repo, "remote", "add", "origin", repo)
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	reservedAt := "2026-07-02T05:39:58Z"
	prov := runProvenance{
		RunID:        "engineer-goose-infrastructure-426",
		Repo:         "coilyco-flight-deck/infrastructure",
		Issue:        513,
		ReservedAt:   reservedAt,
		BaselineMain: mustGitRev(t, repo, "origin/main"),
	}
	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	if r.runProvenanceLanded(t.Context(), repo, prov, 513) {
		t.Fatal("preexisting commit on origin/main must not satisfy a newer run reservation")
	}
}

func TestRunProvenanceLandedRequiresMatchingIssueAfterReservation(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGitCommitAt(t, repo, "2026-07-02T05:19:32Z", "base.txt", "base\n", "stale carry\n\ncloses #512")
	runGit(t, repo, "remote", "add", "origin", repo)
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGitCommitAt(t, repo, "2026-07-02T05:45:00Z", "feat.txt", "fresh\n", "fresh carry\n\ncloses #513")
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	prov := runProvenance{
		RunID:        "engineer-goose-infrastructure-426",
		Repo:         "coilyco-flight-deck/infrastructure",
		Issue:        513,
		ReservedAt:   "2026-07-02T05:39:58Z",
		BaselineMain: mustGitRev(t, repo, "HEAD~1"),
	}
	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	if !r.runProvenanceLanded(t.Context(), repo, prov, 513) {
		t.Fatal("matching issue commit after reservation should satisfy the provenance proof")
	}
	prov.Issue = 514
	if r.runProvenanceLanded(t.Context(), repo, prov, 514) {
		t.Fatal("adjacent issue numbers must not cross-attribute landed history")
	}
}

// TestReapTargetTreeLandedAndClosedDoesNotSalvage covers ward#518 deliverable 1:
// a landed run (HEAD in origin/main, clean tree) reads as done, never salvage.
func TestReapTargetTreeLandedAndClosedDoesNotSalvage(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGitCommitAt(t, repo, "2026-07-02T06:50:00Z", "base.txt", "base\n", "base")
	runGit(t, repo, "remote", "add", "origin", repo)
	runGit(t, repo, "push", "origin", "main")
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	baseline := mustGitRev(t, repo, "HEAD")
	prov := runProvenance{
		RunID:        "engineer-claude-ward-518",
		Repo:         "coilyco-flight-deck/ward",
		Issue:        518,
		ReservedAt:   "2026-07-02T06:55:00Z",
		BaselineMain: baseline,
	}
	provData, err := json.Marshal(prov)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, runProvenanceFile), provData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feat.txt")
	runGit(t, repo, "commit", "-m", "ward work\n\ncloses #518")
	// The work already landed: origin/main IS this commit (residual 0, clean tree).
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	// Launched + carrying an issue: not reservation-releasable, so no Forgejo client.
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me", Mode: "claude", Issue: 518, Launched: true}
	if err := r.reapTargetTree(t.Context(), repo, env, false); err != nil {
		t.Fatalf("reapTargetTree on a landed-and-closed run: %v", err)
	}
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("landed-and-closed run must not create a salvage branch, got: %q", string(out))
	}
}

// TestReapTargetTreeLandedDirectMainWithoutCloseRefSalvages covers ward#674.
// A direct-main run already on origin/main must still verify its carried closes ref.
func TestReapTargetTreeLandedDirectMainWithoutCloseRefSalvages(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")
	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test User")
	runGitCommitAt(t, work, "2026-07-08T10:19:00Z", "base.txt", "base\n", "base")
	runGit(t, work, "remote", "add", "origin", origin)
	runGit(t, work, "push", "origin", "main")
	runGit(t, work, "update-ref", "refs/remotes/origin/main", "HEAD")
	baseline := mustGitRev(t, work, "HEAD")

	prov := runProvenance{
		RunID:        "engineer-codex-ward-674",
		Repo:         "coilyco-flight-deck/ward",
		Issue:        674,
		ReservedAt:   "2026-07-08T10:19:28Z",
		BaselineMain: baseline,
	}
	provData, err := json.Marshal(prov)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, runProvenanceFile), provData, 0o644); err != nil {
		t.Fatal(err)
	}

	runGitCommitAt(t, work, "2026-07-08T10:20:00Z", "feat.txt", "feat\n", "Add reference-media deploy workflow")
	runGit(t, work, "push", "origin", "main")
	runGit(t, work, "update-ref", "refs/remotes/origin/main", "HEAD")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me", Mode: "codex", Issue: 674, Launched: true, Workflow: workflowDirectMain}
	if err := r.reapTargetTree(t.Context(), work, env, false); err != nil {
		t.Fatalf("reapTargetTree on a landed direct-main run without closes #674: %v", err)
	}

	if got, want := mustGitRev(t, origin, "main"), mustGitRev(t, work, "HEAD"); got != want {
		t.Fatalf("a landed direct-main run without closes #674 must not advance main again: origin main=%s work HEAD=%s", got, want)
	}
	out, _ := exec.Command("git", "-C", origin, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("a landed direct-main run without closes #674 must be preserved on a salvage branch")
	}
}

// TestReapTargetTreeLandedWithOnlyProvenanceArtifactDoesNotSalvage covers the
// ward#662 regression: the reaper must ignore its own provenance file.
func TestReapTargetTreeLandedWithOnlyProvenanceArtifactDoesNotSalvage(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")
	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test User")
	runGitCommitAt(t, work, "2026-07-02T06:00:00Z", "base.txt", "base\n", "base")
	runGit(t, work, "remote", "add", "origin", origin)
	runGit(t, work, "push", "origin", "main")
	runGit(t, work, "update-ref", "refs/remotes/origin/main", "HEAD")
	baseline := mustGitRev(t, work, "HEAD")

	prov := runProvenance{
		RunID:        "engineer-goose-ward-662",
		Repo:         "coilyco-flight-deck/ward",
		Issue:        662,
		ReservedAt:   "2026-07-02T06:10:00Z",
		BaselineMain: baseline,
	}
	provData, err := json.Marshal(prov)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, runProvenanceFile), provData, 0o644); err != nil {
		t.Fatal(err)
	}

	runGitCommitAt(t, work, "2026-07-02T06:20:00Z", "feat.txt", "feat\n", "ward work\n\ncloses #662")
	runGit(t, work, "push", "origin", "main")
	runGit(t, work, "update-ref", "refs/remotes/origin/main", "HEAD")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me", Mode: "goose", Issue: 662, Launched: true}
	if err := r.reapTargetTree(t.Context(), work, env, false); err != nil {
		t.Fatalf("reapTargetTree on a landed run with only provenance dirty: %v", err)
	}

	if got, want := mustGitRev(t, origin, "main"), mustGitRev(t, work, "HEAD"); got != want {
		t.Fatalf("a landed run with only provenance dirty must stay on main: origin main=%s, work HEAD=%s", got, want)
	}
	out, _ := exec.Command("git", "-C", origin, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("a landed run with only provenance dirty must not create a salvage branch, got: %q", string(out))
	}
}

// TestReapTargetTreeResidualOnlyRunWithoutCloseRefSalvages is the ward#515 regression:
// a residual-only carried run (reaper commit, subject + trailer, NO closes) salvages.
func TestReapTargetTreeResidualOnlyRunWithoutCloseRefSalvages(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")
	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test User")
	runGitCommitAt(t, work, "2026-07-02T06:00:00Z", "base.txt", "base\n", "base")
	runGit(t, work, "remote", "add", "origin", origin)
	runGit(t, work, "push", "origin", "main")
	runGit(t, work, "update-ref", "refs/remotes/origin/main", "HEAD")
	baseline := mustGitRev(t, work, "origin/main")

	// A real, readable provenance file so the run passes the provenance gate and
	// reaches the closing-ref check, exactly as the incident run did.
	prov := runProvenance{
		RunID:        "engineer-goose-infrastructure-427",
		Repo:         "coilyco-flight-deck/infrastructure",
		Issue:        427,
		ReservedAt:   "2026-07-02T06:23:49Z",
		BaselineMain: baseline,
	}
	provData, err := json.Marshal(prov)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, runProvenanceFile), provData, 0o644); err != nil {
		t.Fatal(err)
	}

	// The exact residual commit shape observed on infrastructure#427's main:
	// reaper subject plus ONLY a Co-Authored-By trailer, no closes ref.
	runGitCommitAt(t, work, "2026-07-02T07:10:00Z", "scratch.txt", "loose\n",
		"ward-container: residual goose work on coilyco-flight-deck/infrastructure\n\nCo-Authored-By: Goose <goose@ward.agent>")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	// Token empty: salvage preserves the branch but files no issue (logged, not fatal).
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "infrastructure", Base: "https://forgejo.coilysiren.me", Mode: "goose", Issue: 427, Launched: true}
	if err := r.reapTargetTree(t.Context(), work, env, false); err != nil {
		t.Fatalf("reapTargetTree salvaging a close-refless residual-only run: %v", err)
	}

	// The carried issue was never closed, so the residual commit must NOT have
	// advanced canonical main: origin/main still points at the baseline.
	if got := mustGitRev(t, origin, "main"); got != baseline {
		t.Fatalf("a residual-only run without closes #427 must not land on main: origin main=%s baseline=%s", got, baseline)
	}
	// The work is preserved on a salvage branch instead.
	out, _ := exec.Command("git", "-C", origin, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("a close-refless residual-only run must be preserved on a salvage branch")
	}
}

// initEmptyRepoRun builds the empty-repo (no origin/main) establish-main fixture:
// an empty bare remote plus a work clone with one commit and no origin/main (ward#599).
func initEmptyRepoRun(t *testing.T, path, content, message string) (remote, work string) {
	t.Helper()
	remote = t.TempDir()
	runGit(t, remote, "init", "--bare", "-b", "main")
	work = t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(work, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", path)
	runGit(t, work, "commit", "-m", message)
	runGit(t, work, "remote", "add", "origin", remote)
	return remote, work
}

// TestReapTargetTreeEstablishesMainOnEmptyRepo is the ward#599 acceptance: a clean,
// run-owned empty-repo run creates main from its work instead of salvaging.
func TestReapTargetTreeEstablishesMainOnEmptyRepo(t *testing.T) {
	remote, work := initEmptyRepoRun(t, "server.py", "print('hi')\n", "build reddit-mcp\n\ncloses #599")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "reddit-mcp", Base: "https://forgejo.coilysiren.me", Mode: "claude", Issue: 599, Launched: true}
	if err := r.reapTargetTree(t.Context(), work, env, true); err != nil {
		t.Fatalf("reapTargetTree establishing main on an empty repo: %v", err)
	}
	if got, want := mustGitRev(t, remote, "main"), mustGitRev(t, work, "HEAD"); got != want {
		t.Fatalf("establish-main must push HEAD as the new default branch: remote main=%s, work HEAD=%s", got, want)
	}
	out, _ := exec.Command("git", "-C", remote, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("a clean empty-repo run must not create a salvage branch, got: %q", string(out))
	}
}

// TestReapTargetTreeEmptyRepoMissingCloseRefSalvages is the guard half of ward#599:
// an empty-repo run whose committed work omits its closing reference still salvages.
func TestReapTargetTreeEmptyRepoMissingCloseRefSalvages(t *testing.T) {
	remote, work := initEmptyRepoRun(t, "server.py", "print('hi')\n", "build reddit-mcp (no closing ref)")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	// Token empty: salvage preserves the branch but files no issue (logged, not fatal).
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "reddit-mcp", Base: "https://forgejo.coilysiren.me", Mode: "claude", Issue: 599, Launched: true}
	if err := r.reapTargetTree(t.Context(), work, env, true); err != nil {
		t.Fatalf("reapTargetTree salvaging a close-refless empty-repo run: %v", err)
	}
	if refExists(t.Context(), r, work, "origin/main") {
		t.Fatal("a salvaged run must NOT establish main on the empty repo")
	}
	out, _ := exec.Command("git", "-C", remote, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("a close-refless empty-repo run must be preserved on a salvage branch")
	}
}

// TestIssueClosingReferenceInRangeWholeHistory covers the empty-repo range: with no
// origin/main baseline, the closing ref is checked across whole-HEAD history (ward#599).
func TestIssueClosingReferenceInRangeWholeHistory(t *testing.T) {
	_, work := initEmptyRepoRun(t, "server.py", "print('hi')\n", "build reddit-mcp\n\ncloses #599")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	// origin/main..HEAD is unresolvable (no origin/main), so the normal check reads false.
	if r.issueClosingReferencePresent(t.Context(), work, 599) {
		t.Fatal("origin/main-relative check must not resolve on an empty repo")
	}
	// The whole-HEAD range finds the closing reference the run committed.
	if !r.issueClosingReferenceInRange(t.Context(), work, 599, "HEAD") {
		t.Fatal("whole-history range must find closes #599 on an empty repo")
	}
	if r.issueClosingReferenceInRange(t.Context(), work, 600, "HEAD") {
		t.Fatal("whole-history range must not cross-attribute an adjacent issue number")
	}
}

// TestCheckExtraRepoLandedTreatsLandedGrantAsLanded is the ward#583 regression: a
// landed grant carrying NO target `closes #N` must read landed, never salvaged.
func TestCheckExtraRepoLandedTreatsLandedGrantAsLanded(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feat.txt")
	// A granted repo lands with its own subject, NOT the target's `closes #N`.
	runGit(t, repo, "commit", "-m", "Merge issue-583: per-role capability roster")
	runGit(t, repo, "remote", "add", "origin", repo)
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me", Mode: "claude", Issue: 583, Launched: true}
	rep, landed := r.checkExtraRepoLanded(t.Context(), env, targetRepo{Owner: "coilyco-flight-deck", Name: "cli-guard"}, repo)
	if !landed {
		t.Fatalf("a landed grant with no closes-ref must read as landed, got unlanded: %+v", rep)
	}
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("a landed grant must not create a salvage branch, got: %q", string(out))
	}
}

// TestGrantLandedTrueOnMergeCommitAncestor covers the ward#583 core: a merge-commit
// landing leaves HEAD a proper ANCESTOR of origin/main; reachability is the signal.
func TestGrantLandedTrueOnMergeCommitAncestor(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base")
	// The work commit F on a side branch.
	runGit(t, repo, "checkout", "-b", "issue-583")
	if err := os.WriteFile(filepath.Join(repo, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feat.txt")
	runGit(t, repo, "commit", "-m", "cli-guard work")
	feat := mustGitRev(t, repo, "HEAD")
	// Land it on main via a merge commit M, then point origin/main at M.
	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "merge", "--no-ff", "-m", "Merge issue-583", "issue-583")
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	// Leave HEAD on the work commit F: F != origin/main (M) but F is an ancestor of M.
	runGit(t, repo, "checkout", feat)

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	if landed, hasMain := r.grantLanded(t.Context(), repo); !landed || !hasMain {
		t.Fatalf("HEAD reachable from origin/main via a merge commit must read landed; got landed=%t hasMain=%t", landed, hasMain)
	}
}

// TestGrantLandedFalseRetriesPropagationWindow covers ward#583's other half: a genuine
// miss reads unlanded, and the reaper exhausts the propagation window before saying so.
func TestGrantLandedFalseRetriesPropagationWindow(t *testing.T) {
	// A separate bare origin that stays pinned at base, so `git fetch origin` cannot
	// carry the un-landed local commit forward and mask the miss.
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "remote", "add", "origin", origin)
	runGit(t, repo, "push", "origin", "main")
	// An un-landed commit on top of origin/main: HEAD is ahead, not reachable.
	if err := os.WriteFile(filepath.Join(repo, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feat.txt")
	runGit(t, repo, "commit", "-m", "un-landed work")

	sleeps := 0
	prev := grantLandingSleep
	grantLandingSleep = func(time.Duration) { sleeps++ }
	t.Cleanup(func() { grantLandingSleep = prev })

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	if landed, hasMain := r.grantLanded(t.Context(), repo); landed || !hasMain {
		t.Fatalf("un-landed HEAD must read unlanded with a present main; got landed=%t hasMain=%t", landed, hasMain)
	}
	if want := grantLandingFetchAttempts - 1; sleeps != want {
		t.Fatalf("propagation window should sleep %d time(s) between %d fetch attempts, slept %d", want, grantLandingFetchAttempts, sleeps)
	}
}

// TestGrantLandedTrueOnDifferentHashPatch is the ward#587 core: a change that landed
// under a DIFFERENT hash (HEAD not an ancestor) but is present by patch-id reads landed.
func TestGrantLandedTrueOnDifferentHashPatch(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base")
	base := mustGitRev(t, repo, "HEAD")

	// The run's local work: add an identical catalog block on the feature branch.
	runGit(t, repo, "checkout", "-b", "issue-587")
	if err := os.WriteFile(filepath.Join(repo, "catalog.txt"), []byte("catalog block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "catalog.txt")
	runGit(t, repo, "commit", "-m", "add catalog block")
	feat := mustGitRev(t, repo, "HEAD")

	// origin/main advanced independently but carries the SAME change under a new hash:
	// cherry-pick the feature commit onto a busy main, then add unrelated work on top.
	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", feat)
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "other.txt")
	runGit(t, repo, "commit", "-m", "unrelated later work")
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	// Leave HEAD on the local feature commit: it is neither origin/main nor an ancestor.
	runGit(t, repo, "checkout", feat)

	if base == feat {
		t.Fatal("test setup: feature commit must differ from base")
	}
	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	if landed, hasMain := r.grantLanded(t.Context(), repo); !landed || !hasMain {
		t.Fatalf("a grant whose change is on origin/main by patch-id (different hash) must read landed; got landed=%t hasMain=%t", landed, hasMain)
	}
}

// TestCheckExtraRepoLandedNoSalvageOnDifferentHash is the ward#587 end-to-end outcome:
// a content-present-but-different-hash grant reads landed and leaves NO salvage branch.
func TestCheckExtraRepoLandedNoSalvageOnDifferentHash(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "checkout", "-b", "issue-587")
	if err := os.WriteFile(filepath.Join(repo, "catalog.txt"), []byte("catalog block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "catalog.txt")
	runGit(t, repo, "commit", "-m", "add catalog block")
	feat := mustGitRev(t, repo, "HEAD")
	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", feat)
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, repo, "checkout", feat)
	runGit(t, repo, "remote", "add", "origin", repo)

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me", Mode: "claude", Issue: 587, Launched: true}
	rep, landed := r.checkExtraRepoLanded(t.Context(), env, targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}, repo)
	if !landed {
		t.Fatalf("a grant present on origin/main by patch-id must read landed, got unlanded: %+v", rep)
	}
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", salvageBranchPrefix+"*").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("a content-landed grant must not fabricate a salvage branch, got: %q", string(out))
	}
}

// TestUnlandedPatchCount checks the git-cherry `+` accounting: a patch already upstream
// counts 0 (landed), a genuinely-new commit counts 1 un-landed (ward#587).
func TestUnlandedPatchCount(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "x.txt")
	runGit(t, repo, "commit", "-m", "add x")
	feat := mustGitRev(t, repo, "HEAD")
	// origin/main carries the same patch under a different hash: 0 un-landed.
	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", feat)
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, repo, "checkout", feat)

	r := &Runner{Runner: &shell.Runner{Resolve: shell.PathResolver}}
	if got := r.unlandedPatchCount(t.Context(), repo); got != 0 {
		t.Fatalf("a patch already upstream must count 0 un-landed, got %d", got)
	}

	// Add a genuinely-new commit on the feature branch: now 1 un-landed.
	if err := os.WriteFile(filepath.Join(repo, "y.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "y.txt")
	runGit(t, repo, "commit", "-m", "add y (never lands)")
	if got := r.unlandedPatchCount(t.Context(), repo); got != 1 {
		t.Fatalf("one genuinely-new commit must count 1 un-landed, got %d", got)
	}
}

// fakeSalvageNotifier records the Forgejo verbs notifySalvage drives so the
// ward#518 routing (comment-on-carried-issue vs standalone-issue) is assertable.
type fakeSalvageNotifier struct {
	reopened     []int
	commented    []int
	commentBody  string
	created      int
	createdTitle string
	createdBody  string
}

func (f *fakeSalvageNotifier) reopenIssue(_ context.Context, _, _ string, number int) error {
	f.reopened = append(f.reopened, number)
	return nil
}

func (f *fakeSalvageNotifier) commentIssue(_ context.Context, _, _ string, number int, body string) error {
	f.commented = append(f.commented, number)
	f.commentBody = body
	return nil
}

func (f *fakeSalvageNotifier) createIssue(_ context.Context, _, _, title, body string) (int, error) {
	f.created++
	f.createdTitle = title
	f.createdBody = body
	return 900, nil
}

// TestNotifySalvageCarriedIssueRepoensAndComments covers ward#518 deliverable 2:
// a carried salvage reopens + comments on its issue, filing no standalone issue.
func TestNotifySalvageCarriedIssueRepoensAndComments(t *testing.T) {
	f := &fakeSalvageNotifier{}
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me", Mode: "claude", Issue: 518}
	report := salvageReport{
		Repo:   env.repo(),
		Mode:   "claude",
		Branch: "ward-salvage/ward-abc123",
		Reason: reasonConflict,
		Base:   env.Base,
		Issue:  518,
	}
	if err := notifySalvage(t.Context(), f, env, report); err != nil {
		t.Fatalf("notifySalvage: %v", err)
	}
	if len(f.reopened) != 1 || f.reopened[0] != 518 {
		t.Errorf("carried salvage must reopen #518, got reopened=%v", f.reopened)
	}
	if len(f.commented) != 1 || f.commented[0] != 518 {
		t.Errorf("carried salvage must comment on #518, got commented=%v", f.commented)
	}
	if f.created != 0 {
		t.Errorf("carried salvage must NOT file a standalone issue, got created=%d", f.created)
	}
	for _, want := range []string{"Reopened", "ward-salvage/ward-abc123", string(reasonConflict), "git fetch"} {
		if !strings.Contains(f.commentBody, want) {
			t.Errorf("carried-issue comment missing %q\n---\n%s", want, f.commentBody)
		}
	}
}

// TestNotifySalvageNoIssueFilesOneStandalone covers ward#518 deliverable 3: a
// freeform run files exactly one standalone issue, never reopen/append.
func TestNotifySalvageNoIssueFilesOneStandalone(t *testing.T) {
	f := &fakeSalvageNotifier{}
	env := reapEnv{Owner: "coilyco-flight-deck", Name: "ward", Base: "https://forgejo.coilysiren.me", Mode: "claude", Issue: 0}
	report := salvageReport{
		Repo:   env.repo(),
		Mode:   "claude",
		Branch: "ward-salvage/ward-def456",
		Reason: reasonConflict,
		Base:   env.Base,
	}
	if err := notifySalvage(t.Context(), f, env, report); err != nil {
		t.Fatalf("notifySalvage: %v", err)
	}
	if f.created != 1 {
		t.Errorf("freeform salvage must file exactly one standalone issue, got created=%d", f.created)
	}
	if len(f.reopened) != 0 || len(f.commented) != 0 {
		t.Errorf("freeform salvage must not reopen/comment a carried issue, got reopened=%v commented=%v", f.reopened, f.commented)
	}
	if !strings.HasPrefix(f.createdTitle, salvageIssueTitlePrefix) {
		t.Errorf("standalone salvage issue title %q missing %q prefix", f.createdTitle, salvageIssueTitlePrefix)
	}
}

func runGit(t *testing.T, dir string, argv ...string) {
	t.Helper()
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", argv, err, string(out))
	}
}

func runGitCommitAt(t *testing.T, dir, date, path, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", path)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, string(out))
	}
	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_DATE="+date,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, string(out))
	}
}

func mustGitRev(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", ref)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s failed: %v\n%s", ref, err, string(out))
	}
	return strings.TrimSpace(string(out))
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
