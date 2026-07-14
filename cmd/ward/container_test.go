package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/urfave/cli/v3"
)

func stubContainerBootstrapStage(t *testing.T) {
	t.Helper()
	prev := stageWardBootstrapBinary
	stageWardBootstrapBinary = func(_ context.Context, dir, wardSource, wardVersion string) error {
		if wardSource != "" {
			t.Fatalf("test bootstrap stage unexpectedly asked to build from source %q", wardSource)
		}
		if wardVersion != "" {
			t.Fatalf("test bootstrap stage unexpectedly asked to resolve release %q", wardVersion)
		}
		return os.WriteFile(filepath.Join(dir, "ward"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	t.Cleanup(func() { stageWardBootstrapBinary = prev })
}

// TestSweepStaleContainerAssets reclaims dirs past the TTL (left by detached
// runs) while sparing fresh ones and unrelated dirs.
func TestSweepStaleContainerAssets(t *testing.T) {
	tmp := t.TempDir()
	stale := filepath.Join(tmp, containerAssetsPrefix+"stale")
	fresh := filepath.Join(tmp, containerAssetsPrefix+"fresh")
	other := filepath.Join(tmp, "unrelated-dir")
	for _, d := range []string{stale, fresh, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * containerAssetsTTL())
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}
	sweepStaleContainerAssets(tmp, nil)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale asset dir should have been swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh asset dir must survive the sweep")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("unrelated dir must not be touched")
	}
}

func TestSweepStaleContainerAssetsSkipsLiveDirs(t *testing.T) {
	tmp := t.TempDir()
	live := filepath.Join(tmp, containerAssetsPrefix+"live")
	stale := filepath.Join(tmp, containerAssetsPrefix+"stale")
	for _, d := range []string{live, stale} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * containerAssetsTTL())
	for _, d := range []string{live, stale} {
		if err := os.Chtimes(d, past, past); err != nil {
			t.Fatal(err)
		}
	}
	sweepStaleContainerAssets(tmp, map[string]bool{live: true})
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("live asset dir must survive the sweep: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale asset dir should still be swept when it is not live")
	}
}

func fakeDockerLiveAssetsRunner(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  ps)\n" +
		"    printf '%s\\n' c1 c2\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"  inspect)\n" +
		"    case \"$4\" in\n" +
		"      c1) printf '%s' " + shellQuote(`[{"Source":"/tmp/asset-c1","Destination":"/opt/ward"}]`) + " ; exit 0 ;;\n" +
		"      c2) printf '%s' " + shellQuote(`[{"Source":"/tmp/asset-c2","Destination":"/opt/ward"},{"Source":"/tmp/ignore","Destination":"/tmp"}]`) + " ; exit 0 ;;\n" +
		"    esac\n" +
		"    exit 1\n" +
		"    ;;\n" +
		"esac\n" +
		"exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write fake docker: %v", err)
	}
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}

func TestLiveContainerAssetDirs(t *testing.T) {
	r := fakeDockerLiveAssetsRunner(t)
	live, ok := r.liveContainerAssetDirs(context.Background())
	if !ok {
		t.Fatal("liveContainerAssetDirs should succeed on the fake docker output")
	}
	for _, want := range []string{"/tmp/asset-c1", "/tmp/asset-c2"} {
		if !live[want] {
			t.Fatalf("liveContainerAssetDirs missing %q in %v", want, live)
		}
	}
	if live["/tmp/ignore"] {
		t.Fatal("liveContainerAssetDirs must ignore non-/opt/ward mounts")
	}
}

func TestParseRepoRef(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"coilyco-gaming/eco-app", "coilyco-gaming", "eco-app", false},
		{"coilyco-gaming/eco-app.git", "coilyco-gaming", "eco-app", false},
		{"https://forgejo.coilysiren.me/coilyco-gaming/eco-app.git", "coilyco-gaming", "eco-app", false},
		{"https://forgejo.coilysiren.me/coilyco-gaming/eco-app", "coilyco-gaming", "eco-app", false},
		{"git@github.com:coilyco-gaming/eco-app.git", "coilyco-gaming", "eco-app", false},
		{"", "", "", true},
		{"not-a-ref", "", "", true},
	}
	for _, c := range cases {
		got, err := parseRepoRef(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseRepoRef(%q): want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseRepoRef(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Owner != c.wantOwner || got.Name != c.wantName {
			t.Errorf("parseRepoRef(%q) = %s/%s, want %s/%s", c.in, got.Owner, got.Name, c.wantOwner, c.wantName)
		}
	}
}

func TestTargetFromRemoteURL(t *testing.T) {
	cases := []struct {
		in       string
		wantSlug string
		wantErr  bool
	}{
		{"https://forgejo.coilysiren.me/coilyco-flight-deck/ward.git", "coilyco-flight-deck/ward", false},
		{"git@github.com:coilyco-flight-deck/ward.git", "coilyco-flight-deck/ward", false},
		{"https://forgejo.coilysiren.me/coilyco-gaming/eco-app", "coilyco-gaming/eco-app", false},
		{"garbage", "", true},
	}
	for _, c := range cases {
		got, err := targetFromRemoteURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("targetFromRemoteURL(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("targetFromRemoteURL(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.slug() != c.wantSlug {
			t.Errorf("targetFromRemoteURL(%q) = %q, want %q", c.in, got.slug(), c.wantSlug)
		}
	}
}

func TestDirectorContainerNameUniqueAndSafe(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}
	suffixRe := regexp.MustCompile(`^[a-hjkm-pqrstuvwxyz]{2}[456789]{2}$`)
	a := containerRoleName(roleDirector, modeClaude, repo, 0, "ab85")
	b := containerRoleName(roleDirector, modeClaude, repo, 0, "cd97")
	if a == b {
		t.Fatalf("two issueless surface runs must not collide on the agent-id suffix: %q == %q", a, b)
	}
	if !strings.HasPrefix(a, "director-claude-") {
		t.Errorf("name %q missing the director-<driver>- lead", a)
	}
	if strings.HasPrefix(a, "ward-") {
		t.Errorf("name %q still carries the dropped ward- prefix", a)
	}
	if !suffixRe.MatchString(strings.TrimPrefix(a, "director-claude-")) {
		t.Errorf("director name %q does not end in a dictatable agent-id suffix", a)
	}
	// docker forbids these; the director name must stay safe even though it no
	// longer carries the repo name.
	weird := targetRepo{Owner: "x", Name: "we/ird name!"}
	got := containerRoleName(roleDirector, modeClaude, weird, 0, "ab85")
	for _, bad := range []string{"/", " ", "!"} {
		if strings.Contains(got, bad) {
			t.Errorf("director name %q still contains %q", got, bad)
		}
	}
}

func TestDictatableIDShape(t *testing.T) {
	re := regexp.MustCompile(`^[a-hjkm-pqrstuvwxyz]{2}[456789]{2}$`)
	for i := 0; i < 32; i++ {
		if got := dictatableID(); !re.MatchString(got) {
			t.Fatalf("dictatableID() = %q, want %s", got, re)
		}
	}
}

func TestBuildUpPlanDirectorUsesDictatableSuffix(t *testing.T) {
	prev := directorSurfaceSessionSuffix
	directorSurfaceSessionSuffix = func() string { return "ab85" }
	t.Cleanup(func() { directorSurfaceSessionSuffix = prev })

	probe := &cli.Command{
		Name:  "probe",
		Flags: tailnetProbeFlags(),
		Action: func(_ context.Context, c *cli.Command) error {
			p, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, roleDirector, t.TempDir(), t.TempDir(), nil, false)
			if err != nil {
				return err
			}
			if got, want := p.Name, "director-claude-ab85"; got != want {
				t.Fatalf("director buildUpPlan name = %q, want %q", got, want)
			}
			if !regexp.MustCompile(`^director-[a-z]+-[a-hjkm-pqrstuvwxyz]{2}[456789]{2}$`).MatchString(p.Name) {
				t.Fatalf("director buildUpPlan name = %q, want the dictatable agent-id shape", p.Name)
			}
			return nil
		},
	}
	if err := probe.Run(context.Background(), []string{"probe"}); err != nil {
		t.Fatalf("probe run: %v", err)
	}
}

func TestAdvisorResearchPlanUsesIssueScope(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 818, Forge: forgeForgejo}
	base := upPlan{Mode: modeClaude, Repo: targetRepo{Owner: ref.Owner, Name: ref.Repo}, Machine: "deadbeef"}
	got := advisorResearchPlan(base, ref)
	if want := "advisor-claude-ward-818"; got.Name != want {
		t.Fatalf("advisor research name = %q, want %q", got.Name, want)
	}
	if other := advisorResearchPlan(upPlan{Mode: modeClaude, Repo: base.Repo, Machine: "beadfeed"}, ref); other.Name != got.Name {
		t.Fatalf("advisor research name must be issue-scoped, got %q and %q", other.Name, got.Name)
	}
}

