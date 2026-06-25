package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

// `architect` is a top-level role of the startup roster (ward#347, was `explore`):
// the read-only interactive scoping session. The writable `sandbox` was removed.
func TestAgentHasArchitectRole(t *testing.T) {
	surfaces := map[string]bool{}
	for _, c := range agentCommand().Commands {
		surfaces[c.Name] = true
	}
	if !surfaces["architect"] {
		t.Errorf("ward agent missing %q role; got %v", "architect", surfaces)
	}
	// The retired verbs are gone outright (hard rename, no aliases; ward#347).
	for _, gone := range []string{"explore", "sandbox"} {
		if surfaces[gone] {
			t.Errorf("retired verb %q must be gone after the architect rename; got %v", gone, surfaces)
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

// resolveForgejoToken prefers an already-present FORGEJO_TOKEN over the host SSM
// lookup, so a `warded #N` dispatched from inside an explore box resolves (ward#315).
func TestResolveForgejoTokenPrefersEnv(t *testing.T) {
	stub := tokenStub(t, "ssm-token")
	r, _, _ := bufRunner(stub)

	// No WARD_BROKER_SOCK here, so the broker seed is inert and the env/SSM path runs.
	t.Setenv("FORGEJO_TOKEN", "env-token")
	got, err := r.resolveForgejoToken(t.Context(), broker.Target{})
	if err != nil {
		t.Fatalf("resolveForgejoToken (env set): %v", err)
	}
	if got != "env-token" {
		t.Errorf("with FORGEJO_TOKEN set, resolveForgejoToken = %q, want the env value (no SSM call)", got)
	}

	t.Setenv("FORGEJO_TOKEN", "")
	got, err = r.resolveForgejoToken(t.Context(), broker.Target{})
	if err != nil {
		t.Fatalf("resolveForgejoToken (env empty): %v", err)
	}
	if got != "ssm-token" {
		t.Errorf("with FORGEJO_TOKEN empty, resolveForgejoToken = %q, want the SSM fallback", got)
	}
}

// tokenStub writes a stand-in binary that echoes a fixed token, standing in for the
// `aws ssm get-parameter` call resolveForgejoToken makes when the env var is unset.
func tokenStub(t *testing.T, token string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "aws")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho "+token+"\n"), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	return stub
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
// since a seedless explore session has no prompt to carry the "do not push" rule.
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
	if !strings.Contains(readonly, "warded architect") {
		t.Error("the read-only block should name the warded architect surface")
	}
	// ward#315: the reframed block permits dispatch (file + commission a sibling),
	// not just "do not push". It must invite filing issues and dispatching headless.
	if !strings.Contains(readonly, "File an issue") {
		t.Error("the read-only block should tell the agent to file an issue (ward#315)")
	}
	if !strings.Contains(readonly, "Dispatch a sibling headless run") {
		t.Error("the read-only block should tell the agent to dispatch a sibling run (ward#315)")
	}
	// ward#320: capture-and-dispatch is an obligation, not a "may". The block must
	// frame it imperatively and contrast with the chatty supervised backlog loop.
	if !strings.Contains(readonly, "obligation, not a") {
		t.Error("the read-only block should frame capture-and-dispatch as an obligation, not a 'may' (ward#320)")
	}
	if !strings.Contains(readonly, "This is not the director loop") {
		t.Error("the read-only block should contrast the architect with the supervised director loop (ward#320)")
	}
	if !strings.Contains(readonly, "without babysitting") {
		t.Error("the read-only block should frame explore as capture-and-dispatch without babysitting (ward#320)")
	}
}

// composeInto runs composeContext against a temp AGENT_HOME with no host context
// (level 0) and returns the composed CLAUDE.md text.
func composeInto(t *testing.T, r *Runner, readOnly bool) string {
	t.Helper()
	home := t.TempDir()
	r.composeContext(bootstrapEnv{
		Mode:         "claude",
		ContextLevel: "0",
		ContextSrc:   filepath.Join(t.TempDir(), "absent"),
		AgentHome:    home,
		ReadOnly:     readOnly,
	})
	out, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("composed context not written: %v", err)
	}
	return string(out)
}
