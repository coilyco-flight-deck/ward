package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/urfave/cli/v3"
)

// TestSweepStaleContainerAssets reclaims dirs past the TTL (left by detached
// runs) while sparing fresh ones and unrelated dirs.
func TestSweepStaleContainerAssets(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if os.TempDir() != tmp {
		t.Skipf("TMPDIR override not honored (os.TempDir()=%s)", os.TempDir())
	}
	stale := filepath.Join(tmp, containerAssetsPrefix+"stale")
	fresh := filepath.Join(tmp, containerAssetsPrefix+"fresh")
	other := filepath.Join(tmp, "unrelated-dir")
	for _, d := range []string{stale, fresh, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * containerAssetsTTL)
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}
	sweepStaleContainerAssets()
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

func TestContainerNameUniqueAndSafe(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}
	a := containerName(repo, "a1b2c3d4")
	b := containerName(repo, "e5f6a7b8")
	if a == b {
		t.Fatalf("two runs against the same repo must not collide: %q == %q", a, b)
	}
	if !strings.HasPrefix(a, "ward-eco-app-") {
		t.Errorf("name %q missing ward-<repo>- prefix", a)
	}
	// docker forbids these; sanitization must strip them.
	weird := targetRepo{Owner: "x", Name: "we/ird name!"}
	got := containerName(weird, "deadbeef")
	for _, bad := range []string{"/", " ", "!"} {
		if strings.Contains(got, bad) {
			t.Errorf("sanitized name %q still contains %q", got, bad)
		}
	}
}

func TestAgentContainerNameIsMeaningful(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}
	got := agentContainerName(repo, modeClaude, 140, "a1b2c3d4")
	want := "ward-ward-issue-140-claude-a1b2c3d4"
	if got != want {
		t.Errorf("agentContainerName = %q, want %q", got, want)
	}
	// The repo, issue, and harness must all be legible in the name so a host
	// running several agents can tell them apart at a glance.
	for _, frag := range []string{"ward", "issue-140", "claude"} {
		if !strings.Contains(got, frag) {
			t.Errorf("name %q missing %q", got, frag)
		}
	}
	// The random suffix keeps concurrent runs on the same issue from colliding.
	other := agentContainerName(repo, modeClaude, 140, "e5f6a7b8")
	if got == other {
		t.Fatalf("two runs on the same issue must not collide: %q == %q", got, other)
	}
	// The mode distinguishes a claude run from a codex run on the same issue.
	if agentContainerName(repo, modeCodex, 140, "a1b2c3d4") == got {
		t.Error("different harnesses on the same issue must produce different names")
	}
	// docker-forbidden characters in the repo name must be sanitized away.
	weird := targetRepo{Owner: "x", Name: "we/ird name!"}
	dirty := agentContainerName(weird, modeQwen, 7, "deadbeef")
	for _, bad := range []string{"/", " ", "!"} {
		if strings.Contains(dirty, bad) {
			t.Errorf("sanitized name %q still contains %q", dirty, bad)
		}
	}
}

func TestLeastAccessMountsDefaultIsCwdOnly(t *testing.T) {
	mounts := leastAccessMounts("/home/kai/projects/coilyco-bridge/agentic-os-kai", mountOpts{AssetsDir: "/tmp/ward-assets"})
	// The target repo must never be a host bind: only cwd + assets binds, plus
	// the gitcache named volume.
	var hostBinds []string
	for _, m := range mounts {
		if !m.Volume {
			hostBinds = append(hostBinds, m.Source)
		}
		if !m.Volume && !m.ReadOnly && m.Target != containerGitcacheMnt {
			t.Errorf("host bind %q is writable; least-access binds are read-only", m.Source)
		}
	}
	wantBinds := []string{"/home/kai/projects/coilyco-bridge/agentic-os-kai", "/tmp/ward-assets"}
	if !slices.Equal(hostBinds, wantBinds) {
		t.Errorf("default host binds = %v, want exactly %v (cwd + assets, no target repo)", hostBinds, wantBinds)
	}
}