func TestEngineerContainerNameIsRepoIssueUnique(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}
	// The engineer name is unique by repo+issue, no random suffix (ward#364): the
	// machine arg is ignored, identity rides the ward.machine label instead.
	got := containerRoleName(roleEngineer, modeClaude, repo, 140, "a1b2c3d4")
	want := "engineer-claude-ward-140"
	if got != want {
		t.Errorf("engineer name = %q, want %q", got, want)
	}
	// The role, harness, repo, and issue must all be legible at a glance.
	for _, frag := range []string{"engineer", "claude", "ward", "140"} {
		if !strings.Contains(got, frag) {
			t.Errorf("name %q missing %q", got, frag)
		}
	}
	// Same repo+issue is deterministic (the machine suffix never enters the name).
	if other := containerRoleName(roleEngineer, modeClaude, repo, 140, "e5f6a7b8"); other != got {
		t.Errorf("engineer name must be machine-independent: %q != %q", other, got)
	}
	// A different issue and a different harness each change the name.
	if containerRoleName(roleEngineer, modeClaude, repo, 141, "a1b2c3d4") == got {
		t.Error("different issues on the same repo must produce different names")
	}
	if containerRoleName(roleEngineer, modeCodex, repo, 140, "a1b2c3d4") == got {
		t.Error("different harnesses on the same issue must produce different names")
	}
	// docker-forbidden characters in the repo name must be sanitized away.
	weird := targetRepo{Owner: "x", Name: "we/ird name!"}
	dirty := containerRoleName(roleEngineer, modeOpencode, weird, 7, "deadbeef")
	for _, bad := range []string{"/", " ", "!"} {
		if strings.Contains(dirty, bad) {
			t.Errorf("sanitized name %q still contains %q", dirty, bad)
		}
	}
}

func TestUpPlanLabels(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}
	// An engineer run: role/driver/repo + the carried issue + a machine id.
	eng := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 364, Machine: "deadbeef"}
	got := strings.Join(eng.labels(), " ")
	for _, want := range []string{
		"ward=true", "ward.role=engineer", "ward.driver=claude",
		"ward.repo=coilyco-flight-deck/ward", "ward.issue=364", "ward.machine=deadbeef",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("engineer labels %q missing %q", got, want)
		}
	}
	// An issueless run (advisor here): no ward.issue label, role defaults absent -> session.
	adv := upPlan{Role: roleAdvisor, Mode: modeCodex, Repo: repo, Machine: "beadfeed"}
	got = strings.Join(adv.labels(), " ")
	if strings.Contains(got, "ward.issue") {
		t.Errorf("issueless run must not carry a ward.issue label: %q", got)
	}
	if !strings.Contains(got, "ward.role=advisor") || !strings.Contains(got, "ward.driver=codex") {
		t.Errorf("advisor labels %q missing role/driver", got)
	}
	qa := upPlan{Role: roleQA, Mode: modeGoose, Repo: repo, Machine: "cafefeed"}
	got = strings.Join(qa.labels(), " ")
	if strings.Contains(got, "ward.issue") {
		t.Errorf("QA run must not carry a ward.issue label: %q", got)
	}
	if !strings.Contains(got, "ward.role=qa") || !strings.Contains(got, "ward.driver=goose") {
		t.Errorf("qa labels %q missing role/driver", got)
	}
	// A plan with no Role set falls back to the session role so the label is never blank.
	bare := upPlan{Mode: modeClaude, Repo: repo}
	if !strings.Contains(strings.Join(bare.labels(), " "), "ward.role=session") {
		t.Errorf("roleless plan should label ward.role=session, got %v", bare.labels())
	}
}

func TestLeastAccessMountsDefaultIsCwdOnly(t *testing.T) {
	mounts := leastAccessMounts("/home/kai/projects/coilyco-bridge/agentic-os-kai", mountOpts{AssetsDir: "/tmp/ward-assets"})
	// The target repo must never be a host bind: only cwd + assets binds,
	// plus the staged entrypoint file and the gitcache named volume.
	var hostBinds []string
	for _, m := range mounts {
		if !m.Volume {
			hostBinds = append(hostBinds, m.Source)
		}
		if !m.Volume && !m.ReadOnly && m.Target != containerGitcacheMnt {
			t.Errorf("host bind %q is writable; least-access binds are read-only", m.Source)
		}
	}
	wantBinds := []string{
		"/home/kai/projects/coilyco-bridge/agentic-os-kai",
		"/tmp/ward-assets",
		filepath.Join("/tmp/ward-assets", containerEntrypointRel),
	}
	if !slices.Equal(hostBinds, wantBinds) {
		t.Errorf("default host binds = %v, want exactly %v (cwd + assets + entrypoint, no target repo)", hostBinds, wantBinds)
	}
}

func TestLeastAccessMountsOptIns(t *testing.T) {
	mounts := leastAccessMounts("/cwd", mountOpts{AssetsDir: "/a", AWSHome: "/home/kai/.aws", WardSource: "/src/ward"})
	targets := map[string]bool{}
	for _, m := range mounts {
		targets[m.Target] = true
	}
	for _, want := range []string{containerContextMount, containerGitcacheMnt, containerWardAssets, containerEntrypointPath, containerAWSMount, containerWardSrcMount} {
		if !targets[want] {
			t.Errorf("opt-in mount set missing %q", want)
		}
	}
}

func TestModeContextLevelLadder(t *testing.T) {
	if modeClaude.contextLevel() <= modeCodex.contextLevel() {
		t.Error("claude must carry more context than codex")
	}
	if modeCodex.contextLevel() <= modeOpencode.contextLevel() {
		t.Error("codex must carry more context than opencode")
	}
	if modeOpencode.contextLevel() != 0 {
		t.Errorf("opencode is the minimal-context floor, got %d", modeOpencode.contextLevel())
	}
	// goose is a full carry-to-merge harness: same context tier as claude.
	if modeGoose.contextLevel() != modeClaude.contextLevel() {
		t.Errorf("goose must carry the same context level as claude, got %d", modeGoose.contextLevel())
	}
	if modeClaude.agentBinary() != "claude" || modeCodex.agentBinary() != "codex" ||
		modeOpencode.agentBinary() != "opencode" || modeGoose.agentBinary() != "goose" {
		t.Error("mode -> agent binary mapping wrong")
	}
}

