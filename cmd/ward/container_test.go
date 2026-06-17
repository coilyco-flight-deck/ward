package main

import (
	"slices"
	"strings"
	"testing"
)

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
	if modeClaude.agentBinary() != "claude" || modeCodex.agentBinary() != "codex" || modeQwen.agentBinary() != "opencode" {
		t.Error("mode -> agent binary mapping wrong")
	}
}

func TestParseMode(t *testing.T) {
	for _, ok := range []string{"claude", "codex", "qwen"} {
		if _, err := parseMode(ok); err != nil {
			t.Errorf("parseMode(%q) errored: %v", ok, err)
		}
	}
	if _, err := parseMode("gpt"); err == nil {
		t.Error("parseMode should reject unknown mode")
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

func TestDockerExecDownListArgv(t *testing.T) {
	exec := dockerExecArgv("ward-eco-app-deadbeef", true, []string{"ward", "exec", "test"})
	if strings.Join(exec, " ") != "exec -it ward-eco-app-deadbeef ward exec test" {
		t.Errorf("exec argv wrong: %v", exec)
	}
	execNoTTY := dockerExecArgv("ward-eco-app-deadbeef", false, []string{"ward", "exec", "test"})
	if strings.Join(execNoTTY, " ") != "exec -i ward-eco-app-deadbeef ward exec test" {
		t.Errorf("exec no-TTY argv wrong: %v", execNoTTY)
	}
	down := dockerDownArgv("ward-eco-app-deadbeef")
	if strings.Join(down, " ") != "rm -f ward-eco-app-deadbeef" {
		t.Errorf("down argv wrong: %v", down)
	}
	list := dockerListArgv(true)
	lj := strings.Join(list, " ")
	if !strings.Contains(lj, "ps") || !strings.Contains(lj, "-a") || !strings.Contains(lj, "label="+containerLabel) {
		t.Errorf("list argv wrong: %v", list)
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

func TestRepoCloneURLAndMirror(t *testing.T) {
	r := targetRepo{Owner: "coilyco-gaming", Name: "eco-app"}
	if got := r.cloneURL("https://forgejo.coilysiren.me"); got != "https://forgejo.coilysiren.me/coilyco-gaming/eco-app.git" {
		t.Errorf("cloneURL = %q", got)
	}
	if got := r.mirrorName(); got != "coilyco-gaming__eco-app.git" {
		t.Errorf("mirrorName = %q", got)
	}
}