func TestLeastAccessMountsOptIns(t *testing.T) {
	mounts := leastAccessMounts("/cwd", mountOpts{AssetsDir: "/a", AWSHome: "/home/kai/.aws", WardSource: "/src/ward"})
	targets := map[string]bool{}
	for _, m := range mounts {
		targets[m.Target] = true
	}
	for _, want := range []string{containerContextMount, containerGitcacheMnt, containerWardAssets, containerAWSMount, containerWardSrcMount} {
		if !targets[want] {
			t.Errorf("opt-in mount set missing %q", want)
		}
	}
}

func TestModeContextLevelLadder(t *testing.T) {
	if modeClaude.contextLevel() <= modeCodex.contextLevel() {
		t.Error("claude must carry more context than codex")
	}
	if modeCodex.contextLevel() <= modeQwen.contextLevel() {
		t.Error("codex must carry more context than qwen")
	}
	if modeQwen.contextLevel() != 0 {
		t.Errorf("qwen is the minimal-context floor, got %d", modeQwen.contextLevel())
	}
	// goose is a full carry-to-merge harness: same context tier as claude.
	if modeGoose.contextLevel() != modeClaude.contextLevel() {
		t.Errorf("goose must carry the same context level as claude, got %d", modeGoose.contextLevel())
	}
	if modeClaude.agentBinary() != "claude" || modeCodex.agentBinary() != "codex" ||
		modeQwen.agentBinary() != "opencode" || modeGoose.agentBinary() != "goose" {
		t.Error("mode -> agent binary mapping wrong")
	}
}

// ward#148: claude+goose (the full carry-to-merge harnesses) keep parity on the
// headless pre-flight, so both expose a host one-shot argv; codex/qwen don't yet.
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
	// codex/qwen: no reliable host one-shot yet, so the pre-flight bows out and
	// the dispatch proceeds unguarded rather than fabricating a verdict.
	for _, m := range []containerMode{modeCodex, modeQwen} {
		if argv, ok := m.hostPreflightArgv("carry it?"); ok {
			t.Errorf("%s: did not expect a host pre-flight argv yet, got %v", m, argv)
		}
	}
}