// ward#148: claude+goose (the full carry-to-merge harnesses) keep parity on the
// headless pre-flight, so both expose a host one-shot argv; codex/opencode don't yet.
func TestHostPreflightArgvParity(t *testing.T) {
	want := map[containerMode][]string{
		modeClaude: {"claude", "-p", "carry it?"},
		modeGoose:  {"goose", "run", "-t", "carry it?"},
	}
	for m, exp := range want {
		argv, ok := m.hostPreflightArgv("carry it?")
		if !ok {
			t.Errorf("%s: expected a host pre-flight argv (parity with the other full carry-to-merge harness)", m)
			continue
		}
		if len(argv) != len(exp) {
			t.Errorf("%s: pre-flight argv = %v, want %v", m, argv, exp)
			continue
		}
		for i := range exp {
			if argv[i] != exp[i] {
				t.Errorf("%s: pre-flight argv[%d] = %q, want %q (full %v)", m, i, argv[i], exp[i], argv)
			}
		}
		if argv[0] != m.agentBinary() {
			t.Errorf("%s: pre-flight argv must start with the agent binary %q, got %q", m, m.agentBinary(), argv[0])
		}
	}
	// codex/opencode: no reliable host one-shot yet, so the pre-flight bows out and
	// the dispatch proceeds unguarded rather than fabricating a verdict.
	for _, m := range []containerMode{modeCodex, modeOpencode} {
		if argv, ok := m.hostPreflightArgv("carry it?"); ok {
			t.Errorf("%s: did not expect a host pre-flight argv yet, got %v", m, argv)
		}
	}
}

func TestParseMode(t *testing.T) {
	for _, ok := range []string{"claude", "codex", "opencode", "goose", "qwen"} { // qwen still parses (deprecated alias)
		if _, err := parseMode(ok); err != nil {
			t.Errorf("parseMode(%q) errored: %v", ok, err)
		}
	}
	if _, err := parseMode("gpt"); err == nil {
		t.Error("parseMode should reject unknown mode")
	}
}

