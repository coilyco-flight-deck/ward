package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"github.com/urfave/cli/v3"
)

// ward#353 collapsed `architect` into the director's surface phase: the roster is now
// engineer/director/qa, and `warded architect` errors as an unknown command.
func TestArchitectRoleCollapsedIntoDirectorSurface(t *testing.T) {
	surfaces := map[string]bool{}
	for _, c := range agentCommand().Commands {
		surfaces[c.Name] = true
	}
	// The roster is exactly the product roles (plus meta verbs).
	for _, want := range []string{"engineer", "director", "qa"} {
		if !surfaces[want] {
			t.Errorf("ward agent missing %q role; got %v", want, surfaces)
		}
	}
	// architect (and the older explore/sandbox, and the internal surface verb) is never
	// a registered role - all hard renames, no aliases (ward#353, ward#347).
	for _, gone := range []string{"architect", "explore", "sandbox", "surface"} {
		if surfaces[gone] {
			t.Errorf("retired/internal verb %q must not be a registered role; got %v", gone, surfaces)
		}
	}
}

// A read-only plan exports WARD_READONLY=1 so the entrypoint revokes the push
// credential and the reaper skips salvage; a writable sandbox plan never sets it.
func TestReadOnlyPlanExportsFlag(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_READONLY"]; ok {
		t.Error("a default (writable) plan must not set WARD_READONLY")
	}
	p.ReadOnly = true
	if p.wardEnv()["WARD_READONLY"] != "1" {
		t.Error("a read-only plan must export WARD_READONLY=1")
	}
}

// The docker socket binds read-write at the same path both sides and is NOT in the
// least-access default - only explore opts in (ward#315).
func TestDockerSockMount(t *testing.T) {
	m := dockerSockMount()
	if m.Source != "/var/run/docker.sock" || m.Target != "/var/run/docker.sock" {
		t.Errorf("docker sock mount = %s -> %s, want /var/run/docker.sock both sides", m.Source, m.Target)
	}
	if m.ReadOnly {
		t.Error("docker sock mount must be read-write (the docker client writes to it)")
	}
	if m.Volume {
		t.Error("docker sock mount must be a host bind, not a named volume")
	}
	if arg := m.arg(); arg != "/var/run/docker.sock:/var/run/docker.sock" {
		t.Errorf("docker sock mount arg = %q, want unsuffixed host bind", arg)
	}
	for _, def := range leastAccessMounts("/cwd", mountOpts{AssetsDir: "/a"}) {
		if def.Source == "/var/run/docker.sock" {
			t.Error("the least-access default must not bind the docker socket; only explore opts in")
		}
	}
}

// The host agent-log drain binds read-only at containerAgentLogsMount when
// mountOpts.AgentLogsDir is set, and is NEVER in the least-access default (ward#525).
func TestAgentLogsMountOptIn(t *testing.T) {
	// Default (no AgentLogsDir): the drain is never bound.
	for _, def := range leastAccessMounts("/cwd", mountOpts{AssetsDir: "/a"}) {
		if def.Target == containerAgentLogsMount || strings.Contains(def.Source, "agent-logs") {
			t.Errorf("least-access default must not bind the agent-log drain; got %s -> %s", def.Source, def.Target)
		}
	}
	// Opt-in: the drain binds read-only at the fixed in-container path.
	var found bool
	for _, m := range leastAccessMounts("/cwd", mountOpts{AssetsDir: "/a", AgentLogsDir: "/host/agent-logs"}) {
		if m.Target != containerAgentLogsMount {
			continue
		}
		found = true
		if m.Source != "/host/agent-logs" {
			t.Errorf("agent-logs mount source = %q, want the host drain dir", m.Source)
		}
		if !m.ReadOnly {
			t.Error("agent-logs mount must be read-only (:ro)")
		}
		if m.Volume {
			t.Error("agent-logs mount must be a host bind, not a named volume")
		}
		if arg := m.arg(); !strings.HasSuffix(arg, ":ro") {
			t.Errorf("agent-logs mount arg = %q, want a :ro-suffixed bind", arg)
		}
	}
	if !found {
		t.Errorf("mountOpts.AgentLogsDir set but no mount at %s", containerAgentLogsMount)
	}
}