func TestParseMode(t *testing.T) {
	for _, ok := range []string{"claude", "codex", "qwen", "goose"} {
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

// TestAgentGrantFlagName pins the agent extra-repo grant as "--repo" with the
// "--with-repo" alias, and proves the "with-repo" lookup resolves it (ward#280).
func TestAgentGrantFlagName(t *testing.T) {
	for _, cmd := range []*cli.Command{
		agentSurfaceCommand("work", false),
		agentSurfaceCommand("headless", true),
		agentTaskCommand(),
	} {
		var grant cli.Flag
		for _, f := range cmd.Flags {
			if slices.Contains(f.Names(), "with-repo") {
				grant = f
				break
			}
		}
		if grant == nil {
			t.Fatalf("%s: no grant flag reachable by the shared \"with-repo\" key", cmd.Name)
		}
		names := grant.Names()
		if !slices.Contains(names, "repo") {
			t.Errorf("%s: grant flag missing the shortened \"repo\" name; got %v", cmd.Name, names)
		}
		if !slices.Contains(names, "with-repo") {
			t.Errorf("%s: grant flag dropped the \"with-repo\" alias; got %v", cmd.Name, names)
		}
		if _, ok := grant.(*cli.StringSliceFlag); !ok {
			t.Errorf("%s: grant flag is %T, want a repeatable *cli.StringSliceFlag", cmd.Name, grant)
		}
	}

	// And the shortened name reaches the reader buildUpPlan uses: `--repo` must
	// surface through the shared `StringSlice("with-repo")` lookup, repeatably.
	var got []string
	probe := &cli.Command{
		Name:  "probe",
		Flags: []cli.Flag{&cli.StringSliceFlag{Name: "repo", Aliases: []string{"with-repo"}}},
		Action: func(_ context.Context, c *cli.Command) error {
			got = c.StringSlice("with-repo")
			return nil
		},
	}
	if err := probe.Run(context.Background(), []string{"probe", "--repo", "o/a", "--repo", "o/b"}); err != nil {
		t.Fatalf("probe run: %v", err)
	}
	if want := []string{"o/a", "o/b"}; !slices.Equal(got, want) {
		t.Errorf("--repo via the with-repo lookup = %v, want %v", got, want)
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
	if _, ok := p.wardEnv()["WARD_TARGET_ISSUE"]; ok {
		t.Error("WARD_TARGET_ISSUE must be absent when no issue is carried (Issue 0)")
	}
	p.Issue = 264
	if got := p.wardEnv()["WARD_TARGET_ISSUE"]; got != "264" {
		t.Errorf("WARD_TARGET_ISSUE = %q, want 264", got)
	}
}

func sampleUpPlan() upPlan {
	repo := targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}
	return upPlan{
		Image:       "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest",
		Name:        "ward-eco-app-deadbeef",
		Repo:        repo,
		Mode:        modeClaude,
		Branch:      "feat/foo",
		ForgejoBase: forgejoBaseURL,
		HostCwd:     "/cwd",
		Mounts:      leastAccessMounts("/cwd", mountOpts{AssetsDir: "/a"}),
		Interactive: true,
		TTY:         true,
		WardVersion: "v0.16.0",
	}
}

func TestDockerCreateArgvShape(t *testing.T) {
	argv := dockerCreateArgv(sampleUpPlan(), "/tmp/ward-env-xyz")
	joined := strings.Join(argv, " ")

	if argv[0] != "run" {
		t.Errorf("argv[0] = %q, want run", argv[0])
	}
	for _, want := range []string{
		"--name ward-eco-app-deadbeef",
		"--label " + containerLabel,
		"--label ward.repo=coilyco-gaming/eco-app",
		"--entrypoint " + containerWardAssets + "/" + containerEntrypointRel,
		"-it",
		"--env-file /tmp/ward-env-xyz",
		"-e WARD_TARGET_REPO=coilyco-gaming/eco-app",
		"-e WARD_MODE=claude",
		"-e WARD_CONTEXT_LEVEL=2",
		"-e WARD_BRANCH=feat/foo",
		"-e WARD_VERSION=v0.16.0",
		"-e WARD_SUBSTRATE_TTL=" + containerSubstrateTTL,
		"-e WARD_SUBSTRATE_SEED=" + containerSubstrateSeed,
		"-e WARD_SUBSTRATE_MANIFEST=" + containerSubstrateManifest,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("docker argv missing %q\n got: %s", want, joined)
		}
	}
	// The image is the final arg.
	if argv[len(argv)-1] != "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest" {
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

// TestEntrypointNoAgentCommitGate locks the ward#244 fix: ward must never inject
// the retired, unsatisfiable agent-only commit-msg gate.
func TestEntrypointNoAgentCommitGate(t *testing.T) {
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

// TestEntrypointGooseHeadless locks ward#141: entrypoint runs `goose run -t <seed>`
// (not claude `-p`) and mirrors doctrine into .goosehints since goose ignores ~/.claude.
func TestEntrypointGooseHeadless(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		`case "$WARD_MODE" in`, // launch argv is mode-aware
		"goose run -t",         // headless goose runs the seed to completion
		"goose session",        // interactive goose
		".goosehints",          // doctrine mirrored to goose's hints file
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#141 goose headless)", want)
		}
	}
	// goose headless must not borrow claude's stream-json flags: the goose `run -t`
	// invocation precedes the claude `-p --output-format` block in the mode switch.
	goose := strings.Index(script, "goose run -t")
	claudeFlags := strings.Index(script, "--output-format stream-json")
	if goose < 0 || claudeFlags < 0 || goose > claudeFlags {
		t.Errorf("goose headless argv must be distinct from claude stream-json (goose=%d claude=%d)", goose, claudeFlags)
	}
}

// TestEntrypointGooseConfig guards goose's provider wiring (ward#186): the entrypoint
// seeds ~/.config/goose/config.yaml with provider + model from the resolved host.
func TestEntrypointGooseConfig(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"compose_goose_config",       // the seed step exists...
		"config.yaml",                // ...and writes goose's config file
		"GOOSE_PROVIDER",             // provider is bound
		"GOOSE_MODEL",                // model is bound
		"WARD_GOOSE_OLLAMA_HOST_B64", // the host-resolved tower endpoint rides the env-file
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#186 goose config)", want)
		}
	}
	// The step must be wired into main() alongside the other credential steps.
	if !strings.Contains(script, "\n  compose_goose_config\n") {
		t.Error("compose_goose_config must be called in main()")
	}
}