// TestParseExtraRepos covers the --repo grant parsing (ward#230): refs,
// target drop, dedupe, and the two hard errors (bad ref, workspace collision).
func TestParseExtraRepos(t *testing.T) {
	target := targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}

	// Bare owner/name and a clone URL both resolve; order preserved.
	got, err := parseExtraRepos([]string{
		"coilyco-gaming/eco-protos",
		"https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard.git",
	}, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []targetRepo{
		{Owner: "coilyco-gaming", Name: "eco-protos"},
		{Owner: "coilyco-flight-deck", Name: "cli-guard"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d repos, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("repo[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The target itself, blanks, and exact duplicates are dropped (not errors).
	got, err = parseExtraRepos([]string{
		"coilyco-gaming/eco-app", // the target: no-op
		"  ",                     // blank
		"coilyco-gaming/eco-protos",
		"coilyco-gaming/eco-protos", // dup slug
	}, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "eco-protos" {
		t.Errorf("target/blank/dup not collapsed to one: %+v", got)
	}

	// A malformed ref is a hard error.
	if _, err := parseExtraRepos([]string{"not a repo ref"}, target); err == nil {
		t.Error("malformed --repo ref should error")
	}

	// Two grants whose names collide on /workspace/<name> is a hard error, even
	// across different owners (they would clobber the same working dir).
	if _, err := parseExtraRepos([]string{"orgA/shared", "orgB/shared"}, target); err == nil {
		t.Error("workspace-dir name collision should error")
	}
	// A grant colliding with the target's own workspace dir also errors.
	if _, err := parseExtraRepos([]string{"otherorg/eco-app"}, target); err == nil {
		t.Error("grant colliding with the target workspace dir should error")
	}
}

// TestAgentGrantFlagName pins the extra-repo grant as a repeatable "--repo" with the
// legacy "--with-repo" alias removed (ward#362), and that extraRepoGrant resolves it.
func TestAgentGrantFlagName(t *testing.T) {
	for _, cmd := range []*cli.Command{
		agentEngineerCommand(),
	} {
		var grant cli.Flag
		for _, f := range cmd.Flags {
			if slices.Contains(f.Names(), "repo") {
				grant = f
				break
			}
		}
		if grant == nil {
			t.Fatalf("%s: no --repo grant flag on the engineer surface", cmd.Name)
		}
		names := grant.Names()
		if slices.Contains(names, "with-repo") {
			t.Errorf("%s: the legacy --with-repo alias must be gone (ward#362); got %v", cmd.Name, names)
		}
		if _, ok := grant.(*cli.StringSliceFlag); !ok {
			t.Errorf("%s: grant flag is %T, want a repeatable *cli.StringSliceFlag", cmd.Name, grant)
		}
	}

	// And `--repo` reaches the reader buildUpPlan uses: extraRepoGrant must surface a
	// repeatable --repo (the engineer surface path, no alias) as the extra-repo grant.
	var got []string
	probe := &cli.Command{
		Name:  "probe",
		Flags: []cli.Flag{&cli.StringSliceFlag{Name: "repo"}},
		Action: func(_ context.Context, c *cli.Command) error {
			got = extraRepoGrant(c)
			return nil
		},
	}
	if err := probe.Run(context.Background(), []string{"probe", "--repo", "o/a", "--repo", "o/b"}); err != nil {
		t.Fatalf("probe run: %v", err)
	}
	if want := []string{"o/a", "o/b"}; !slices.Equal(got, want) {
		t.Errorf("--repo via extraRepoGrant = %v, want %v", got, want)
	}
}

// TestWardEnvExtraRepos asserts the grant list rides WARD_EXTRA_REPOS as a
// space-separated slug list, and is absent when no repo is granted (ward#230).
func TestWardEnvExtraRepos(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_EXTRA_REPOS"]; ok {
		t.Error("WARD_EXTRA_REPOS must be absent when no --repo is granted")
	}
	p.ExtraRepos = []targetRepo{
		{Owner: "coilyco-gaming", Name: "eco-protos"},
		{Owner: "coilyco-flight-deck", Name: "cli-guard"},
	}
	if got := p.wardEnv()["WARD_EXTRA_REPOS"]; got != "coilyco-gaming/eco-protos coilyco-flight-deck/cli-guard" {
		t.Errorf("WARD_EXTRA_REPOS = %q, want the space-separated slug list", got)
	}
	// And it must reach the docker argv as a single -e element (spaces and all).
	argv := dockerCreateArgv(p, "")
	var found bool
	for _, a := range argv {
		if a == "WARD_EXTRA_REPOS=coilyco-gaming/eco-protos coilyco-flight-deck/cli-guard" {
			found = true
		}
	}
	if !found {
		t.Errorf("WARD_EXTRA_REPOS not passed as one -e argv element: %v", argv)
	}
}

// TestWardEnvTargetIssue asserts the carried issue rides WARD_TARGET_ISSUE (ward#264)
// and is absent for a bare `container up` (Issue 0).
func TestWardEnvTargetIssue(t *testing.T) {
	p := sampleUpPlan()
	p.Issue = 0
	if _, ok := p.wardEnv()["WARD_TARGET_ISSUE"]; ok {
		t.Error("WARD_TARGET_ISSUE must be absent when no issue is carried (Issue 0)")
	}
	p.Issue = 264
	if got := p.wardEnv()["WARD_TARGET_ISSUE"]; got != "264" {
		t.Errorf("WARD_TARGET_ISSUE = %q, want 264", got)
	}
}

// TestWardEnvContainerName asserts the friendly docker --name (plan.Name) rides
// WARD_CONTAINER_NAME so in-container tooling can show it (ward#365).
func TestWardEnvContainerName(t *testing.T) {
	p := sampleUpPlan()
	if got := p.wardEnv()["WARD_CONTAINER_NAME"]; got != p.Name {
		t.Errorf("WARD_CONTAINER_NAME = %q, want the docker --name %q", got, p.Name)
	}
}

// TestWardEnvCorrelationEnvelope asserts the stable run metadata rides the
// container env so launchers and request headers can correlate the run.
func TestWardEnvCorrelationEnvelope(t *testing.T) {
	p := sampleUpPlan()
	env := p.wardEnv()
	for _, want := range []struct {
		key, value string
	}{
		{"WARD_RUN_ID", p.Name},
		{"WARD_HARNESS", string(p.Mode)},
		{"WARD_ISSUE_REF", p.Repo.slug() + "#140"},
		{"WARD_CONTEXT_LEVEL", "2"},
		{"WARD_VERSION", "v0.16.0"},
		{"WARD_TARGET_REPO", p.Repo.slug()},
	} {
		if got := env[want.key]; got != want.value {
			t.Errorf("%s = %q, want %q", want.key, got, want.value)
		}
	}
	if got := env["WARD_WORKFLOW"]; got != "" {
		t.Errorf("WARD_WORKFLOW = %q, want it absent for the merge-remote-main default", got)
	}
}

// TestWardEnvContainerMarker asserts every run exports the WARD_CONTAINER=1 fence
// marker host-only fleet scripts key off (ward#114).
func TestWardEnvContainerMarker(t *testing.T) {
	p := sampleUpPlan()
	if got := p.wardEnv()["WARD_CONTAINER"]; got != "1" {
		t.Errorf("WARD_CONTAINER = %q, want %q", got, "1")
	}
}

func sampleUpPlan() upPlan {
	repo := targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}
	return upPlan{
		Image:       "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os-full:latest",
		Name:        "engineer-claude-eco-app-140",
		Role:        roleEngineer,
		Machine:     "deadbeef",
		Issue:       140,
		Repo:        repo,
		Mode:        modeClaude,
		Branch:      "feat/foo",
		ForgejoBase: forgejoBaseURL,
		HostCwd:     "/cwd",
		Mounts:      leastAccessMounts("/cwd", mountOpts{AssetsDir: "/a"}),
		Interactive: true,
		TTY:         true,
		MemoryLimit: containerMemoryLimitDefault(),
		MemorySwap:  "4g",
		WardVersion: "v0.16.0",
	}
}

func TestLaunchStagingDirIsSnapReadable(t *testing.T) {
	// A snap-confined docker only reaches NON-hidden files under $HOME (ward#569,
	// ward#574), so the staging dir must be $HOME itself, never the hidden ~/.ward.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := launchStagingDir(); got != home {
		t.Fatalf("launchStagingDir() = %q, want the $HOME root %q (a hidden ~/.ward path is invisible to a snap docker)", got, home)
	}
	if strings.Contains(launchStagingDir(), "/.ward") {
		t.Error("launchStagingDir() must not sit under a hidden dir a snap docker cannot read")
	}
}

func TestLaunchStagingDirFallsBackToTmp(t *testing.T) {
	// With no resolvable $HOME the launch dir degrades to $TMPDIR rather than "".
	t.Setenv("HOME", "")
	if got := launchStagingDir(); got == "" {
		t.Error("launchStagingDir() with no $HOME must fall back to a real dir, got empty")
	}
}

func TestWriteContainerAssetsStagesUnderHome(t *testing.T) {
	// The assets bind-mount source is daemon-resolved, so it must land under $HOME
	// (never /tmp) for a snap docker daemon to see it at `docker run` (ward#574).
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubContainerBootstrapStage(t)
	dir, cleanup, err := writeContainerAssets(context.Background(), nil, "", "")
	if err != nil {
		t.Fatalf("writeContainerAssets: %v", err)
	}
	defer cleanup()
	if filepath.Dir(dir) != home {
		t.Errorf("assets dir %q must sit directly under $HOME %q, not /tmp", dir, home)
	}
	if !strings.HasPrefix(filepath.Base(dir), containerAssetsPrefix) {
		t.Errorf("assets dir %q must carry the sweep-recognizable prefix %q", dir, containerAssetsPrefix)
	}
}

func TestWriteContainerAssetsStagesWardBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prev := stageWardBootstrapBinary
	stageWardBootstrapBinary = func(_ context.Context, dir, wardSource, wardVersion string) error {
		if wardSource != "/src/ward" {
			t.Fatalf("stage bootstrap source = %q, want /src/ward", wardSource)
		}
		if wardVersion != "v0.1.2" {
			t.Fatalf("stage bootstrap version = %q, want v0.1.2", wardVersion)
		}
		return os.WriteFile(filepath.Join(dir, "ward"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	t.Cleanup(func() { stageWardBootstrapBinary = prev })

	dir, cleanup, err := writeContainerAssets(context.Background(), nil, "/src/ward", "v0.1.2")
	if err != nil {
		t.Fatalf("writeContainerAssets: %v", err)
	}
	defer cleanup()
	info, err := os.Stat(filepath.Join(dir, "ward"))
	if err != nil {
		t.Fatalf("staged ward binary missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("staged ward binary must be executable, mode %o", info.Mode())
	}
}

func TestDownloadWardBootstrapBinarySelectsLatestAssetBearingRelease(t *testing.T) {
	origBase := forgejoBaseURL
	forgejoBaseURL = ""
	t.Cleanup(func() { forgejoBaseURL = origBase })

	arch := runtime.GOARCH
	assetName := bootstrapWardBinaryAssetName(arch)

	var listHits, assetHits map[string]int
	listHits = map[string]int{}
	assetHits = map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Query().Get("page") {
			case "1":
				listHits["1"]++
				_, _ = fmt.Fprint(w, `[
					{"tag_name":"v0.580.0","draft":false,"prerelease":false,"assets":[]},
					{"tag_name":"v0.579.0","draft":false,"prerelease":false,"assets":[]}
				]`)
			case "2":
				listHits["2"]++
				_, _ = fmt.Fprint(w, `[
					{"tag_name":"v0.578.0","draft":false,"prerelease":false,"assets":[{"name":"`+assetName+`"}]}
				]`)
			case "3":
				listHits["3"]++
				_, _ = fmt.Fprint(w, `[]`)
			default:
				t.Fatalf("unexpected releases page %q", r.URL.Query().Get("page"))
			}
		case strings.Contains(r.URL.Path, "/releases/download/v0.578.0/"+assetName):
			assetHits["v0.578.0"]++
			_, _ = w.Write([]byte("bootstrapped ward"))
		case strings.Contains(r.URL.Path, "/releases/download/v0.580.0/") || strings.Contains(r.URL.Path, "/releases/download/v0.579.0/"):
			t.Fatalf("bootstrap selection should skip assetless releases, hit %q", r.URL.Path)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	forgejoBaseURL = srv.URL

	dir := t.TempDir()
	if err := downloadWardBootstrapBinary(context.Background(), "dev", filepath.Join(dir, "ward")); err != nil {
		t.Fatalf("downloadWardBootstrapBinary: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ward"))
	if err != nil {
		t.Fatalf("read staged binary: %v", err)
	}
	if got := string(data); got != "bootstrapped ward" {
		t.Fatalf("staged binary = %q, want %q", got, "bootstrapped ward")
	}
	if got := assetHits["v0.578.0"]; got != 1 {
		t.Fatalf("selected release asset hits = %d, want 1", got)
	}
	if got := len(listHits); got != 2 {
		t.Fatalf("releases list pages = %v, want two pages scanned", listHits)
	}
}

func TestDownloadWardBootstrapBinaryReportsReleaseAssetsNotReady(t *testing.T) {
	origBase := forgejoBaseURL
	forgejoBaseURL = ""
	t.Cleanup(func() { forgejoBaseURL = origBase })

	arch := runtime.GOARCH
	assetName := bootstrapWardBinaryAssetName(arch)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/releases/download/v0.544.0/"+assetName):
			http.Error(w, "Not Found", http.StatusNotFound)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	forgejoBaseURL = srv.URL

	dir := t.TempDir()
	err := downloadWardBootstrapBinary(context.Background(), "v0.544.0", filepath.Join(dir, "ward"))
	if err == nil {
		t.Fatal("downloadWardBootstrapBinary should fail on a missing release asset")
	}
	if !isReleaseAssetsNotReadyError(err) {
		t.Fatalf("download error = %v, want release-assets-not-ready classification", err)
	}
	for _, want := range []string{"v0.544.0", assetName, "release-assets-not-ready"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in error %v", want, err)
		}
	}
}

func TestSweepStaleLaunchEnvFiles(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, launchEnvFilePrefix+"fresh")
	stale := filepath.Join(dir, launchEnvFilePrefix+"stale")
	other := filepath.Join(dir, "unrelated-file")
	for _, p := range []string{fresh, stale, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	// Age the stale orphan past the TTL; leave the others recent.
	old := time.Now().Add(-2 * containerAssetsTTL())
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("age stale file: %v", err)
	}

	sweepStaleLaunchEnvFiles(dir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("past-TTL env-file orphan should have been swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a within-TTL env-file (a concurrent launch's) must be left alone")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a non-env-file must never be swept")
	}
}

func TestDockerCreateArgvShape(t *testing.T) {
	argv := dockerCreateArgv(sampleUpPlan(), "/tmp/ward-env-xyz")
	joined := strings.Join(argv, " ")

	if argv[0] != "run" {
		t.Errorf("argv[0] = %q, want run", argv[0])
	}
	for _, want := range []string{
		"--name engineer-claude-eco-app-140",
		"--label " + containerLabel,
		"--label ward.role=engineer",
		"--label ward.driver=claude",
		"--label ward.repo=coilyco-gaming/eco-app",
		"--label ward.machine=deadbeef",
		"--label ward.issue=140",
		"--entrypoint " + containerEntrypointPath,
		"--memory=2g",
		"--memory-swap=4g",
		"-it",
		"--env-file /tmp/ward-env-xyz",
		"-e WARD_CONTAINER_NAME=engineer-claude-eco-app-140",
		"-e WARD_TARGET_REPO=coilyco-gaming/eco-app",
		"-e WARD_MODE=claude",
		"-e WARD_CONTEXT_LEVEL=2",
		"-e WARD_BRANCH=feat/foo",
		"-e WARD_VERSION=v0.16.0",
		"-e TERM=xterm-256color",
		"-e COLORTERM=truecolor",
		"-e WARD_SUBSTRATE_TTL=" + containerSubstrateTTL,
		"-e WARD_SUBSTRATE_SEED=" + containerSubstrateSeed,
		"-e WARD_SUBSTRATE_MANIFEST=" + containerSubstrateManifest,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("docker argv missing %q\n got: %s", want, joined)
		}
	}
	// The image is the final arg.
	if argv[len(argv)-1] != "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os-full:latest" {
		t.Errorf("image must be the final arg, got %q", argv[len(argv)-1])
	}
}

func TestDockerCreateArgvNoSecretLeak(t *testing.T) {
	// The token must never be in the argv: it rides --env-file only.
	argv := dockerCreateArgv(sampleUpPlan(), "/tmp/ward-env-xyz")
	for _, a := range argv {
		if strings.Contains(strings.ToLower(a), "token") || strings.Contains(a, "FORGEJO_TOKEN") {
			t.Errorf("argv element %q looks like a leaked secret", a)
		}
	}
}

func TestDockerCreateArgvDetached(t *testing.T) {
	p := sampleUpPlan()
	p.Interactive = false
	argv := dockerCreateArgv(p, "")
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "-it") {
		t.Error("non-interactive run must not pass -it")
	}
	if !strings.Contains(joined, "-d") {
		t.Error("non-interactive run must pass -d")
	}
	if strings.Contains(joined, "--env-file") {
		t.Error("empty env-file path must omit the flag")
	}
}