// buildUpPlan binds the director-surface extras only when its opt-in is passed.
// The extras are the secret-safe agent-log drain and the Docker socket (ward#525/#1001).
func TestBuildUpPlanDirectorSurfaceThreading(t *testing.T) {
	run := func(mountSurfaceExtras bool) upPlan {
		var got upPlan
		probe := &cli.Command{
			Name:  "probe",
			Flags: agentPlanProbeFlags(),
			Action: func(_ context.Context, c *cli.Command) error {
				p, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, roleDirector, t.TempDir(), t.TempDir(), nil, mountSurfaceExtras)
				if err != nil {
					return err
				}
				got = p
				return nil
			},
		}
		if rerr := probe.Run(context.Background(), []string{"probe"}); rerr != nil {
			t.Fatalf("probe run: %v", rerr)
		}
		return got
	}
	has := func(p upPlan) bool {
		for _, m := range p.Mounts {
			if m.Target == containerAgentLogsMount {
				return true
			}
		}
		return false
	}
	hasSock := func(p upPlan) bool {
		for _, m := range p.Mounts {
			if m.Target == containerDockerSock {
				return true
			}
		}
		return false
	}
	if has(run(false)) || hasSock(run(false)) {
		t.Error("buildUpPlan(mountSurfaceExtras=false) must not bind director-surface extras")
	}
	if !has(run(true)) {
		t.Errorf("buildUpPlan(mountSurfaceExtras=true) must bind the agent-log drain at %s", containerAgentLogsMount)
	}
	if !hasSock(run(true)) {
		t.Errorf("buildUpPlan(mountSurfaceExtras=true) must bind the Docker socket at %s", containerDockerSock)
	}
}