// TestEntrypointCodexExec guards the codex launch dialect (ward#178): codex runs
// via `codex exec` with its auth + config written before launch, not claude flags.
func TestEntrypointCodexExec(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"codex exec",           // headless codex speaks the exec dialect
		"write_codex_creds",    // host-injected auth.json is decoded in
		"compose_codex_config", // approvals-off / sandbox-open posture is written
		"approval_policy",      // ...and that config sets the autonomous posture
		"sandbox_mode",
		"WARD_CODEX_AUTH_B64", // the env-file credential channel
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#178 codex)", want)
		}
	}
	// codex headless must not borrow claude's stream-json flags: its `exec`
	// invocation precedes the claude `-p --output-format` default branch.
	codex := strings.Index(script, "codex exec")
	claudeFlags := strings.Index(script, "--output-format stream-json")
	if codex < 0 || claudeFlags < 0 || codex > claudeFlags {
		t.Errorf("codex headless argv must be distinct from claude stream-json (codex=%d claude=%d)", codex, claudeFlags)
	}
}

// TestEntrypointQwenOpencode guards the qwen launch dialect (ward#187): opencode
// self-installed, qwen-backed config written, `opencode run` not claude's flags.
func TestEntrypointQwenOpencode(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/" + containerEntrypointRel)
	if err != nil {
		t.Fatalf("read entrypoint: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"opencode run",            // headless qwen drives opencode's run dialect
		"install_opencode",        // ward self-installs the standalone binary
		"compose_opencode_config", // ...and writes the ollama-backed config
		"ollama",                  // the provider the config registers
		"WARD_QWEN_MODEL",         // the overridable model tag
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint missing %q (ward#187 qwen)", want)
		}
	}
	// qwen headless must not borrow claude's stream-json flags: its `opencode run`
	// invocation precedes the claude `-p --output-format` default branch.
	qwen := strings.Index(script, "opencode run")
	claudeFlags := strings.Index(script, "--output-format stream-json")
	if qwen < 0 || claudeFlags < 0 || qwen > claudeFlags {
		t.Errorf("qwen headless argv must be distinct from claude stream-json (qwen=%d claude=%d)", qwen, claudeFlags)
	}
}

// TestCredEnvLines pins the per-mode credential env-file shaping (ward#178): each
// present blob rides base64'd on its own WARD_*_B64 line, absent blobs are omitted.
func TestCredEnvLines(t *testing.T) {
	if got := credEnvLines(agentCreds{}); len(got) != 0 {
		t.Errorf("no creds should yield no lines, got %v", got)
	}
	claudeOnly := credEnvLines(agentCreds{Claude: "claude-blob"})
	if len(claudeOnly) != 1 || !strings.HasPrefix(claudeOnly[0], "WARD_CLAUDE_CREDS_B64=") {
		t.Fatalf("claude-only lines = %v", claudeOnly)
	}
	codexOnly := credEnvLines(agentCreds{Codex: "codex-blob"})
	if len(codexOnly) != 1 || !strings.HasPrefix(codexOnly[0], "WARD_CODEX_AUTH_B64=") {
		t.Fatalf("codex-only lines = %v", codexOnly)
	}
	// The codex blob must round-trip through base64 unchanged.
	enc := strings.TrimPrefix(codexOnly[0], "WARD_CODEX_AUTH_B64=")
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || string(dec) != "codex-blob" {
		t.Errorf("codex blob did not round-trip: dec=%q err=%v", dec, err)
	}
	gooseOnly := credEnvLines(agentCreds{GooseOllamaHost: "http://tower:11434"})
	if len(gooseOnly) != 1 || !strings.HasPrefix(gooseOnly[0], "WARD_GOOSE_OLLAMA_HOST_B64=") {
		t.Fatalf("goose-only lines = %v", gooseOnly)
	}
	gdec, gerr := base64.StdEncoding.DecodeString(strings.TrimPrefix(gooseOnly[0], "WARD_GOOSE_OLLAMA_HOST_B64="))
	if gerr != nil || string(gdec) != "http://tower:11434" {
		t.Errorf("goose host did not round-trip: dec=%q err=%v", gdec, gerr)
	}
	if got := credEnvLines(agentCreds{Claude: "a", Codex: "b"}); len(got) != 2 {
		t.Errorf("both creds should yield two lines, got %v", got)
	}
}

// TestResolveAgentCredsRouting checks the resolver routes by mode: codex reads
// auth.json, goose resolves the tower Ollama from SSM (ward#186), qwen none.
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
	r := &Runner{Runner: &shell.Runner{Resolve: func(bin string) (string, error) {
		if bin == "aws" {
			return stub, nil
		}
		return "", fmt.Errorf("unexpected binary %q", bin)
	}}}

	got := r.resolveAgentCreds(t.Context(), modeCodex)
	if got.Codex != "codex-auth-blob" {
		t.Errorf("codex mode: Codex = %q, want the auth.json contents", got.Codex)
	}
	if got.Claude != "" {
		t.Errorf("codex mode must not resolve a claude credential, got %q", got.Claude)
	}
	// goose binds the tower Ollama, so ward resolves and injects its endpoint.
	goose := r.resolveAgentCreds(t.Context(), modeGoose)
	if goose.GooseOllamaHost != towerHost {
		t.Errorf("goose mode: GooseOllamaHost = %q, want the resolved tower host %q", goose.GooseOllamaHost, towerHost)
	}
	if goose.Claude != "" || goose.Codex != "" {
		t.Errorf("goose mode must resolve only its ollama host, got %+v", goose)
	}
	// qwen's opencode provider is image-configured, so ward injects nothing.
	if c := r.resolveAgentCreds(t.Context(), modeQwen); c != (agentCreds{}) {
		t.Errorf("qwen must resolve no creds, got %+v", c)
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

func TestClaudeCredsHealth(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour).UnixMilli()
	past := now.Add(-time.Hour).UnixMilli()
	tests := []struct {
		name    string
		blob    string
		wantOK  bool
		wantSub string // substring expected in reason when !wantOK
	}{
		{"empty", "", false, "empty"},
		{"whitespace only", "   \n", false, "empty"},
		{"healthy nested claudeAiOauth", fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, future), true, ""},
		{"healthy top-level fallback", fmt.Sprintf(`{"accessToken":"tok","expiresAt":%d}`, future), true, ""},
		{"healthy no expiry", `{"claudeAiOauth":{"accessToken":"tok"}}`, true, ""},
		{"expired token", fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, past), false, "expired"},
		{"no access token", `{"claudeAiOauth":{"expiresAt":12345}}`, false, "no accessToken"},
		{"unrecognised but valid json", `{"something":"else"}`, false, "no accessToken"},
		{"not json at all", "not-json-blob", true, ""}, // defer to in-container smoke test
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := claudeCredsHealth(tc.blob, now)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (reason=%q)", ok, tc.wantOK, reason)
			}
			if !tc.wantOK && tc.wantSub != "" && !strings.Contains(reason, tc.wantSub) {
				t.Errorf("reason = %q, want substring %q", reason, tc.wantSub)
			}
		})
	}
}