// ward#323: the in-container dispatch creates the sibling with ONLY volume mounts
// (host binds are docker-cp'd in after); bind sources don't resolve on the daemon.
func TestDockerCreateNoBindsArgv(t *testing.T) {
	argv := dockerCreateNoBindsArgv(sampleUpPlan(), "/tmp/ward-env-xyz")
	joined := strings.Join(argv, " ")
	if argv[0] != "create" {
		t.Errorf("argv[0] = %q, want create (a stopped container to cp into)", argv[0])
	}
	for _, want := range []string{
		"--name engineer-claude-eco-app-140",
		"--entrypoint " + containerEntrypointPath,
		"-v " + containerGitcacheVol + ":" + containerGitcacheMnt, // the named volume survives
		"--memory=2g",
		"--memory-swap=4g",
		"--env-file /tmp/ward-env-xyz",
		"-e WARD_TARGET_REPO=coilyco-gaming/eco-app",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("create argv missing %q\n got: %s", want, joined)
		}
	}
	// The host-path binds (cwd context, assets dir) must NOT be -v mounts here.
	for _, banned := range []string{"/cwd:" + containerContextMount, "/a:" + containerWardAssets} {
		if strings.Contains(joined, banned) {
			t.Errorf("create argv must not bind host path %q (it is docker-cp'd in)\n got: %s", banned, joined)
		}
	}
	for _, a := range argv {
		if a == "-d" || a == "-it" || a == "-i" {
			t.Errorf("create makes a stopped container; run-mode flag %q belongs to start", a)
		}
	}
}

// hostBindMounts returns exactly the non-volume mounts - the ones docker-cp'd into a
// sibling - and never the named volume (ward#323).
func TestHostBindMounts(t *testing.T) {
	binds := hostBindMounts(sampleUpPlan())
	for _, m := range binds {
		if m.Volume {
			t.Errorf("hostBindMounts returned a volume mount %s:%s", m.Source, m.Target)
		}
	}
	var targets []string
	for _, m := range binds {
		targets = append(targets, m.Target)
	}
	joined := strings.Join(targets, " ")
	if !strings.Contains(joined, containerContextMount) || !strings.Contains(joined, containerWardAssets) || !strings.Contains(joined, containerEntrypointPath) {
		t.Errorf("hostBindMounts should include the cwd context + assets + entrypoint binds, got targets %v", targets)
	}
	if strings.Contains(joined, containerGitcacheMnt) {
		t.Error("hostBindMounts must exclude the gitcache named volume")
	}
}

func TestDockerCreateArgvAttachedNoTTY(t *testing.T) {
	// Attached (not detached) but no terminal: -i to keep stdin open, never -it
	// (docker rejects -t without a terminal), and never -d (still attached).
	p := sampleUpPlan()
	p.TTY = false
	argv := dockerCreateArgv(p, "")
	// Exact-arg checks: the container name ("...app-deadbeef") contains the
	// substring "-d", so substring matching would false-positive.
	has := func(flag string) bool {
		for _, a := range argv {
			if a == flag {
				return true
			}
		}
		return false
	}
	if has("-it") {
		t.Error("attached no-TTY run must not pass -it")
	}
	if has("-d") {
		t.Error("attached no-TTY run must not pass -d (it is not detached)")
	}
	if !has("-i") {
		t.Errorf("attached no-TTY run must pass -i, got: %s", strings.Join(argv, " "))
	}
}