func TestDirectorExplicitRepoDoesNotRequireResolvableCWD(t *testing.T) {
	stubContainerBootstrapStage(t)
	oldStartupCWD := startupCWD
	startupCWD = ""
	t.Cleanup(func() { startupCWD = oldStartupCWD })

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	deleted := t.TempDir()
	if err := os.Chdir(deleted); err != nil {
		t.Fatalf("chdir deleted cwd fixture: %v", err)
	}
	if err := os.RemoveAll(deleted); err != nil {
		t.Fatalf("remove deleted cwd fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	cmd := parseCommandForTest(t, agentScratchFlags(), []string{"surface", "--repo", "coilyco-flight-deck/ward", "--no-pull"})
	r := &Runner{}
	plan, cleanup, err := r.prepareScratchPlan(t.Context(), cmd, modeCodex, true, "ward agent director")
	if err != nil {
		t.Fatalf("prepareScratchPlan with explicit --repo and unresolvable cwd: %v", err)
	}
	defer cleanup()
	if got := plan.Repo.slug(); got != "coilyco-flight-deck/ward" {
		t.Fatalf("plan repo = %q, want coilyco-flight-deck/ward", got)
	}
	for _, mount := range plan.Mounts {
		if mount.Target == containerContextMount {
			t.Fatalf("unresolvable launch cwd must not be mounted as context: %+v", mount)
		}
	}
}

// resolveForgejoToken prefers an already-present FORGEJO_TOKEN so a `warded #N`
// dispatched from inside an explore box resolves (ward#315).
func TestResolveForgejoTokenPrefersEnv(t *testing.T) {
	r, _, _ := bufRunner("")

	// No WARD_BROKER_SOCK here, so the broker seed is inert and the env path runs.
	t.Setenv("FORGEJO_TOKEN", "env-token")
	got, err := r.resolveForgejoToken(t.Context(), broker.Target{}, forgeForgejo)
	if err != nil {
		t.Fatalf("resolveForgejoToken (env set): %v", err)
	}
	if got != "env-token" {
		t.Errorf("with FORGEJO_TOKEN set, resolveForgejoToken = %q, want the env value", got)
	}

	t.Setenv("FORGEJO_TOKEN", "")
	if _, err = r.resolveForgejoToken(t.Context(), broker.Target{}, forgeForgejo); err == nil {
		t.Fatal("resolveForgejoToken with no env or broker seed: want error, got nil")
	}
}

// readBootstrapEnv maps WARD_READONLY onto bootstrapEnv.ReadOnly (the in-container
// side that revokes the credential + composes the restriction).
func TestReadBootstrapEnvReadOnly(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")

	t.Setenv("WARD_READONLY", "")
	if e, _ := readBootstrapEnv(); e.ReadOnly {
		t.Error("ReadOnly should default false when WARD_READONLY is unset")
	}
	t.Setenv("WARD_READONLY", "1")
	if e, _ := readBootstrapEnv(); !e.ReadOnly {
		t.Error("WARD_READONLY=1 should set bootstrapEnv.ReadOnly")
	}
}

// readReapEnv maps WARD_READONLY onto reapEnv.ReadOnly so the reaper short-circuits
// before it can push or salvage a read-only session's working tree (ward#293).
func TestReadReapEnvReadOnly(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")

	t.Setenv("WARD_READONLY", "1")
	if e, _ := readReapEnv(); !e.ReadOnly {
		t.Error("WARD_READONLY=1 should set reapEnv.ReadOnly")
	}
	t.Setenv("WARD_READONLY", "")
	if e, _ := readReapEnv(); e.ReadOnly {
		t.Error("ReadOnly should default false when WARD_READONLY is unset")
	}
}

// composeContext appends the read-only restriction block only for a read-only run,
// since a seedless surface session has no prompt to carry the "do not push" rule.
func TestComposeContextReadOnlyBlock(t *testing.T) {
	const marker = "Read-only session (this overrides the autonomy doctrine above)"
	r := &Runner{}

	writable := composeInto(t, r, false)
	if strings.Contains(writable, marker) {
		t.Error("a writable session must not get the read-only restriction block")
	}
	readonly := composeInto(t, r, true)
	if !strings.Contains(readonly, marker) {
		t.Error("a read-only session must get the read-only restriction block")
	}
	if !strings.Contains(readonly, "warded director") {
		t.Error("the read-only block should name the warded director surface (ward#353)")
	}
	// ward#315: the reframed block permits dispatch (file + commission a sibling),
	// not just "do not push". It must invite filing issues and dispatching headless.
	if !strings.Contains(readonly, "File an issue") {
		t.Error("the read-only block should tell the agent to file an issue (ward#315)")
	}
	if !strings.Contains(readonly, "Dispatch a sibling headless run") {
		t.Error("the read-only block should tell the agent to dispatch a sibling run (ward#315)")
	}
	if !strings.Contains(readonly, "inherits the surface's own harness by default") {
		t.Error("the read-only block should make the default harness inheritance explicit")
	}
	if !strings.Contains(readonly, "Codex director") {
		t.Error("the read-only block should name the Codex-director default explicitly")
	}
	if !strings.Contains(readonly, "ward agent stop <owner/repo#N>") {
		t.Error("the read-only block should name the brokered cleanup command")
	}
	if !strings.Contains(readonly, "restart `warded`") {
		t.Error("the read-only block should say already-running surfaces need a restart for the socket mount")
	}
	// ward#320: capture-and-dispatch is an obligation, not a "may". The block must
	// frame it imperatively.
	if !strings.Contains(readonly, "obligation, not a") {
		t.Error("the read-only block should frame capture-and-dispatch as an obligation, not a 'may' (ward#320)")
	}
	// ward#1620: the attached surface returns judgment and repetition to the
	// harness-native goal. Ward itself has no heartbeat or autonomous retry loop.
	if !strings.Contains(readonly, "harness-native goal loop") || !strings.Contains(readonly, "`/goal`") {
		t.Error("the read-only block should return control to the harness-native goal loop")
	}
	if strings.Contains(readonly, "director heartbeat") {
		t.Error("the read-only block must not promise a retired director heartbeat")
	}
	if !strings.Contains(readonly, "babysit one worker") {
		t.Error("the read-only block should return to the goal loop instead of babysitting one worker (ward#320)")
	}
	// ward#374: prefer a sibling warded dispatch over an in-session subagent. The block
	// must steer delegable work to a durable sibling run, not a scrollback-bound subagent.
	if !strings.Contains(readonly, "Prefer a sibling dispatch over an in-session subagent") {
		t.Error("the read-only block should prefer a sibling dispatch over an in-session subagent (ward#374)")
	}
	if !strings.Contains(readonly, "Reserve an in-session subagent for read-only fan-out") {
		t.Error("the read-only block should reserve a subagent for read-only fan-out that feeds only its own reasoning (ward#374)")
	}
}

// composeInto runs composeContext against a temp AGENT_HOME with no host context
// (level 0) and returns the composed CLAUDE.md text.
func composeInto(t *testing.T, r *Runner, readOnly bool) string {
	t.Helper()
	home := t.TempDir()
	if err := r.composeContext(bootstrapEnv{
		Mode:         "claude",
		ContextLevel: "0",
		ContextSrc:   filepath.Join(t.TempDir(), "absent"),
		AgentHome:    home,
		ReadOnly:     readOnly,
	}); err != nil {
		t.Fatalf("composeContext: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("composed context not written: %v", err)
	}
	return string(out)
}