// TestBuildUpPlanWardVersion covers ward#312: --ward-version (env WARD_AGENT_VERSION)
// overrides the host ward version the container downloads; unset keeps Version.
func TestBuildUpPlanWardVersion(t *testing.T) {
	run := func(args []string) string {
		var got string
		probe := &cli.Command{
			Name: "probe",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "ward-version", Sources: cli.EnvVars(envAgentVersion)},
				&cli.StringFlag{Name: "ward-source"},
				&cli.StringFlag{Name: "image", Value: containerImageDefault},
				&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
				&cli.StringFlag{Name: "branch"},
				&cli.StringSliceFlag{Name: "repo", Aliases: []string{"with-repo"}},
				&cli.BoolFlag{Name: "aws"},
				&cli.BoolFlag{Name: "detach"},
			},
			Action: func(_ context.Context, c *cli.Command) error {
				p, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, t.TempDir(), t.TempDir(), nil)
				if err != nil {
					return err
				}
				got = p.WardVersion
				return nil
			},
		}
		if err := probe.Run(context.Background(), append([]string{"probe"}, args...)); err != nil {
			t.Fatalf("probe run: %v", err)
		}
		return got
	}
	if got := run([]string{"--ward-version", "v0.148.0"}); got != "v0.148.0" {
		t.Errorf("--ward-version override: WardVersion = %q, want v0.148.0", got)
	}
	if got := run(nil); got != Version {
		t.Errorf("unset: WardVersion = %q, want host Version %q", got, Version)
	}
}