// TestContainerNamespaceHiddenPlumbingOnly locks in ward#263: the container
// umbrella is Hidden with only reap+bootstrap (Hidden); up/exec/down/ls are gone.
func TestContainerNamespaceHiddenPlumbingOnly(t *testing.T) {
	c := containerCommand()
	if !c.Hidden {
		t.Error("container umbrella must be Hidden so `ward --help` drops it (ward#263)")
	}
	if c.Before != nil {
		t.Fatal("container bootstrap must not run the edge WARD_CONFIG_REF guard")
	}
	got := map[string]bool{}
	for _, sub := range c.Commands {
		got[sub.Name] = true
		if !sub.Hidden {
			t.Errorf("remaining container leaf %q must be Hidden (entrypoint-internal)", sub.Name)
		}
	}
	for _, want := range []string{"reap", "bootstrap"} {
		if !got[want] {
			t.Errorf("entrypoint-internal leaf %q must stay registered+resolvable", want)
		}
	}
	for _, gone := range []string{"up", "exec", "down", "ls", "list"} {
		if got[gone] {
			t.Errorf("retired user-facing verb %q must be removed (ward#263)", gone)
		}
	}
}

// TestEntrypointContainerVerbsResolve is the static acceptance gate (ward#263):
// every `ward container <verb>` the entrypoint invokes must resolve to a leaf.
func TestEntrypointContainerVerbsResolve(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/entrypoint.sh")
	if err != nil {
		t.Fatalf("read embedded entrypoint: %v", err)
	}
	registered := map[string]bool{}
	for _, sub := range containerCommand().Commands {
		registered[sub.Name] = true
	}
	re := regexp.MustCompile(`ward container ([a-z][a-z0-9-]*)`)
	var found int
	// Skip comments and string-emitting builtins (echo/printf/log/...): prose like
	// "the ward container entrypoint" is a noun phrase, not an invocation.
	emitter := regexp.MustCompile(`^(echo|printf|log|cat|die)\b`)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || emitter.MatchString(trimmed) {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			found++
			if !registered[m[1]] {
				t.Errorf("entrypoint calls `ward container %s` but no such leaf is registered", m[1])
			}
		}
	}
	if found == 0 {
		t.Fatal("expected the entrypoint to invoke at least one `ward container <verb>`")
	}
}

func TestDockerExitedListArgv(t *testing.T) {
	argv := strings.Join(dockerExitedListArgv(), " ")
	for _, want := range []string{"ps", "-a", "label=" + containerLabel, "status=exited", "{{.Names}}"} {
		if !strings.Contains(argv, want) {
			t.Errorf("exited-list argv %q missing %q", argv, want)
		}
	}
}

func TestStaleContainersToReap(t *testing.T) {
	// `docker ps` lists newest first; the sweep keeps the leading `keep` for
	// post-mortem and returns the older tail for removal.
	const ps = "ward-c-newest\nward-c-2\nward-c-3\nward-c-oldest\n"
	cases := []struct {
		name string
		in   string
		keep int
		want []string
	}{
		{"keeps newest, reaps tail", ps, 2, []string{"ward-c-3", "ward-c-oldest"}},
		{"keep covers all", ps, 4, nil},
		{"keep exceeds count", ps, 10, nil},
		{"keep zero reaps all", "ward-a\nward-b\n", 0, []string{"ward-a", "ward-b"}},
		{"negative keep clamps to zero", "ward-a\n", -3, []string{"ward-a"}},
		{"blank lines ignored", "\nward-a\n\n  \nward-b\n", 1, []string{"ward-b"}},
		{"empty input", "", 2, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := staleContainersToReap(c.in, c.keep)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("staleContainersToReap(keep=%d) = %v, want %v", c.keep, got, c.want)
			}
		})
	}
}

func TestDockerRmArgv(t *testing.T) {
	if got := dockerRmArgv(nil); got != nil {
		t.Errorf("empty names should yield nil argv, got %v", got)
	}
	got := strings.Join(dockerRmArgv([]string{"ward-a", "ward-b"}), " ")
	if got != "rm ward-a ward-b" {
		t.Errorf("rm argv wrong: %q", got)
	}
	// The sweep targets only already-exited containers, so -f is never added.
	if strings.Contains(got, "-f") {
		t.Errorf("stale sweep must not force-kill: %q", got)
	}
}

func TestImageRef(t *testing.T) {
	cases := []struct{ image, tag, want string }{
		{"forgejo.coilysiren.me/coilyco-flight-deck/agentic-os", "latest", "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest"},
		{"forgejo.coilysiren.me/coilyco-flight-deck/agentic-os", "", "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest"},
		{"forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:v1.2.3", "latest", "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:v1.2.3"},
		{"repo@sha256:abc", "latest", "repo@sha256:abc"},
	}
	for _, c := range cases {
		if got := imageRef(c.image, c.tag); got != c.want {
			t.Errorf("imageRef(%q,%q) = %q, want %q", c.image, c.tag, got, c.want)
		}
	}
}

// TestEntrypointInstallsPreCommitHooks locks the ward#133 fix: the entrypoint
// registers pre-commit hooks after the clone (a fresh clone ships none).
func TestEntrypointInstallsPreCommitHooks(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"install_precommit_hooks()",         // the function exists
		"install_precommit_hooks \"$work\"", // main() invokes it on the clone
		".pre-commit-config.yaml",           // gated on a config being present
		"pre-commit install",                // registers the default hook
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#133 pre-commit parity)", want)
		}
	}
	// It must run after the clone (work exists) and before the agent launches,
	// so the hooks are in place for the first commit.
	clone := strings.Index(script, "work=\"$(clone_target)\"")
	install := strings.Index(script, "install_precommit_hooks \"$work\"")
	launch := strings.Index(script, "log \"launching $WARD_AGENT")
	if clone < 0 || install < 0 || launch < 0 {
		t.Fatalf("entrypoint markers not found: clone=%d install=%d launch=%d", clone, install, launch)
	}
	if clone >= install || install >= launch {
		t.Errorf("pre-commit install must run after clone and before launch: clone=%d install=%d launch=%d", clone, install, launch)
	}
}

// TestEntrypointOllamaSmokeGate locks ward#487: the entrypoint carries a pre-launch
// Ollama-reachability gate, run after the claude smoke test and before launch.
func TestEntrypointOllamaSmokeGate(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"smoke_test_ollama_reachable()",      // the function exists
		"smoke_test_ollama_reachable",        // main() invokes it
		"WARD_SMOKE_TEST_SKIP",               // shares claude's bypass switch
		"WARD_OLLAMA_URL",                    // opencode endpoint source
		"OLLAMA_HOST",                        // goose endpoint source (config.yaml)
		"the local-harness analog of claude", // documents the symmetry (ward#487)
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#487 ollama smoke gate)", want)
		}
	}
	// It must run after the claude smoke test and before the agent launches, so a
	// dead local model aborts before the hang, in parity with the claude gate.
	claude := strings.Index(script, "smoke_test_claude_auth\n")
	ollama := strings.LastIndex(script, "smoke_test_ollama_reachable\n")
	launch := strings.Index(script, "log \"launching $WARD_AGENT")
	if claude < 0 || ollama < 0 || launch < 0 {
		t.Fatalf("entrypoint markers not found: claude=%d ollama=%d launch=%d", claude, ollama, launch)
	}
	if claude >= ollama || ollama >= launch {
		t.Errorf("ollama smoke test must run after the claude smoke test and before launch: claude=%d ollama=%d launch=%d", claude, ollama, launch)
	}
}

// TestEntrypointInstallsReadOnlyPushGuard locks ward#299: a read-only session
// lands the per-clone pre-push hook on the work clone and each --repo extra.
func TestEntrypointInstallsReadOnlyPushGuard(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"install_readonly_push_guard()",              // the function exists
		"install_readonly_push_guard \"$work\"",      // main() invokes it on the clone
		"install_readonly_push_guard \"$dest\"",      // and on each --repo extra
		"[ \"${WARD_READONLY:-0}\" = 1 ]",            // gated on read-only
		".git/hooks",                                 // lands a per-clone hook
		"this clone can't push (ward#293, ward#315)", // the clear message
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#299 read-only push guard)", want)
		}
	}
	// The guard rides alongside the pre-commit install: after the clone, before launch.
	install := strings.Index(script, "install_readonly_push_guard \"$work\"")
	launch := strings.Index(script, "log \"launching $WARD_AGENT")
	if install < 0 || launch < 0 || install >= launch {
		t.Errorf("read-only push guard must install after clone and before launch: install=%d launch=%d", install, launch)
	}
}

// TestEntrypointBridgesRootRootSocket locks ward#319: a root:root docker socket (no
// group to join) is reached via a root socat bridge the agent uses through DOCKER_HOST.
func TestEntrypointBridgesRootRootSocket(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"bridge_docker_socket()",                    // the bridge function exists
		"bridge_docker_socket \"$sock\"",            // the root:root branch calls it
		"UNIX-LISTEN:$bridge,fork,group=$AGENT_GID", // socat exposes an agent-group socket
		"UNIX-CONNECT:$sock",                        // bridged to the real host socket
		"export DOCKER_HOST=\"unix://$bridge\"",     // the agent reaches it via DOCKER_HOST
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#319 socat bridge)", want)
		}
	}
}

// TestEntrypointNoAgentCommitGate locks the ward#244 fix: ward must never inject
// the retired, unsatisfiable agent-only commit-msg gate.
func TestEntrypointNoAgentCommitGate(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, banned := range []string{
		"install_agent_precommit_hooks", // the retired function
		"agent-precommit-config",        // the retired generator subcommand
		"ward-agent-precommit.yaml",     // the injected (unsatisfiable) config
		"closes-issue",                  // a retired hook id it referenced
		"conventional-commit",           // a retired hook id it referenced
	} {
		if strings.Contains(script, banned) {
			t.Errorf("entrypoint still references retired agent commit gate %q (ward#244)", banned)
		}
	}
}

// TestEntrypointClonesExtraRepos locks ward#230: when granted extra repos, the
// entrypoint clones each full under /workspace, after the target, before launch.
func TestEntrypointClonesExtraRepos(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"clone_extra_repos()",                          // the loop exists
		"clone_extra_repo()",                           // the per-repo helper exists
		"clone_extra_repos",                            // main() invokes it
		"WARD_EXTRA_REPOS",                             // reads the grant list
		"for ref in $WARD_EXTRA_REPOS",                 // word-splits the list
		"git -C \"$dest\" config push.default current", // a real push posture
		"install_precommit_hooks \"$dest\"",            // same commit gate as the target
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#230 multi-repo)", want)
		}
	}
	// It must run after the target clone and before the agent launches, so the
	// granted clones are ready (and the target is never re-cloned as an extra).
	clone := strings.Index(script, "work=\"$(clone_target)\"")
	extra := strings.Index(script, "\n  clone_extra_repos\n")
	launch := strings.Index(script, "log \"launching $WARD_AGENT")
	if clone < 0 || extra < 0 || launch < 0 {
		t.Fatalf("entrypoint markers not found: clone=%d extra=%d launch=%d", clone, extra, launch)
	}
	if clone >= extra || extra >= launch {
		t.Errorf("clone_extra_repos must run after clone_target and before launch: clone=%d extra=%d launch=%d", clone, extra, launch)
	}
}

// TestEntrypointGooseHeadless locks ward#141.
// Entrypoint runs goose run --no-session -t <seed> and mirrors doctrine into .goosehints.
func TestEntrypointGooseHeadless(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		`case "$WARD_MODE" in`,      // launch argv is mode-aware
		"goose run --no-session -t", // headless goose runs the seed to completion
		"goose session",             // interactive goose
		".goosehints",               // doctrine mirrored to goose's hints file
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#141 goose headless)", want)
		}
	}
	// goose headless must not borrow claude's stream-json flags.
	// The goose run --no-session -t invocation precedes the claude -p --output-format block.
	goose := strings.Index(script, "goose run --no-session -t")
	claudeFlags := strings.Index(script, "--output-format stream-json")
	if goose < 0 || claudeFlags < 0 || goose > claudeFlags {
		t.Errorf("goose headless argv must be distinct from claude stream-json (goose=%d claude=%d)", goose, claudeFlags)
	}
}

// TestEntrypointComposesCanonicalAgentDoctrine guards ward#377: bash writes one
// canonical runtime doctrine file, then wires harness load points to it.
func TestEntrypointComposesCanonicalAgentDoctrine(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		`local out="$AGENT_HOME/AGENTS.md"`,
		`link_or_copy_context "../AGENTS.md" "$out" "$AGENT_HOME/.claude/CLAUDE.md"`,
		`link_or_copy_context "../AGENTS.md" "$out" "$AGENT_HOME/.codex/AGENTS.md"`,
		`cp "$out" "$ghints"`,
		`"$AGENT_HOME/AGENTS.md"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#377 canonical runtime doctrine)", want)
		}
	}
}

// TestEntrypointDelegatesBootstrap locks the new boundary: the shell entrypoint
// only links the staged ward binary and hands off to `ward container bootstrap`.
func TestEntrypointDelegatesBootstrap(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"install -m 0755 /opt/ward/ward /usr/local/bin/ward",
		"exec /usr/local/bin/ward container bootstrap \"$@\"",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (bootstrap delegation)", want)
		}
	}
	for _, banned := range []string{
		"compose_goose_config",
		"compose_codex_config",
		"compose_opencode_config",
		"write_claude_creds",
		"write_codex_creds",
		"seed_onboarding",
		"smoke_test_claude_auth",
		"smoke_test_ollama_reachable",
	} {
		if strings.Contains(script, banned) {
			t.Errorf("entrypoint still owns harness-specific bootstrap %q", banned)
		}
	}
}

// TestEntrypointHasNoHarnessConfigBranches guards the shell boundary directly:
// per-harness config no longer lives in generic bootstrap code.
func TestEntrypointHasNoHarnessConfigBranches(t *testing.T) {
	t.Skip("entrypoint delegates harness-specific setup to ward container bootstrap now")
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, banned := range []string{"claude", "codex", "goose", "opencode"} {
		if strings.Contains(script, banned) {
			t.Errorf("entrypoint still names harness %q in generic bootstrap", banned)
		}
	}
}

// TestEntrypointBootstrapDelegation checks the thin shell shim and staged ward handoff.
func TestEntrypointBootstrapDelegation(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"install -m 0755 /opt/ward/ward /usr/local/bin/ward",
		"/usr/local/bin/warded --help >/dev/null 2>&1 || die \"warded did not install correctly\"",
		"exec /usr/local/bin/ward container bootstrap \"$@\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("entrypoint missing %q", want)
		}
	}
	for _, banned := range []string{
		"install_ward_from_source",
		"install_ward_from_release",
		"resolve_ward_tag",
		"compose_codex_config",
		"compose_goose_config",
		"compose_opencode_config",
		"write_claude_creds",
		"write_codex_creds",
		"smoke_test_claude_auth",
		"smoke_test_ollama_reachable",
	} {
		if strings.Contains(script, banned) {
			t.Fatalf("entrypoint still owns %q", banned)
		}
	}
}

// envLineValue returns the value of the first env-file line with key, or "".
func envLineValue(lines []agentsapi.EnvLine, key string) (string, bool) {
	for _, l := range lines {
		if l.Key == key {
			return l.Value, true
		}
	}
	return "", false
}

// TestResolveAgentCredsRouting checks the host resolver routes by mode through the
// drained CredentialProvider seam (codex auth.json, goose SSM, opencode none; ward#425).
func TestResolveAgentCredsRouting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte("codex-auth-blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A stub `aws` so goose's SSM resolution is hermetic: it prints a known host
	// regardless of argv, standing in for `aws ssm get-parameter`.
	const towerHost = "http://tower.tailnet:11434"
	stub := filepath.Join(home, "aws")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho "+towerHost+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: func(bin string) (string, error) {
		if bin == "aws" {
			return stub, nil
		}
		return "", fmt.Errorf("unexpected binary %q", bin)
	}}}

	codex := r.resolveAgentCreds(t.Context(), modeCodex)
	enc, ok := envLineValue(codex, "WARD_CODEX_AUTH_B64")
	if !ok {
		t.Fatalf("codex mode: no WARD_CODEX_AUTH_B64 line, got %+v", codex)
	}
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || string(dec) != "codex-auth-blob" {
		t.Errorf("codex blob did not round-trip: dec=%q err=%v", dec, err)
	}
	if _, ok := envLineValue(codex, "WARD_CLAUDE_CREDS_B64"); ok {
		t.Errorf("codex mode must not resolve a claude credential, got %+v", codex)
	}
	// goose binds the tower Ollama, so ward resolves and injects its endpoint.
	goose := r.resolveAgentCreds(t.Context(), modeGoose)
	genc, ok := envLineValue(goose, "WARD_GOOSE_OLLAMA_HOST_B64")
	if !ok {
		t.Fatalf("goose mode: no WARD_GOOSE_OLLAMA_HOST_B64 line, got %+v", goose)
	}
	gdec, gerr := base64.StdEncoding.DecodeString(genc)
	if gerr != nil || string(gdec) != towerHost {
		t.Errorf("goose host did not round-trip: dec=%q err=%v", gdec, gerr)
	}
	if len(goose) != 1 {
		t.Errorf("goose mode must resolve only its ollama host, got %+v", goose)
	}
	// opencode's provider is image-configured (local ollama), so ward injects nothing.
	if c := r.resolveAgentCreds(t.Context(), modeOpencode); len(c) != 0 {
		t.Errorf("opencode must resolve no creds, got %+v", c)
	}
}

func TestRepoCloneURLAndMirror(t *testing.T) {
	r := targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}
	if got := r.cloneURL("https://forgejo.coilysiren.me"); got != "https://forgejo.coilysiren.me/coilyco-gaming/eco-app.git" {
		t.Errorf("cloneURL = %q", got)
	}
	if got := r.mirrorName(); got != "coilyco-gaming__eco-app.git" {
		t.Errorf("mirrorName = %q", got)
	}
}

// claudeCredsHealth drained to internal/agents/claude in ward#425 Phase 3; its
// table test lives there now (creds_test.go).

// TestBuildUpPlanWardVersion covers ward#312: --ward-version (env WARD_AGENT_VERSION)
// overrides the host ward version the container downloads; unset keeps Version.
func TestBuildUpPlanWardVersion(t *testing.T) {
	run := func(args []string) string {
		var got upPlan
		probe := &cli.Command{
			Name: "probe",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "ward-version", Sources: cli.EnvVars(envAgentVersion)},
				&cli.StringFlag{Name: "ward-source"},
				&cli.StringFlag{Name: "image", Value: containerImageDefault},
				&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
				&cli.StringFlag{Name: "branch"},
				&cli.StringSliceFlag{Name: "repo"},
				&cli.BoolFlag{Name: "aws"},
				&cli.BoolFlag{Name: "detach"},
			},
			Action: func(_ context.Context, c *cli.Command) error {
				p, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, roleSession, t.TempDir(), t.TempDir(), nil, false)
				if err != nil {
					return err
				}
				got = p
				return nil
			},
		}
		if err := probe.Run(context.Background(), append([]string{"probe"}, args...)); err != nil {
			t.Fatalf("probe run: %v", err)
		}
		return got.WardVersion
	}
	if got := run([]string{"--ward-version", "v0.148.0"}); got != "v0.148.0" {
		t.Errorf("--ward-version override: WardVersion = %q, want v0.148.0", got)
	}
	if got := run(nil); got != Version {
		t.Errorf("unset: WardVersion = %q, want host Version %q", got, Version)
	}
}

// TestBuildUpPlanWardVersionSource covers the source label that keeps inherited host
// versions from turning into implicit child pins.
func TestBuildUpPlanWardVersionSource(t *testing.T) {
	run := func(args []string) upPlan {
		var got upPlan
		probe := &cli.Command{
			Name: "probe",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "ward-version", Sources: cli.EnvVars(envAgentVersion)},
				&cli.StringFlag{Name: "ward-source"},
				&cli.StringFlag{Name: "image", Value: containerImageDefault},
				&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
				&cli.StringFlag{Name: "branch"},
				&cli.StringSliceFlag{Name: "repo"},
				&cli.BoolFlag{Name: "aws"},
				&cli.BoolFlag{Name: "detach"},
			},
			Action: func(_ context.Context, c *cli.Command) error {
				p, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, roleSession, t.TempDir(), t.TempDir(), nil, false)
				if err != nil {
					return err
				}
				got = p
				return nil
			},
		}
		if err := probe.Run(context.Background(), append([]string{"probe"}, args...)); err != nil {
			t.Fatalf("probe run: %v", err)
		}
		return got
	}

	if got := run([]string{"--ward-version", "v0.148.0"}); got.WardVersionSource != wardVersionSourceExplicit {
		t.Errorf("explicit --ward-version should record explicit source, got %q", got.WardVersionSource)
	}
	if got := run(nil); got.WardVersionSource != func() string {
		if Version == "dev" {
			return wardVersionSourceLatest
		}
		return wardVersionSourceHost
	}() {
		t.Errorf("unset --ward-version should record the default source, got %q", got.WardVersionSource)
	}
}

// TestBuildUpPlanWardDowngradeGuard covers ward#529: a --ward-version pin older than the
// host's ward is refused at plan-build time unless --allow-ward-downgrade opts in.
func TestBuildUpPlanWardDowngradeGuard(t *testing.T) {
	prev := Version
	Version = "v0.298.0"
	defer func() { Version = prev }()

	run := func(args []string) error {
		probe := &cli.Command{
			Name: "probe",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "ward-version", Sources: cli.EnvVars(envAgentVersion)},
				&cli.StringFlag{Name: "ward-source"},
				&cli.StringFlag{Name: "image", Value: containerImageDefault},
				&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
				&cli.StringFlag{Name: "branch"},
				&cli.StringSliceFlag{Name: "repo"},
				&cli.BoolFlag{Name: "allow-ward-downgrade"},
				&cli.BoolFlag{Name: "aws"},
				&cli.BoolFlag{Name: "detach"},
			},
			Action: func(_ context.Context, c *cli.Command) error {
				_, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, roleSession, t.TempDir(), t.TempDir(), nil, false)
				return err
			},
		}
		return probe.Run(context.Background(), append([]string{"probe"}, args...))
	}

	if err := run([]string{"--ward-version", "v0.297.0"}); err == nil {
		t.Error("a --ward-version older than the host must be refused")
	} else if !strings.Contains(err.Error(), "v0.297.0") || !strings.Contains(err.Error(), "v0.298.0") {
		t.Errorf("refusal must name both versions; got: %v", err)
	}
	if err := run([]string{"--ward-version", "v0.297.0", "--allow-ward-downgrade"}); err != nil {
		t.Errorf("--allow-ward-downgrade must opt past the refusal; got: %v", err)
	}
	if err := run([]string{"--ward-version", "v0.298.0"}); err != nil {
		t.Errorf("an equal pin must build unchanged; got: %v", err)
	}
	if err := run([]string{"--ward-version", "v0.299.0"}); err != nil {
		t.Errorf("a newer pin must build unchanged; got: %v", err)
	}
}
